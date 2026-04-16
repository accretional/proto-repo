// Package gitfetch clones and pulls git repositories to a local scratch dir.
package gitfetch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Result describes a fetched repository on the local filesystem.
type Result struct {
	Path   string // absolute or dest-relative path to the working tree
	Commit string // resolved HEAD commit SHA
}

// Fetch clones the repo at cloneURL into dest/<name>, or updates it if already
// present. Returns the local path and resolved HEAD commit SHA.
func Fetch(ctx context.Context, cloneURL, dest, name string, shallow bool) (*Result, error) {
	if cloneURL == "" {
		return nil, fmt.Errorf("gitfetch: empty cloneURL")
	}
	if dest == "" {
		return nil, fmt.Errorf("gitfetch: empty dest")
	}
	if name == "" {
		return nil, fmt.Errorf("gitfetch: empty name")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, fmt.Errorf("gitfetch: mkdir dest: %w", err)
	}

	localPath := filepath.Join(dest, name)

	if info, err := os.Stat(filepath.Join(localPath, ".git")); err == nil && info.IsDir() {
		if out, err := run(ctx, localPath, "git", "fetch", "--all", "--prune"); err != nil {
			return nil, fmt.Errorf("git fetch in %s: %w\n%s", localPath, err, out)
		}
		// Best-effort fast-forward; ignore failure (detached HEAD, diverged).
		_, _ = run(ctx, localPath, "git", "pull", "--ff-only")
	} else {
		args := []string{"clone"}
		if shallow {
			args = append(args, "--depth", "1")
		}
		args = append(args, cloneURL, localPath)
		if out, err := run(ctx, "", "git", args...); err != nil {
			return nil, fmt.Errorf("git clone %s: %w\n%s", cloneURL, err, out)
		}
	}

	head, err := run(ctx, localPath, "git", "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("git rev-parse HEAD in %s: %w", localPath, err)
	}
	return &Result{Path: localPath, Commit: strings.TrimSpace(head)}, nil
}

func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
