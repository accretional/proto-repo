// Package protos indexes a FileDescriptorSet into a SQLite DB at
// both package and symbol granularity.
package protos

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/accretional/proto-repo/schema"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Paths in SourceCodeInfo.Location.Path follow the tag numbers of the parent
// field in FileDescriptorProto / DescriptorProto. These are the top-level
// entries we care about.
const (
	tagMessage int = 4 // FileDescriptorProto.message_type
	tagEnum    int = 5 // FileDescriptorProto.enum_type
	tagService int = 6 // FileDescriptorProto.service
	tagMethod  int = 2 // ServiceDescriptorProto.method
)

// Index groups fds by package, writes one packages row per package (with a
// per-package FileDescriptorSet blob) and one symbols row per top-level
// message/enum/service and per service method.
func Index(fds *descriptorpb.FileDescriptorSet, repoLabel, outPath string) error {
	db, err := schema.OpenFresh(outPath, schema.ProtosDDL)
	if err != nil {
		return err
	}
	defer db.Close()

	// Group files by proto package.
	byPkg := map[string][]*descriptorpb.FileDescriptorProto{}
	for _, f := range fds.GetFile() {
		byPkg[f.GetPackage()] = append(byPkg[f.GetPackage()], f)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("protos: begin tx: %w", err)
	}
	pkgStmt, err := tx.Prepare(`INSERT INTO packages(repo, proto_package, file_count, descriptor_set) VALUES (?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("protos: prepare packages: %w", err)
	}
	defer pkgStmt.Close()
	symStmt, err := tx.Prepare(`INSERT INTO symbols(package_id, kind, name, fqn, file_path, line, descriptor, input_fqn, output_fqn) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("protos: prepare symbols: %w", err)
	}
	defer symStmt.Close()

	for pkg, files := range byPkg {
		pkgFDS := &descriptorpb.FileDescriptorSet{File: files}
		pkgBlob, err := proto.Marshal(pkgFDS)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("protos: marshal package %q: %w", pkg, err)
		}
		res, err := pkgStmt.Exec(repoLabel, pkg, len(files), pkgBlob)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("protos: insert package %q: %w", pkg, err)
		}
		pkgID, err := res.LastInsertId()
		if err != nil {
			tx.Rollback()
			return err
		}

		for _, f := range files {
			if err := indexFile(symStmt, pkgID, pkg, f); err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("protos: commit: %w", err)
	}
	return nil
}

func indexFile(stmt *sql.Stmt, pkgID int64, pkg string, f *descriptorpb.FileDescriptorProto) error {
	lines := buildLineMap(f)
	filePath := f.GetName()

	for i, m := range f.MessageType {
		blob, err := proto.Marshal(m)
		if err != nil {
			return fmt.Errorf("protos: marshal message %s: %w", m.GetName(), err)
		}
		if _, err := stmt.Exec(
			pkgID, "message", m.GetName(), fqn(pkg, m.GetName()),
			filePath, nullInt(lines[key(tagMessage, i)]), blob, nil, nil,
		); err != nil {
			return err
		}
	}
	for i, e := range f.EnumType {
		blob, err := proto.Marshal(e)
		if err != nil {
			return fmt.Errorf("protos: marshal enum %s: %w", e.GetName(), err)
		}
		if _, err := stmt.Exec(
			pkgID, "enum", e.GetName(), fqn(pkg, e.GetName()),
			filePath, nullInt(lines[key(tagEnum, i)]), blob, nil, nil,
		); err != nil {
			return err
		}
	}
	for i, s := range f.Service {
		blob, err := proto.Marshal(s)
		if err != nil {
			return fmt.Errorf("protos: marshal service %s: %w", s.GetName(), err)
		}
		if _, err := stmt.Exec(
			pkgID, "service", s.GetName(), fqn(pkg, s.GetName()),
			filePath, nullInt(lines[key(tagService, i)]), blob, nil, nil,
		); err != nil {
			return err
		}
		for j, m := range s.Method {
			mblob, err := proto.Marshal(m)
			if err != nil {
				return fmt.Errorf("protos: marshal method %s.%s: %w", s.GetName(), m.GetName(), err)
			}
			methodFQN := fqn(pkg, s.GetName()) + "." + m.GetName()
			if _, err := stmt.Exec(
				pkgID, "method", m.GetName(), methodFQN,
				filePath, nullInt(lines[key(tagService, i, tagMethod, j)]), mblob,
				strings.TrimPrefix(m.GetInputType(), "."),
				strings.TrimPrefix(m.GetOutputType(), "."),
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func fqn(pkg, name string) string {
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}

// key produces a stable string for SourceCodeInfo path lookups.
func key(parts ...int) string {
	b := make([]byte, 0, len(parts)*3)
	for i, p := range parts {
		if i > 0 {
			b = append(b, '.')
		}
		b = appendInt(b, p)
	}
	return string(b)
}

func appendInt(b []byte, n int) []byte {
	if n == 0 {
		return append(b, '0')
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return append(b, buf[i:]...)
}

// buildLineMap returns map[pathKey]line(1-based) for every Location in the file.
func buildLineMap(f *descriptorpb.FileDescriptorProto) map[string]int {
	out := map[string]int{}
	sci := f.GetSourceCodeInfo()
	if sci == nil {
		return out
	}
	for _, loc := range sci.Location {
		if len(loc.Span) < 1 {
			continue
		}
		parts := make([]int, len(loc.Path))
		for i, v := range loc.Path {
			parts[i] = int(v)
		}
		out[key(parts...)] = int(loc.Span[0]) + 1
	}
	return out
}

func nullInt(n int) interface{} {
	if n == 0 {
		return nil
	}
	return n
}
