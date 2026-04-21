// Package gitfs reads a real .git directory into the protobuf messages
// defined in gitfs.proto. The byte-level reasoning behind every parser
// here lives in GIT_DEEPDIVE.md.
//
// This package only reads. Writing back into a working .git would also
// need to compute SHA-1s, zlib-encode object bodies, and resolve pack
// deltas — all out of scope here.
package gitfs

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	pb "github.com/accretional/proto-repo/gitfs/pb"
)

// ---------------------------------------------------------------------------
// Object loading (loose objects only — pack object resolution is deferred)
// ---------------------------------------------------------------------------

// LoadObject reads .git/objects/XX/<rest> for the given hex SHA-1, verifies
// the SHA matches, and returns the typed Object. Errors if the object is
// not present as a loose object — packed objects are returned by
// LoadPackedObject (not yet implemented).
func LoadObject(gitDir, hexSHA string) (*pb.Object, error) {
	if len(hexSHA) != 40 {
		return nil, fmt.Errorf("gitfs: sha must be 40 hex chars, got %d", len(hexSHA))
	}
	path := filepath.Join(gitDir, "objects", hexSHA[:2], hexSHA[2:])
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gitfs: read loose object: %w", err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("gitfs: zlib reader: %w", err)
	}
	defer zr.Close()
	full, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("gitfs: zlib decompress: %w", err)
	}
	// Verify SHA over uncompressed framing.
	got := sha1.Sum(full)
	want, err := hex.DecodeString(hexSHA)
	if err != nil {
		return nil, fmt.Errorf("gitfs: decode sha: %w", err)
	}
	if !bytes.Equal(got[:], want) {
		return nil, fmt.Errorf("gitfs: sha mismatch — file says %x, computed %x", want, got)
	}
	return parseObjectBody(want, full)
}

// parseObjectBody takes the SHA + uncompressed framing bytes and dispatches
// to a typed parser.
func parseObjectBody(sha20, full []byte) (*pb.Object, error) {
	nul := bytes.IndexByte(full, 0)
	if nul < 0 {
		return nil, errors.New("gitfs: object framing missing NUL")
	}
	header := string(full[:nul])
	body := full[nul+1:]

	sp := strings.IndexByte(header, ' ')
	if sp < 0 {
		return nil, fmt.Errorf("gitfs: object header missing space: %q", header)
	}
	typeStr := header[:sp]
	declSize, err := strconv.Atoi(header[sp+1:])
	if err != nil {
		return nil, fmt.Errorf("gitfs: object header bad size: %q", header)
	}
	if declSize != len(body) {
		return nil, fmt.Errorf("gitfs: object header size %d != actual body %d", declSize, len(body))
	}

	out := &pb.Object{Sha1: sha20}
	switch typeStr {
	case "blob":
		out.Type = pb.ObjectType_BLOB
		out.Body = &pb.Object_Blob{Blob: &pb.Blob{Content: body}}
	case "tree":
		out.Type = pb.ObjectType_TREE
		t, err := parseTree(body)
		if err != nil {
			return nil, err
		}
		out.Body = &pb.Object_Tree{Tree: t}
	case "commit":
		out.Type = pb.ObjectType_COMMIT
		c, err := parseCommit(body)
		if err != nil {
			return nil, err
		}
		out.Body = &pb.Object_Commit{Commit: c}
	case "tag":
		out.Type = pb.ObjectType_TAG
		t, err := parseTag(body)
		if err != nil {
			return nil, err
		}
		out.Body = &pb.Object_Tag{Tag: t}
	default:
		return nil, fmt.Errorf("gitfs: unknown object type %q", typeStr)
	}
	return out, nil
}

