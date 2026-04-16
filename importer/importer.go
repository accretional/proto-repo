// Package importer implements the repo.Importer gRPC service: it clones,
// pulls, locates, and zips repositories on the local filesystem.
package importer

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	pb "github.com/accretional/proto-repo/genpb"
	"github.com/accretional/proto-repo/internal/gitexec"
	"google.golang.org/grpc"
)

// Server implements pb.ImporterServer. ScratchDir is the parent directory
// under which URI/GitHub-sourced repos are cloned (one subdir per repo).
type Server struct {
	pb.UnimplementedImporterServer
	ScratchDir string
}

// New constructs a Server after verifying git is on PATH and new enough.
// Mirrors subcommands.New — fails fast if git is missing or too old.
func New(scratchDir string) (*Server, error) {
	if _, err := gitexec.ProbeGit(); err != nil {
		return nil, err
	}
	return &Server{ScratchDir: scratchDir}, nil
}

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
		path := ""
		if rv, err := gitexec.Resolve(s.ScratchDir, r); err == nil {
			path = rv.Path
		}
		if err := stream.Send(&pb.RepoPath{Path: path}); err != nil {
			return err
		}
	}
	return nil
}

// Zip writes a zip archive containing every input repo's working tree
// (skipping .git). Each call creates a uniquely-named file under ScratchDir
// so concurrent Zip requests don't race.
func (s *Server) Zip(ctx context.Context, req *pb.RepoList) (*pb.MultiRepoZip, error) {
	if err := os.MkdirAll(s.ScratchDir, 0o755); err != nil {
		return nil, fmt.Errorf("zip: mkdir scratch: %w", err)
	}
	f, err := os.CreateTemp(s.ScratchDir, "repos-*.zip")
	if err != nil {
		return nil, fmt.Errorf("zip: create temp: %w", err)
	}
	zipPath := f.Name()
	defer f.Close()
	zw := zip.NewWriter(f)

	var nFiles int32
	for _, r := range req.GetRepos() {
		rv, err := gitexec.Resolve(s.ScratchDir, r)
		if err != nil || rv.Path == "" {
			continue
		}
		if _, err := os.Stat(rv.Path); err != nil {
			continue
		}
		root := filepath.Base(rv.Path)
		walkErr := filepath.Walk(rv.Path, func(p string, info os.FileInfo, werr error) error {
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
			rel, err := filepath.Rel(rv.Path, p)
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
			return nil, fmt.Errorf("zip: walk %s: %w", rv.Path, walkErr)
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

func (s *Server) fetch(ctx context.Context, r *pb.Repo) *pb.RepoMsg {
	msg := gitexec.NewMsg(r)
	rv, err := gitexec.Resolve(s.ScratchDir, r)
	if err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	if rv.Explicit {
		msg.Stdout.Line = append(msg.Stdout.Line, "local path source: "+rv.Path)
		return msg
	}
	if gitexec.IsGitRepo(rv.Path) {
		gitexec.Exec(ctx, rv.Path, msg, "fetch", "--all", "--prune")
		gitexec.Exec(ctx, rv.Path, msg, "pull", "--ff-only")
	} else {
		if err := os.MkdirAll(s.ScratchDir, 0o755); err != nil {
			msg.Errs = append(msg.Errs, err.Error())
			return msg
		}
		gitexec.Exec(ctx, "", msg, "clone", rv.CloneURL, rv.Path)
	}
	applyBranchCommit(ctx, rv.Path, r, msg)
	return msg
}

func (s *Server) clone(ctx context.Context, r *pb.Repo) *pb.RepoMsg {
	msg := gitexec.NewMsg(r)
	rv, err := gitexec.Resolve(s.ScratchDir, r)
	if err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	if rv.Explicit {
		msg.Stdout.Line = append(msg.Stdout.Line, "local path source: "+rv.Path)
		return msg
	}
	if gitexec.IsGitRepo(rv.Path) {
		msg.Stdout.Line = append(msg.Stdout.Line, "already cloned: "+rv.Path)
		applyBranchCommit(ctx, rv.Path, r, msg)
		return msg
	}
	if err := os.MkdirAll(s.ScratchDir, 0o755); err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	gitexec.Exec(ctx, "", msg, "clone", rv.CloneURL, rv.Path)
	applyBranchCommit(ctx, rv.Path, r, msg)
	return msg
}

func (s *Server) pull(ctx context.Context, r *pb.Repo) *pb.RepoMsg {
	msg := gitexec.NewMsg(r)
	rv, err := gitexec.Resolve(s.ScratchDir, r)
	if err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	if !gitexec.IsGitRepo(rv.Path) {
		msg.Errs = append(msg.Errs, "not a git checkout: "+rv.Path)
		return msg
	}
	gitexec.Exec(ctx, rv.Path, msg, "fetch", "--all", "--prune")
	gitexec.Exec(ctx, rv.Path, msg, "pull", "--ff-only")
	applyBranchCommit(ctx, rv.Path, r, msg)
	return msg
}

func applyBranchCommit(ctx context.Context, dir string, r *pb.Repo, msg *pb.RepoMsg) {
	if r.GetBranch() != "" {
		gitexec.Exec(ctx, dir, msg, "checkout", r.GetBranch())
	}
	if r.GetCommit() != "" {
		gitexec.Exec(ctx, dir, msg, "checkout", r.GetCommit())
	}
}
