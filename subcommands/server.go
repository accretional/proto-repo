// Package subcommands implements the subcommands.SubCommands gRPC service:
// thin per-subcommand wrappers around `git <subcommand> ...` executed inside
// each request's resolved repo path. Each subcommand lives in its own file
// (add.go, archive.go, …) and is just a few lines wrapping the run helper
// below. This package handles the basic happy path; per-subcommand args
// beyond the bare minimum are passed through verbatim via SubCommandReq.args.
package subcommands

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

// MinGitVersion is the oldest git the server will accept. 2.20 (Dec 2018)
// is the baseline for the flag surface our argv builders assume.
var MinGitVersion = gitVersion{2, 20, 0}

// TestedGitVersion is the version CI currently exercises against. Older-but-
// supported gits load a stderr warning because some newer flags/behavior
// may be absent or differ.
var TestedGitVersion = gitVersion{2, 39, 0}

// Server implements pb.SubCommandsServer. ScratchDir is the parent dir under
// which uri/gh-sourced repos are expected to live (one subdir per repo) —
// path-sourced repos resolve to their explicit absolute path instead.
type Server struct {
	pb.UnimplementedSubCommandsServer
	ScratchDir string
}

// New constructs a Server after verifying git is on PATH and new enough.
// Returns an error if git is missing or older than MinGitVersion. Logs a
// stderr warning if the installed version is older than TestedGitVersion.
func New(scratchDir string) (*Server, error) {
	if _, err := probeGit(); err != nil {
		return nil, err
	}
	return &Server{ScratchDir: scratchDir}, nil
}

type gitVersion struct{ major, minor, patch int }

func (v gitVersion) String() string { return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch) }

func (v gitVersion) less(o gitVersion) bool {
	if v.major != o.major {
		return v.major < o.major
	}
	if v.minor != o.minor {
		return v.minor < o.minor
	}
	return v.patch < o.patch
}

func probeGit() (gitVersion, error) {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return gitVersion{}, fmt.Errorf("git not usable on PATH (need >= %s): %w", MinGitVersion, err)
	}
	v, err := parseGitVersion(string(out))
	if err != nil {
		return gitVersion{}, fmt.Errorf("parsing %q: %w", strings.TrimSpace(string(out)), err)
	}
	if v.less(MinGitVersion) {
		return v, fmt.Errorf("git %s is too old; need >= %s", v, MinGitVersion)
	}
	if v.less(TestedGitVersion) {
		fmt.Fprintf(os.Stderr, "subcommands: warning: git %s is older than the tested %s; some flags may behave differently\n", v, TestedGitVersion)
	}
	return v, nil
}

// parseGitVersion pulls X.Y.Z out of strings like
// "git version 2.39.5 (Apple Git-154)". Trailing non-numeric suffixes on
// each field (e.g. "5-rc1") are stripped.
func parseGitVersion(s string) (gitVersion, error) {
	const prefix = "git version "
	i := strings.Index(s, prefix)
	if i < 0 {
		return gitVersion{}, fmt.Errorf("missing %q prefix", prefix)
	}
	rest := s[i+len(prefix):]
	if j := strings.IndexAny(rest, " ("); j >= 0 {
		rest = rest[:j]
	}
	parts := strings.SplitN(strings.TrimSpace(rest), ".", 3)
	if len(parts) < 2 {
		return gitVersion{}, fmt.Errorf("need major.minor in %q", rest)
	}
	out := gitVersion{}
	fields := []*int{&out.major, &out.minor, &out.patch}
	for k, p := range parts {
		end := 0
		for end < len(p) && p[end] >= '0' && p[end] <= '9' {
			end++
		}
		if end == 0 {
			return gitVersion{}, fmt.Errorf("field %d (%q): not numeric", k, parts[k])
		}
		n, err := strconv.Atoi(p[:end])
		if err != nil {
			return gitVersion{}, fmt.Errorf("field %d (%q): %w", k, parts[k], err)
		}
		*fields[k] = n
	}
	return out, nil
}

// run executes `git <sub> args...` inside r's resolved local path and returns
// a populated RepoMsg. Resolution failures land in msg.Errs (gRPC error stays
// nil) so callers always get a structured response.
func (s *Server) run(ctx context.Context, r *pb.Repo, sub string, args ...string) *pb.RepoMsg {
	msg := newMsg(r)
	dir, err := s.resolve(r)
	if err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	s.exec(ctx, dir, msg, append([]string{sub}, args...)...)
	return msg
}

// runMkdir is run() for subcommands like `init` whose target dir may not yet
// exist — it ensures the dir is present before invoking git.
func (s *Server) runMkdir(ctx context.Context, r *pb.Repo, sub string, args ...string) *pb.RepoMsg {
	msg := newMsg(r)
	dir, err := s.resolve(r)
	if err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		msg.Errs = append(msg.Errs, fmt.Sprintf("mkdir %s: %v", dir, err))
		return msg
	}
	s.exec(ctx, dir, msg, append([]string{sub}, args...)...)
	return msg
}

func (s *Server) exec(ctx context.Context, dir string, msg *pb.RepoMsg, args ...string) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	msg.Stdout.Line = append(msg.Stdout.Line, splitLines(stdout.String())...)
	msg.Stderr.Line = append(msg.Stderr.Line, splitLines(stderr.String())...)
	if err != nil {
		msg.Errs = append(msg.Errs, fmt.Sprintf("git %s: %v", strings.Join(args, " "), err))
	}
}

// resolve mirrors importer.Server.resolve (kept local to avoid taking a
// dependency on an internal helper of a sibling package).
func (s *Server) resolve(r *pb.Repo) (string, error) {
	src := r.GetSource()
	if src == nil || src.Source == nil {
		return "", fmt.Errorf("repo missing source")
	}
	switch v := src.Source.(type) {
	case *pb.RepoSource_Uri:
		if v.Uri == "" {
			return "", fmt.Errorf("uri source is empty")
		}
		return filepath.Join(s.ScratchDir, nameFromURI(v.Uri)), nil
	case *pb.RepoSource_Gh:
		gh := v.Gh
		if gh == nil || gh.Owner == "" || gh.Name == "" {
			return "", fmt.Errorf("github source missing owner/name")
		}
		return filepath.Join(s.ScratchDir, gh.Name), nil
	case *pb.RepoSource_Path:
		if v.Path == nil || v.Path.Path == "" {
			return "", fmt.Errorf("path source missing path")
		}
		return filepath.Abs(v.Path.Path)
	}
	return "", fmt.Errorf("unknown source type")
}

func newMsg(r *pb.Repo) *pb.RepoMsg {
	return &pb.RepoMsg{Repo: r, Stdout: &pb.RepoLogs{}, Stderr: &pb.RepoLogs{}}
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// hasFlag reports whether args contains any of the given flag tokens, either
// bare ("-m") or with attached value ("-m=foo"). Used by happy-path defaults
// to avoid clobbering an explicit user flag.
func hasFlag(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f || strings.HasPrefix(a, f+"=") {
				return true
			}
		}
	}
	return false
}

func nameFromURI(uri string) string {
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