func parseTree(body []byte) (*pb.Tree, error) {
	var entries []*pb.TreeEntry
	for i := 0; i < len(body); {
		sp := bytes.IndexByte(body[i:], ' ')
		if sp < 0 {
			return nil, errors.New("gitfs: tree entry missing space")
		}
		modeStr := string(body[i : i+sp])
		mode, err := strconv.ParseUint(modeStr, 8, 32)
		if err != nil {
			return nil, fmt.Errorf("gitfs: tree mode parse %q: %w", modeStr, err)
		}
		nameStart := i + sp + 1
		nul := bytes.IndexByte(body[nameStart:], 0)
		if nul < 0 {
			return nil, errors.New("gitfs: tree entry missing NUL")
		}
		name := string(body[nameStart : nameStart+nul])
		shaStart := nameStart + nul + 1
		if shaStart+20 > len(body) {
			return nil, errors.New("gitfs: tree entry truncated SHA")
		}
		sha := make([]byte, 20)
		copy(sha, body[shaStart:shaStart+20])
		entries = append(entries, &pb.TreeEntry{
			Mode: uint32(mode),
			Name: name,
			Sha1: sha,
		})
		i = shaStart + 20
	}
	return &pb.Tree{Entries: entries}, nil
}

func parseCommit(body []byte) (*pb.Commit, error) {
	headers, message, err := splitHeadersAndMessage(body)
	if err != nil {
		return nil, err
	}
	c := &pb.Commit{Message: message}
	for _, h := range headers {
		switch h.key {
		case "tree":
			sha, err := hex.DecodeString(h.value)
			if err != nil {
				return nil, fmt.Errorf("gitfs: commit tree sha: %w", err)
			}
			c.TreeSha1 = sha
		case "parent":
			sha, err := hex.DecodeString(h.value)
			if err != nil {
				return nil, fmt.Errorf("gitfs: commit parent sha: %w", err)
			}
			c.ParentSha1S = append(c.ParentSha1S, sha)
		case "author":
			sig, err := parseSignature(h.value)
			if err != nil {
				return nil, fmt.Errorf("gitfs: commit author: %w", err)
			}
			c.Author = sig
		case "committer":
			sig, err := parseSignature(h.value)
			if err != nil {
				return nil, fmt.Errorf("gitfs: commit committer: %w", err)
			}
			c.Committer = sig
		default:
			c.ExtraHeaders = append(c.ExtraHeaders, &pb.CommitHeader{
				Key:   h.key,
				Value: h.value,
			})
		}
	}
	return c, nil
}

func parseTag(body []byte) (*pb.Tag, error) {
	headers, message, err := splitHeadersAndMessage(body)
	if err != nil {
		return nil, err
	}
	t := &pb.Tag{Message: message}
	for _, h := range headers {
		switch h.key {
		case "object":
			sha, err := hex.DecodeString(h.value)
			if err != nil {
				return nil, fmt.Errorf("gitfs: tag object sha: %w", err)
			}
			t.ObjectSha1 = sha
		case "type":
			switch h.value {
			case "commit":
				t.TargetType = pb.ObjectType_COMMIT
			case "tree":
				t.TargetType = pb.ObjectType_TREE
			case "blob":
				t.TargetType = pb.ObjectType_BLOB
			case "tag":
				t.TargetType = pb.ObjectType_TAG
			}
		case "tag":
			t.Name = h.value
		case "tagger":
			sig, err := parseSignature(h.value)
			if err != nil {
				return nil, fmt.Errorf("gitfs: tag tagger: %w", err)
			}
			t.Tagger = sig
		}
	}
	return t, nil
}

type rawHeader struct {
	key, value string
}

// splitHeadersAndMessage parses RFC-822-style folded headers, a blank line,
// then the free-form message body.
func splitHeadersAndMessage(body []byte) ([]rawHeader, string, error) {
	// Find the blank line separating headers from message.
	sep := bytes.Index(body, []byte("\n\n"))
	if sep < 0 {
		return nil, "", errors.New("gitfs: object missing header/message separator")
	}
	headerBytes := body[:sep]
	message := string(body[sep+2:])

	var headers []rawHeader
	lines := strings.Split(string(headerBytes), "\n")
	var cur *rawHeader
	for _, line := range lines {
		if strings.HasPrefix(line, " ") {
			if cur == nil {
				return nil, "", errors.New("gitfs: continuation with no current header")
			}
			cur.value += "\n" + line[1:]
			continue
		}
		if cur != nil {
			headers = append(headers, *cur)
		}
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			return nil, "", fmt.Errorf("gitfs: bad header line %q", line)
		}
		cur = &rawHeader{key: line[:sp], value: line[sp+1:]}
	}
	if cur != nil {
		headers = append(headers, *cur)
	}
	return headers, message, nil
}

