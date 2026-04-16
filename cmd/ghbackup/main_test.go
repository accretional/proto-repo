package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRepoRef(t *testing.T) {
	cases := []struct {
		in    string
		owner string
		name  string
		err   bool
	}{
		{"google/xls", "google", "xls", false},
		{"https://github.com/google/xls", "google", "xls", false},
		{"https://github.com/google/xls.git", "google", "xls", false},
		{"http://github.com/google/xls/", "google", "xls", false},
		{"git@github.com:google/xls.git", "google", "xls", false},
		{"just-one-field", "", "", true},
		{"", "", "", true},
		{"owner/", "", "", true},
		{"/name", "", "", true},
	}
	for _, c := range cases {
		owner, name, err := parseRepoRef(c.in)
		if (err != nil) != c.err {
			t.Errorf("%q: err=%v, want err=%t", c.in, err, c.err)
			continue
		}
		if !c.err && (owner != c.owner || name != c.name) {
			t.Errorf("%q: got %s/%s, want %s/%s", c.in, owner, name, c.owner, c.name)
		}
	}
}

func TestParseList(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "list.txt")
	body := `# leading comment
google/xls
  google/orbax   # trailing comment

https://github.com/openxla/xla
# another comment
transparency-dev/tessera
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	repos, err := parseList(f)
	if err != nil {
		t.Fatalf("parseList: %v", err)
	}
	want := []struct{ owner, name string }{
		{"google", "xls"},
		{"google", "orbax"},
		{"openxla", "xla"},
		{"transparency-dev", "tessera"},
	}
	if len(repos) != len(want) {
		t.Fatalf("got %d repos, want %d", len(repos), len(want))
	}
	for i, w := range want {
		gh := repos[i].GetSource().GetGh()
		if gh.GetOwner() != w.owner || gh.GetName() != w.name {
			t.Errorf("repo[%d]: got %s/%s, want %s/%s", i, gh.GetOwner(), gh.GetName(), w.owner, w.name)
		}
	}
}

func TestParseListBadLine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(p, []byte("good/repo\nnope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := parseList(f); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("expected line-2 error, got %v", err)
	}
}

func TestAcquireLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.lock")

	release, err := acquireLock(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file should exist: %v", err)
	}
	// Second attempt while held should fail.
	if _, err := acquireLock(path); err == nil {
		t.Error("second acquire should fail while held")
	}
	release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("release should remove lock: %v", err)
	}
	// Now it should acquire again.
	release2, err := acquireLock(path)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release2()
}
