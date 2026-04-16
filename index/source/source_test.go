package source

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestIndexSelf(t *testing.T) {
	// Index this repo (three dirs up from source_test.go: index/source/ -> repo root).
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "self.source.sqlite")
	if err := Index(repoRoot, "proto-repo", out); err != nil {
		t.Fatalf("Index: %v", err)
	}
	db, err := sql.Open("sqlite", out)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected files rows, got 0")
	}

	// FTS lookup for a term we know appears.
	var hits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM files_fts WHERE files_fts MATCH 'gitfetch'`).Scan(&hits); err != nil {
		t.Fatal(err)
	}
	if hits == 0 {
		t.Fatal("expected FTS hits for 'gitfetch', got 0")
	}
	t.Logf("indexed %d files, %d FTS matches for 'gitfetch'", n, hits)
}