// parseSignature parses "Name <email> 1234567890 -0700".
func parseSignature(s string) (*pb.Signature, error) {
	lt := strings.LastIndexByte(s, '<')
	gt := strings.LastIndexByte(s, '>')
	if lt < 0 || gt < 0 || gt < lt {
		return nil, fmt.Errorf("gitfs: signature missing email brackets: %q", s)
	}
	name := strings.TrimRight(s[:lt], " ")
	email := s[lt+1 : gt]
	rest := strings.TrimSpace(s[gt+1:])
	parts := strings.Fields(rest)
	if len(parts) != 2 {
		return nil, fmt.Errorf("gitfs: signature missing ts/tz: %q", s)
	}
	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("gitfs: signature ts: %w", err)
	}
	return &pb.Signature{
		Name:        name,
		Email:       email,
		UnixSeconds: ts,
		TzOffset:    parts[1],
	}, nil
}

// ---------------------------------------------------------------------------
// Refs
// ---------------------------------------------------------------------------

// LoadRef reads a single ref file (loose). name is relative to .git/, e.g.
// "HEAD" or "refs/heads/main".
func LoadRef(gitDir, name string) (*pb.Ref, error) {
	raw, err := os.ReadFile(filepath.Join(gitDir, name))
	if err != nil {
		return nil, fmt.Errorf("gitfs: read ref %s: %w", name, err)
	}
	return parseRefContent(name, raw)
}

func parseRefContent(name string, raw []byte) (*pb.Ref, error) {
	s := strings.TrimRight(string(raw), "\n")
	r := &pb.Ref{Name: name}
	if strings.HasPrefix(s, "ref: ") {
		r.Target = &pb.Ref_SymbolicRef{SymbolicRef: strings.TrimPrefix(s, "ref: ")}
		return r, nil
	}
	sha, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("gitfs: bad ref content for %s: %w", name, err)
	}
	r.Target = &pb.Ref_Sha1{Sha1: sha}
	return r, nil
}

