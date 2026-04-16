package protos

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/accretional/proto-repo/protocompile"
	_ "modernc.org/sqlite"
)

func TestIndexSelf(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	fds, err := protocompile.Compile(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	out := filepath.Join(t.TempDir(), "self.protos.sqlite")
	if err := Index(fds, "proto-repo", out); err != nil {
		t.Fatalf("Index: %v", err)
	}

	db, err := sql.Open("sqlite", out)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var pkgs, syms, methods int
	if err := db.QueryRow(`SELECT COUNT(*) FROM packages`).Scan(&pkgs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM symbols`).Scan(&syms); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM symbols WHERE kind='method'`).Scan(&methods); err != nil {
		t.Fatal(err)
	}
	if pkgs < 1 {
		t.Errorf("expected >=1 package, got %d", pkgs)
	}
	if syms < 4 {
		t.Errorf("expected >=4 symbols (3 messages + 1 service + method), got %d", syms)
	}
	if methods < 1 {
		t.Errorf("expected >=1 method, got %d", methods)
	}

	// Validate method FQN composition + input/output capture.
	var fqn, in, out2 string
	var line sql.NullInt64
	err = db.QueryRow(`SELECT fqn, input_fqn, output_fqn, line FROM symbols WHERE kind='method' LIMIT 1`).
		Scan(&fqn, &in, &out2, &line)
	if err != nil {
		t.Fatal(err)
	}
	if fqn == "" || in == "" || out2 == "" {
		t.Errorf("method row incomplete: fqn=%q in=%q out=%q", fqn, in, out2)
	}
	t.Logf("packages=%d symbols=%d methods=%d sample=%s (%s -> %s) line=%v",
		pkgs, syms, methods, fqn, in, out2, line)
}
