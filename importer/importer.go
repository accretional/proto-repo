// Package importer implements the repo.Importer gRPC service: it clones,
// pulls, locates, and zips repositories on the local filesystem.
package importer

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pb "github.com/accretional/proto-repo/genpb"
	"google.golang.org/grpc"
)

// Server implements pb.ImporterServer. ScratchDir is the parent directory
// under which URI/GitHub-sourced repos are cloned (one subdir per repo).
type Server struct {
	pb.UnimplementedImporterServer
	ScratchDir string
}

func New(scratchDir string) *Server { return &Server{ScratchDir: scratchDir} }

// Download ensures each repo is present on disk: clones if missing,
// otherwise fetches + fast-forwards. Streams one RepoMsg per input.
func (s *Server) Download(req *pb.RepoList, stream grpc.ServerStreamingServer[pb.RepoMsg]) error {
	for _, r := range req.GetRepos() {
		if err := stream.Send(s.fetch(stream.Context(), r)); err != nil {
			return err
		}
	}
	return nil
}

// Clone clones each repo. If a checkout already exists, it's reported as a
// no-op rather than re-cloned.
func (s *Server) Clone(req *pb.RepoList, stream grpc.ServerStreamingServer[pb.RepoMsg]) error {
	for _, r := range req.GetRepos() {
		if err := stream.Send(s.clone(stream.Context(), r)); err != nil {
			return err
		}
	}
	return nil
}

// Pull runs `git fetch --all --prune` + `git pull --ff-only` against each
// repo's existing checkout. Errors if a checkout is missing.
func (s *Server) Pull(req *pb.RepoList, stream grpc.ServerStreamingServer[pb.RepoMsg]) error {
	for _, r := range req.GetRepos() {
		if err := stream.Send(s.pull(stream.Context(), r)); err != nil {
			return err
		}
	}
	return nil
}

// Where returns the local on-disk path each repo would resolve to.
// Path may be empty if the source can't be resolved.
func (s *Server) Where(req *pb.RepoList, stream grpc.ServerStreamingServer[pb.RepoPath]) error {
	for _, r := range req.GetRepos() {
		path, _ := s.localPath(r)
		if err := stream.Send(&pb.RepoPath{Path: path}); err != nil {
			return err
		}
	}
	return nil
}