// LoadAllLooseRefs walks .git/refs/ and returns every loose ref.
func LoadAllLooseRefs(gitDir string) ([]*pb.Ref, error) {
	var out []*pb.Ref
	root := filepath.Join(gitDir, "refs")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(gitDir, path)
		if err != nil {
			return err
		}
		// Use forward slashes regardless of OS — git ref names always do.
		rel = filepath.ToSlash(rel)
		ref, err := LoadRef(gitDir, rel)
		if err != nil {
			return err
		}
		out = append(out, ref)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// LoadPackedRefs parses .git/packed-refs. Returns (nil, nil) if the file
// doesn't exist (some repos have only loose refs).
func LoadPackedRefs(gitDir string) (*pb.PackedRefs, error) {
	raw, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gitfs: read packed-refs: %w", err)
	}
	out := &pb.PackedRefs{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var lastRef *pb.PackedRef
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# pack-refs with: ") {
			tail := strings.TrimPrefix(line, "# pack-refs with: ")
			for _, t := range strings.Fields(tail) {
				out.HeaderTraits = append(out.HeaderTraits, t)
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "^") {
			if lastRef == nil {
				return nil, fmt.Errorf("gitfs: peeled line without preceding ref: %q", line)
			}
			peeled, err := hex.DecodeString(strings.TrimPrefix(line, "^"))
			if err != nil {
				return nil, fmt.Errorf("gitfs: peeled sha decode: %w", err)
			}
			lastRef.PeeledSha1 = peeled
			continue
		}
		// Normal "<sha> <refname>" line.
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			return nil, fmt.Errorf("gitfs: bad packed-ref line %q", line)
		}
		sha, err := hex.DecodeString(line[:sp])
		if err != nil {
			return nil, fmt.Errorf("gitfs: packed-ref sha: %w", err)
		}
		pr := &pb.PackedRef{Name: line[sp+1:], Sha1: sha}
		out.Refs = append(out.Refs, pr)
		lastRef = pr
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Index
// ---------------------------------------------------------------------------

// LoadIndex parses .git/index. Versions 2 and 3 are fully decoded; v4
// (path prefix compression) returns ErrUnsupportedIndexVersion — the
// schema permits it but the parser does not yet.
var ErrUnsupportedIndexVersion = errors.New("gitfs: unsupported index version (v4 path-compression not implemented)")

func LoadIndex(gitDir string) (*pb.Index, error) {
	raw, err := os.ReadFile(filepath.Join(gitDir, "index"))
	if err != nil {
		return nil, fmt.Errorf("gitfs: read index: %w", err)
	}
	if len(raw) < 12+20 {
		return nil, errors.New("gitfs: index too short")
	}
	if string(raw[:4]) != "DIRC" {
		return nil, fmt.Errorf("gitfs: bad index signature %q", raw[:4])
	}
	version := binary.BigEndian.Uint32(raw[4:8])
	numEntries := binary.BigEndian.Uint32(raw[8:12])

	if version != 2 && version != 3 {
		return nil, ErrUnsupportedIndexVersion
	}

	// Verify trailer SHA over preceding bytes.
	body := raw[:len(raw)-20]
	trailer := raw[len(raw)-20:]
	got := sha1.Sum(body)
	if !bytes.Equal(got[:], trailer) {
		return nil, fmt.Errorf("gitfs: index trailer sha mismatch — got %x want %x", got, trailer)
	}

	idx := &pb.Index{
		Version:     version,
		TrailerSha1: append([]byte(nil), trailer...),
	}

	off := 12
	for i := uint32(0); i < numEntries; i++ {
		e, n, err := parseIndexEntry(raw[off:], version)
		if err != nil {
			return nil, fmt.Errorf("gitfs: index entry %d: %w", i, err)
		}
		idx.Entries = append(idx.Entries, e)
		off += n
	}

	// Extensions until we hit the trailer (last 20 bytes).
	end := len(raw) - 20
	for off < end {
		if off+8 > end {
			return nil, errors.New("gitfs: index extension truncated header")
		}
		sig := append([]byte(nil), raw[off:off+4]...)
		size := binary.BigEndian.Uint32(raw[off+4 : off+8])
		if off+8+int(size) > end {
			return nil, errors.New("gitfs: index extension truncated body")
		}
		data := append([]byte(nil), raw[off+8:off+8+int(size)]...)
		idx.Extensions = append(idx.Extensions, &pb.IndexExtension{
			Signature: sig,
			Data:      data,
		})
		off += 8 + int(size)
	}
	return idx, nil
}

func parseIndexEntry(buf []byte, version uint32) (*pb.IndexEntry, int, error) {
	if len(buf) < 62 {
		return nil, 0, errors.New("gitfs: entry header too short")
	}
	e := &pb.IndexEntry{
		CtimeSeconds:     binary.BigEndian.Uint32(buf[0:4]),
		CtimeNanoseconds: binary.BigEndian.Uint32(buf[4:8]),
		MtimeSeconds:     binary.BigEndian.Uint32(buf[8:12]),
		MtimeNanoseconds: binary.BigEndian.Uint32(buf[12:16]),
		Dev:              binary.BigEndian.Uint32(buf[16:20]),
		Ino:              binary.BigEndian.Uint32(buf[20:24]),
		Mode:             binary.BigEndian.Uint32(buf[24:28]),
		Uid:              binary.BigEndian.Uint32(buf[28:32]),
		Gid:              binary.BigEndian.Uint32(buf[32:36]),
		Size:             binary.BigEndian.Uint32(buf[36:40]),
		Sha1:             append([]byte(nil), buf[40:60]...),
	}
	flags := binary.BigEndian.Uint16(buf[60:62])
	e.AssumeValid = flags&0x8000 != 0
	e.Extended = flags&0x4000 != 0
	e.Stage = uint32((flags >> 12) & 0x3)
	nameLen := int(flags & 0x0FFF)
	off := 62
	if version >= 3 && e.Extended {
		if len(buf) < 64 {
			return nil, 0, errors.New("gitfs: entry ext_flags truncated")
		}
		e.ExtFlags = uint32(binary.BigEndian.Uint16(buf[62:64]))
		off = 64
	}

	// Name: known length OR scan to NUL when nameLen == 0xFFF (overflow).
	var name []byte
	if nameLen < 0xFFF {
		if off+nameLen+1 > len(buf) {
			return nil, 0, errors.New("gitfs: entry name truncated")
		}
		name = buf[off : off+nameLen]
		off += nameLen
	} else {
		end := bytes.IndexByte(buf[off:], 0)
		if end < 0 {
			return nil, 0, errors.New("gitfs: long entry name missing NUL")
		}
		name = buf[off : off+end]
		off += end
	}
	e.Name = string(name)

	// Pad with NULs to 8-byte boundary (entry start was 8-aligned because
	// the previous entry was, and the index header is 12 bytes — but the
	// padding rule is "total entry length multiple of 8", so pad here).
	totalSoFar := off
	pad := 8 - (totalSoFar % 8)
	if pad == 0 {
		pad = 8
	}
	off += pad
	return e, off, nil
}

// ---------------------------------------------------------------------------
// Reflog
// ---------------------------------------------------------------------------

// LoadReflog parses .git/logs/<refPath>. refPath is something like "HEAD"
// or "refs/heads/main" (without the leading "logs/").
func LoadReflog(gitDir, refPath string) (*pb.Reflog, error) {
	raw, err := os.ReadFile(filepath.Join(gitDir, "logs", refPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gitfs: read reflog %s: %w", refPath, err)
	}
	rl := &pb.Reflog{RefPath: refPath}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		entry, err := parseReflogLine(line)
		if err != nil {
			return nil, fmt.Errorf("gitfs: reflog %s: %w", refPath, err)
		}
		rl.Entries = append(rl.Entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rl, nil
}

func parseReflogLine(line string) (*pb.ReflogEntry, error) {
	// "<old> <new> <committer>\t<message>"
	tab := strings.IndexByte(line, '\t')
	if tab < 0 {
		return nil, fmt.Errorf("missing tab: %q", line)
	}
	prefix := line[:tab]
	message := line[tab+1:]

	if len(prefix) < 41+41 {
		return nil, fmt.Errorf("prefix too short: %q", prefix)
	}
	oldHex := prefix[:40]
	if prefix[40] != ' ' {
		return nil, fmt.Errorf("missing sep after old sha: %q", prefix)
	}
	newHex := prefix[41:81]
	if prefix[81] != ' ' {
		return nil, fmt.Errorf("missing sep after new sha: %q", prefix)
	}
	sigStr := prefix[82:]
	old, err := hex.DecodeString(oldHex)
	if err != nil {
		return nil, fmt.Errorf("old sha: %w", err)
	}
	newer, err := hex.DecodeString(newHex)
	if err != nil {
		return nil, fmt.Errorf("new sha: %w", err)
	}
	sig, err := parseSignature(sigStr)
	if err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}
	return &pb.ReflogEntry{
		OldSha1: old,
		NewSha1: newer,
		Who:     sig,
		Message: message,
	}, nil
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// LoadConfig parses .git/config (or any git config file at `path`).
// LoadConfigDefault reads gitDir/config.
func LoadConfigDefault(gitDir string) (*pb.Config, error) {
	return LoadConfig(filepath.Join(gitDir, "config"))
}

func LoadConfig(path string) (*pb.Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gitfs: read config: %w", err)
	}
	cfg := &pb.Config{}
	var cur *pb.ConfigSection
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inner := line[1 : len(line)-1]
			name, sub := splitSectionHeader(inner)
			cur = &pb.ConfigSection{Name: name, Subsection: sub}
			cfg.Sections = append(cfg.Sections, cur)
			continue
		}
		if cur == nil {
			return nil, fmt.Errorf("gitfs: config variable outside section: %q", line)
		}
		eq := strings.IndexByte(line, '=')
		var k, v string
		if eq < 0 {
			k = line
			v = "true"
		} else {
			k = strings.TrimSpace(line[:eq])
			v = strings.TrimSpace(line[eq+1:])
			v = strings.Trim(v, `"`)
		}
		cur.Variables = append(cur.Variables, &pb.ConfigVariable{Name: k, Value: v})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func splitSectionHeader(inner string) (name, sub string) {
	// `[remote "origin"]` → inner = `remote "origin"`
	if idx := strings.IndexByte(inner, ' '); idx > 0 {
		return inner[:idx], strings.Trim(strings.TrimSpace(inner[idx+1:]), `"`)
	}
	return inner, ""
}

// ---------------------------------------------------------------------------
// Packfiles (header + idx; full object explosion is deferred)
// ---------------------------------------------------------------------------

// LoadPack parses just the header of a .pack file and the full idx file.
// The .pack body (variable-length objects, deltas) is not exploded into
// Object messages here — callers that need that should delegate to a real
// git library.
func LoadPack(gitDir, basename string) (*pb.Pack, error) {
	packPath := filepath.Join(gitDir, "objects", "pack", basename+".pack")
	idxPath := filepath.Join(gitDir, "objects", "pack", basename+".idx")
	hdr, err := loadPackHeader(packPath)
	if err != nil {
		return nil, err
	}
	idx, err := loadPackIndex(idxPath)
	if err != nil {
		return nil, err
	}
	return &pb.Pack{Name: basename, Header: hdr, Index: idx}, nil
}

func loadPackHeader(path string) (*pb.PackHeader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("gitfs: open pack: %w", err)
	}
	defer f.Close()
	var hdr [12]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return nil, fmt.Errorf("gitfs: pack header: %w", err)
	}
	if string(hdr[:4]) != "PACK" {
		return nil, fmt.Errorf("gitfs: bad pack magic %q", hdr[:4])
	}
	version := binary.BigEndian.Uint32(hdr[4:8])
	count := binary.BigEndian.Uint32(hdr[8:12])
	// Trailer is the last 20 bytes of the file.
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	trailer := make([]byte, 20)
	if _, err := f.ReadAt(trailer, st.Size()-20); err != nil {
		return nil, fmt.Errorf("gitfs: pack trailer: %w", err)
	}
	return &pb.PackHeader{
		Version:     version,
		ObjectCount: count,
		PackSha1:    trailer,
	}, nil
}

