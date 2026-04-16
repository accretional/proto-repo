// Package gitexec centralizes the plumbing shared by the importer and
// subcommands services: resolving a Repo to a local directory, running
// `git` against that directory, and verifying git itself is usable.
//
// Both services used to carry near-identical copies of this code. Keeping
// the logic in one place avoids drift (and historical bugs like the two
// `resolve` functions disagreeing on how `path` sources behave).
package gitexec

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	pb "github.com/accretional/proto-repo/genpb"
)

// Resolved is the outcome of mapping a Repo + scratch dir to a local location.
// Path is always set after a successful Resolve. CloneURL is empty for
// path-sourced repos (where we're not allowed to clone — the caller pointed
// us at an on-disk checkout they manage).
type Resolved struct {
	Path     string
	CloneURL string
	Name     string
	Explicit bool
}

// Resolve maps a Repo to its Resolved form. URI and GitHub sources live
// under scratchDir/<basename>; explicit-path sources are used as-is.
func Resolve(scratchDir string, r *pb.Repo) (Resolved, error) {
	src := r.GetSource()
	if src == nil || src.Source == nil {
		return Resolved{}, fmt.Errorf("repo missing source")
	}
	switch v := src.Source.(type) {
	case *pb.RepoSource_Uri:
		if v.Uri == "" {
			return Resolved{}, fmt.Errorf("uri source is empty")
		}
		name := NameFromURI(v.Uri)
		return Resolved{Path: filepath.Join(scratchDir, name), CloneURL: v.Uri, Name: name}, nil
	case *pb.RepoSource_Gh:
		gh := v.Gh
		if gh == nil || gh.Owner == "" || gh.Name == "" {
			return Resolved{}, fmt.Errorf("github source missing owner/name")
		}
		return Resolved{
			Path:     filepath.Join(scratchDir, gh.Name),
			CloneURL: fmt.Sprintf("https://github.com/%s/%s.git", gh.Owner, gh.Name),
			Name:     gh.Name,
		}, nil
	case *pb.RepoSource_Path:
		if v.Path == nil || v.Path.Path == "" {
			return Resolved{}, fmt.Errorf("path source missing path")
		}
		abs, err := filepath.Abs(v.Path.Path)
		if err != nil {
			return Resolved{}, err
		}
		return Resolved{Path: abs, Name: filepath.Base(abs), Explicit: true}, nil
	}
	return Resolved{}, fmt.Errorf("unknown source type")
}

// Exec runs `git args...` inside dir, appending captured stdout/stderr lines
// to msg and any non-zero exit to msg.Errs. If dir is empty, the command
// runs in the caller's current directory (useful for `git clone <url> <dest>`).
func Exec(ctx context.Context, dir string, msg *pb.RepoMsg, args ...string) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	msg.Stdout.Line = append(msg.Stdout.Line, SplitLines(stdout.String())...)
	msg.Stderr.Line = append(msg.Stderr.Line, SplitLines(stderr.String())...)
	if err != nil {
		msg.Errs = append(msg.Errs, fmt.Sprintf("git %s: %v", strings.Join(args, " "), err))
	}
}

// NewMsg returns a RepoMsg populated with the Repo reference and empty
// stdout/stderr containers, ready for Exec to append into.
func NewMsg(r *pb.Repo) *pb.RepoMsg {
	return &pb.RepoMsg{Repo: r, Stdout: &pb.RepoLogs{}, Stderr: &pb.RepoLogs{}}
}

// SplitLines strips a trailing newline and splits on "\n". Returns nil for
// an empty string so RepoMsg.{Stdout,Stderr}.Line doesn't gain a spurious
// empty entry.
func SplitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// IsGitRepo reports whether dir contains a .git directory. Good enough for
// the importer's "already cloned?" probe; doesn't detect bare repos or the
// file-style .git pointer used by submodules.
func IsGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

// NameFromURI extracts a sensible local subdirectory name from a clone URL,
// e.g. "https://github.com/foo/bar.git" -> "bar", "file:///tmp/x" -> "x".
func NameFromURI(uri string) string {
	if i := strings.IndexAny(uri, "?#"); i >= 0 {
		uri = uri[:i]
	}
	uri = strings.TrimRight(uri, "/")
	uri = strings.TrimSuffix(uri, ".git")
	if u, err := url.Parse(uri); err == nil && u.Path != "" {
		return filepath.Base(u.Path)
	}
	return filepath.Base(uri)
}

// MinGitVersion is the oldest git the server will accept. 2.20 (Dec 2018)
// is the baseline for the flag surface our argv builders assume.
var MinGitVersion = GitVersion{2, 20, 0}

// TestedGitVersion is the version CI currently exercises against. Older-but-
// supported gits load a stderr warning because some newer flags/behavior
// may be absent or differ.
var TestedGitVersion = GitVersion{2, 39, 0}

type GitVersion struct{ Major, Minor, Patch int }

func (v GitVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func (v GitVersion) Less(o GitVersion) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// ProbeGit runs `git --version` once and verifies the binary is usable and
// new enough. Returns the parsed version. Fails if git is missing or older
// than MinGitVersion; writes a warning to stderr if older than TestedGitVersion.
func ProbeGit() (GitVersion, error) {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return GitVersion{}, fmt.Errorf("git not usable on PATH (need >= %s): %w", MinGitVersion, err)
	}
	v, err := ParseGitVersion(string(out))
	if err != nil {
		return GitVersion{}, fmt.Errorf("parsing %q: %w", strings.TrimSpace(string(out)), err)
	}
	if v.Less(MinGitVersion) {
		return v, fmt.Errorf("git %s is too old; need >= %s", v, MinGitVersion)
	}
	if v.Less(TestedGitVersion) {
		fmt.Fprintf(os.Stderr, "gitexec: warning: git %s is older than the tested %s; some flags may behave differently\n", v, TestedGitVersion)
	}
	return v, nil
}

// ParseGitVersion pulls X.Y.Z out of strings like
// "git version 2.39.5 (Apple Git-154)". Trailing non-numeric suffixes on
// each field (e.g. "5-rc1" or "1.1-ubuntu") are stripped.
func ParseGitVersion(s string) (GitVersion, error) {
	const prefix = "git version "
	i := strings.Index(s, prefix)
	if i < 0 {
		return GitVersion{}, fmt.Errorf("missing %q prefix", prefix)
	}
	rest := s[i+len(prefix):]
	if j := strings.IndexAny(rest, " ("); j >= 0 {
		rest = rest[:j]
	}
	parts := strings.SplitN(strings.TrimSpace(rest), ".", 3)
	if len(parts) < 2 {
		return GitVersion{}, fmt.Errorf("need major.minor in %q", rest)
	}
	out := GitVersion{}
	fields := []*int{&out.Major, &out.Minor, &out.Patch}
	for k, p := range parts {
		end := 0
		for end < len(p) && p[end] >= '0' && p[end] <= '9' {
			end++
		}
		if end == 0 {
			return GitVersion{}, fmt.Errorf("field %d (%q): not numeric", k, parts[k])
		}
		n, err := strconv.Atoi(p[:end])
		if err != nil {
			return GitVersion{}, fmt.Errorf("field %d (%q): %w", k, parts[k], err)
		}
		*fields[k] = n
	}
	return out, nil
}