// Zip writes a single zip archive containing every input repo's working tree
// (skipping .git) and returns its path, total size, and file count.
func (s *Server) Zip(ctx context.Context, req *pb.RepoList) (*pb.MultiRepoZip, error) {
	if err := os.MkdirAll(s.ScratchDir, 0o755); err != nil {
		return nil, fmt.Errorf("zip: mkdir scratch: %w", err)
	}
	zipPath := filepath.Join(s.ScratchDir, "_repos.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		return nil, fmt.Errorf("zip: create %s: %w", zipPath, err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)

	var nFiles int32
	for _, r := range req.GetRepos() {
		path, err := s.localPath(r)
		if err != nil || path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		root := filepath.Base(path)
		walkErr := filepath.Walk(path, func(p string, info os.FileInfo, werr error) error {
			if werr != nil {
				return nil
			}
			if info.IsDir() {
				if info.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(path, p)
			if err != nil {
				return nil
			}
			w, err := zw.Create(filepath.ToSlash(filepath.Join(root, rel)))
			if err != nil {
				return err
			}
			src, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer src.Close()
			if _, err := io.Copy(w, src); err != nil {
				return err
			}
			nFiles++
			return nil
		})
		if walkErr != nil {
			zw.Close()
			return nil, fmt.Errorf("zip: walk %s: %w", path, walkErr)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("zip: close writer: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("zip: stat: %w", err)
	}
	return &pb.MultiRepoZip{
		Repos:    req,
		Path:     zipPath,
		Size:     st.Size(),
		NumFiles: nFiles,
	}, nil
}

// --- internal helpers (also handy for tests) ---

// resolved is everything we need to act on a single Repo.
type resolved struct {
	cloneURL string // empty for path-sourced repos
	name     string // local subdir name under ScratchDir
	explicit string // non-empty if the repo's source is an explicit local path
}

func (s *Server) resolve(r *pb.Repo) (resolved, error) {
	src := r.GetSource()
	if src == nil || src.Source == nil {
		return resolved{}, fmt.Errorf("repo missing source")
	}
	switch v := src.Source.(type) {
	case *pb.RepoSource_Uri:
		if v.Uri == "" {
			return resolved{}, fmt.Errorf("uri source is empty")
		}
		return resolved{cloneURL: v.Uri, name: nameFromURI(v.Uri)}, nil
	case *pb.RepoSource_Gh:
		gh := v.Gh
		if gh == nil || gh.Owner == "" || gh.Name == "" {
			return resolved{}, fmt.Errorf("github source missing owner/name")
		}
		return resolved{
			cloneURL: fmt.Sprintf("https://github.com/%s/%s.git", gh.Owner, gh.Name),
			name:     gh.Name,
		}, nil
	case *pb.RepoSource_Path:
		if v.Path == nil || v.Path.Path == "" {
			return resolved{}, fmt.Errorf("path source missing path")
		}
		abs, err := filepath.Abs(v.Path.Path)
		if err != nil {
			return resolved{}, err
		}
		return resolved{name: filepath.Base(abs), explicit: abs}, nil
	}
	return resolved{}, fmt.Errorf("unknown source type")
}

func (s *Server) localPath(r *pb.Repo) (string, error) {
	rv, err := s.resolve(r)
	if err != nil {
		return "", err
	}
	if rv.explicit != "" {
		return rv.explicit, nil
	}
	return filepath.Join(s.ScratchDir, rv.name), nil
}

func (s *Server) fetch(ctx context.Context, r *pb.Repo) *pb.RepoMsg {
	msg := newMsg(r)
	rv, err := s.resolve(r)
	if err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	if rv.explicit != "" {
		msg.Stdout.Line = append(msg.Stdout.Line, "local path source: "+rv.explicit)
		return msg
	}
	dest := filepath.Join(s.ScratchDir, rv.name)
	if isGitRepo(dest) {
		s.runInto(ctx, dest, msg, "git", "fetch", "--all", "--prune")
		s.runInto(ctx, dest, msg, "git", "pull", "--ff-only")
	} else {
		if err := os.MkdirAll(s.ScratchDir, 0o755); err != nil {
			msg.Errs = append(msg.Errs, err.Error())
			return msg
		}
		s.runInto(ctx, "", msg, "git", "clone", rv.cloneURL, dest)
	}
	s.applyBranchCommit(ctx, dest, r, msg)
	return msg
}

func (s *Server) clone(ctx context.Context, r *pb.Repo) *pb.RepoMsg {
	msg := newMsg(r)
	rv, err := s.resolve(r)
	if err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	if rv.explicit != "" {
		msg.Stdout.Line = append(msg.Stdout.Line, "local path source: "+rv.explicit)
		return msg
	}
	dest := filepath.Join(s.ScratchDir, rv.name)
	if isGitRepo(dest) {
		msg.Stdout.Line = append(msg.Stdout.Line, "already cloned: "+dest)
		s.applyBranchCommit(ctx, dest, r, msg)
		return msg
	}
	if err := os.MkdirAll(s.ScratchDir, 0o755); err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	s.runInto(ctx, "", msg, "git", "clone", rv.cloneURL, dest)
	s.applyBranchCommit(ctx, dest, r, msg)
	return msg
}

func (s *Server) pull(ctx context.Context, r *pb.Repo) *pb.RepoMsg {
	msg := newMsg(r)
	path, err := s.localPath(r)
	if err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	if !isGitRepo(path) {
		msg.Errs = append(msg.Errs, "not a git checkout: "+path)
		return msg
	}
	s.runInto(ctx, path, msg, "git", "fetch", "--all", "--prune")
	s.runInto(ctx, path, msg, "git", "pull", "--ff-only")
	s.applyBranchCommit(ctx, path, r, msg)
	return msg
}

func (s *Server) applyBranchCommit(ctx context.Context, dir string, r *pb.Repo, msg *pb.RepoMsg) {
	if r.GetBranch() != "" {
		s.runInto(ctx, dir, msg, "git", "checkout", r.GetBranch())
	}
	if r.GetCommit() != "" {
		s.runInto(ctx, dir, msg, "git", "checkout", r.GetCommit())
	}
}

func (s *Server) runInto(ctx context.Context, dir string, msg *pb.RepoMsg, name string, args ...string) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	msg.Stdout.Line = append(msg.Stdout.Line, splitLines(stdout.String())...)
	msg.Stderr.Line = append(msg.Stderr.Line, splitLines(stderr.String())...)
	if err != nil {
		msg.Errs = append(msg.Errs, fmt.Sprintf("%s %s: %v", name, strings.Join(args, " "), err))
	}
}

func newMsg(r *pb.Repo) *pb.RepoMsg {
	return &pb.RepoMsg{Repo: r, Stdout: &pb.RepoLogs{}, Stderr: &pb.RepoLogs{}}
}

func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// nameFromURI extracts a sensible local subdirectory name from a clone URL,
// e.g. "https://github.com/foo/bar.git" -> "bar", "file:///tmp/x" -> "x".
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