func loadPackIndex(path string) (*pb.PackIndex, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gitfs: read pack idx: %w", err)
	}
	if len(raw) < 8+256*4+20+20 {
		return nil, errors.New("gitfs: idx too short")
	}
	if !bytes.Equal(raw[:4], []byte{0xff, 't', 'O', 'c'}) {
		return nil, fmt.Errorf("gitfs: bad idx magic %x (legacy v1 not supported)", raw[:4])
	}
	version := binary.BigEndian.Uint32(raw[4:8])
	if version != 2 {
		return nil, fmt.Errorf("gitfs: idx version %d not supported (v2 only)", version)
	}
	fanout := make([]uint32, 256)
	for i := 0; i < 256; i++ {
		fanout[i] = binary.BigEndian.Uint32(raw[8+i*4 : 8+(i+1)*4])
	}
	n := int(fanout[255])
	off := 8 + 256*4
	shas := make([][]byte, n)
	for i := 0; i < n; i++ {
		shas[i] = append([]byte(nil), raw[off+i*20:off+(i+1)*20]...)
	}
	off += n * 20
	crcs := make([]uint32, n)
	for i := 0; i < n; i++ {
		crcs[i] = binary.BigEndian.Uint32(raw[off+i*4 : off+(i+1)*4])
	}
	off += n * 4
	offsets := make([]uint64, n)
	for i := 0; i < n; i++ {
		v := binary.BigEndian.Uint32(raw[off+i*4 : off+(i+1)*4])
		// High bit set ⇒ index into 64-bit table; we don't materialize
		// large-pack offsets here (no .pack > 4 GiB in this repo).
		offsets[i] = uint64(v)
	}
	off += n * 4
	// Skip large-offsets table (if any) — leaves us at the trailers.
	trailerStart := len(raw) - 40
	packSha := append([]byte(nil), raw[trailerStart:trailerStart+20]...)
	idxSha := append([]byte(nil), raw[trailerStart+20:]...)

	return &pb.PackIndex{
		Version:  version,
		Fanout:   fanout,
		Sha1S:    shas,
		Crc32S:   crcs,
		Offsets:  offsets,
		PackSha1: packSha,
		IdxSha1:  idxSha,
	}, nil
}

