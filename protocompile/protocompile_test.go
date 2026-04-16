package protocompile

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCompileSelf(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	fds, err := Compile(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if fds == nil || len(fds.File) == 0 {
		t.Fatal("expected at least one FileDescriptorProto")
	}
	var found bool
	for _, f := range fds.File {
		if f.GetPackage() == "repo" {
			found = true
			if n := len(f.MessageType); n < 3 {
				t.Errorf("expected >=3 messages in package repo, got %d", n)
			}
			if n := len(f.Service); n != 1 {
				t.Errorf("expected 1 service in package repo, got %d", n)
			}
		}
	}
	if !found {
		t.Fatal("package 'repo' not found in FDS")
	}
	t.Logf("compiled %d files", len(fds.File))
}
