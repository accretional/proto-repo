// Package source indexes a repository's source files into a SQLite DB.
package source

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/accretional/proto-repo/schema"
)

// skipDirs are directory names we never descend into.
var skipDirs = map[string]bool{
	".git":         true,
	"vendor":       true,
	"node_modules": true,
	"third_party":  true,
	"build":        true,
	"dist":         true,
	".venv":        true,
	"__pycache__":  true,
}

// maxFileSize is the upper bound for files we'll store full content for (1 MiB).
// Larger files still get a row, but content is empty.
const maxFileSize = 1 << 20

// Index walks repoPath and writes every eligible source file into outPath as a
// fresh SQLite DB. repoLabel is stored in the repo column for every row.
func Index(repoPath, repoLabel, outPath string) error {
	db, err := schema.OpenFresh(outPath, schema.SourceDDL)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("source: begin tx: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO files(repo, path, language, size, sha256, content) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("source: prepare: %w", err)
	}
	defer stmt.Close()

	walkErr := filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(repoPath, path)
		if err != nil {
			rel = path
		}

		var content []byte
		if info.Size() <= maxFileSize {
			b, err := os.ReadFile(path)
			if err == nil && utf8.Valid(b) {
				content = b
			}
		}
		sum := sha256.Sum256(content)
		if _, err := stmt.Exec(
			repoLabel,
			rel,
			language(rel),
			info.Size(),
			hex.EncodeToString(sum[:]),
			string(content),
		); err != nil {
			return fmt.Errorf("source: insert %s: %w", rel, err)
		}
		return nil
	})
	if walkErr != nil {
		tx.Rollback()
		return walkErr
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("source: commit: %w", err)
	}
	return nil
}

func language(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".proto":
		return "proto"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".kt":
		return "kotlin"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".hpp", ".hh":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".sh", ".bash":
		return "shell"
	case ".md":
		return "markdown"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".sql":
		return "sql"
	}
	return ""
}