// ---------------------------------------------------------------------------
// Repository (top-level)
// ---------------------------------------------------------------------------

// Open reads a .git directory into a *pb.Repository. Loose objects are
// loaded en masse; packs are loaded as headers+idx only (object explosion
// is deferred). LocalState fields are best-effort — missing files leave
// the field zero.
func Open(gitDir string) (*pb.Repository, error) {
	abs, err := filepath.Abs(gitDir)
	if err != nil {
		return nil, err
	}
	repo := &pb.Repository{GitDir: abs}

	repo.Head, err = LoadRef(gitDir, "HEAD")
	if err != nil {
		return nil, err
	}

	repo.LooseRefs, err = LoadAllLooseRefs(gitDir)
	if err != nil {
		return nil, err
	}

	repo.PackedRefs, err = LoadPackedRefs(gitDir)
	if err != nil {
		return nil, err
	}

	objs, err := loadAllLooseObjects(gitDir)
	if err != nil {
		return nil, err
	}
	repo.LooseObjects = objs

	packs, err := loadAllPacks(gitDir)
	if err != nil {
		return nil, err
	}
	repo.Packs = packs

	if idx, err := LoadIndex(gitDir); err == nil {
		repo.Index = idx
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	repo.Reflogs, err = loadAllReflogs(gitDir)
	if err != nil {
		return nil, err
	}

	repo.Config, err = LoadConfigDefault(gitDir)
	if err != nil {
		return nil, err
	}

	repo.LocalState = loadLocalState(gitDir)
	return repo, nil
}

func loadAllLooseObjects(gitDir string) ([]*pb.Object, error) {
	root := filepath.Join(gitDir, "objects")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []*pb.Object
	for _, e := range entries {
		if !e.IsDir() || len(e.Name()) != 2 {
			continue
		}
		sub := filepath.Join(root, e.Name())
		files, err := os.ReadDir(sub)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.IsDir() || len(f.Name()) != 38 {
				continue
			}
			obj, err := LoadObject(gitDir, e.Name()+f.Name())
			if err != nil {
				return nil, fmt.Errorf("loose object %s%s: %w", e.Name(), f.Name(), err)
			}
			out = append(out, obj)
		}
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].Sha1, out[j].Sha1) < 0 })
	return out, nil
}

