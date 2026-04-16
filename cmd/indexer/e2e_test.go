package main

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/accretional/proto-repo/scan"
	_ "modernc.org/sqlite"
)

// TestE2ELocalRepo exercises the full processRepo pipeline against a
// locally-constructed git repo (served via file:// URL), avoiding any
// network dependency.
func TestE2ELocalRepo(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed a minimal repo with one .proto file and one Go file.
	proto := `syntax = "proto3";
package demo.v1;
message Hello { string name = 1; }
service Greeter { rpc SayHi(Hello) returns (Hello); }
`
	gofile := `package demo

// Greeter is a demo.
func Greeter() string { return "hi" }
`
	if err := os.WriteFile(filepath.Join(src, "demo.proto"), []byte(proto), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "demo.go"), []byte(gofile), 0o644); err != nil {
		t.Fatal(err)
	}

	// git init + commit
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "add", "."},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "seed"},
	} {
		c := exec.Command("git", args...)
		c.Dir = src
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	out := filepath.Join(tmp, "out")
	scratch := filepath.Join(tmp, "scratch")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}

	repo := scan.Repo{
		Owner:         "local",
		Name:          "demo",
		FullName:      "local/demo",
		CloneURL:      "file://" + src,
		DefaultBranch: "main",
	}
	res, err := processRepo(context.Background(), repo, scratch, out, true)
	if err != nil {
		t.Fatalf("processRepo: %v", err)
	}
	if res != resOK {
		t.Fatalf("expected resOK, got %v", res)
	}

	// Validate source DB
	srcDB := filepath.Join(out, "demo.source.sqlite")
	if _, err := os.Stat(srcDB); err != nil {
		t.Fatalf("source db missing: %v", err)
	}
	db, err := sql.Open("sqlite", srcDB)
	if err != nil {
		t.Fatal(err)
	}
	var srcFiles int
	if err := db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&srcFiles); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if srcFiles < 2 {
		t.Errorf("expected >=2 source files, got %d", srcFiles)
	}

	// Validate protos DB
	protoDB := filepath.Join(out, "demo.protos.sqlite")
	db, err = sql.Open("sqlite", protoDB)
	if err != nil {
		t.Fatalf("protos db open: %v", err)
	}
	defer db.Close()

	var pkg string
	if err := db.QueryRow(`SELECT proto_package FROM packages LIMIT 1`).Scan(&pkg); err != nil {
		t.Fatal(err)
	}
	if pkg != "demo.v1" {
		t.Errorf("expected package demo.v1, got %q", pkg)
	}

	var method, in, outFQN string
	err = db.QueryRow(`SELECT fqn, input_fqn, output_fqn FROM symbols WHERE kind='method'`).
		Scan(&method, &in, &outFQN)
	if err != nil {
		t.Fatal(err)
	}
	if method != "demo.v1.Greeter.SayHi" {
		t.Errorf("method FQN = %q, want demo.v1.Greeter.SayHi", method)
	}
	if in != "demo.v1.Hello" || outFQN != "demo.v1.Hello" {
		t.Errorf("method io = %q -> %q, want demo.v1.Hello -> demo.v1.Hello", in, outFQN)
	}
}