func loadAllPacks(gitDir string) ([]*pb.Pack, error) {
	dir := filepath.Join(gitDir, "objects", "pack")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*pb.Pack
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".pack") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".pack")
		p, err := LoadPack(gitDir, base)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func loadAllReflogs(gitDir string) ([]*pb.Reflog, error) {
	root := filepath.Join(gitDir, "logs")
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var out []*pb.Reflog
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		rl, err := LoadReflog(gitDir, rel)
		if err != nil {
			return err
		}
		if rl != nil {
			out = append(out, rl)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RefPath < out[j].RefPath })
	return out, nil
}

func loadLocalState(gitDir string) *pb.LocalState {
	ls := &pb.LocalState{}
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(gitDir, name))
		if err != nil {
			return nil
		}
		return b
	}
	if b := read("ORIG_HEAD"); b != nil {
		s := strings.TrimSpace(string(b))
		if sha, err := hex.DecodeString(s); err == nil {
			ls.OrigHeadSha1 = sha
		}
	}
	ls.FetchHead = read("FETCH_HEAD")
	if b := read("MERGE_HEAD"); b != nil {
		s := strings.TrimSpace(string(b))
		if sha, err := hex.DecodeString(s); err == nil {
			ls.MergeHeadSha1 = sha
		}
	}
	ls.MergeMsg = string(read("MERGE_MSG"))
	ls.MergeMode = string(read("MERGE_MODE"))
	ls.CommitEditmsg = string(read("COMMIT_EDITMSG"))
	ls.Description = string(read("description"))
	ls.InfoExclude = string(read("info/exclude"))
	ls.Shallow = read("shallow")
	return ls
}
