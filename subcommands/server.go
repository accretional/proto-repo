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
	"os"
	"strings"

	pb "github.com/accretional/proto-repo/genpb"
	"github.com/accretional/proto-repo/internal/gitexec"
)

// Server implements pb.SubCommandsServer. ScratchDir is the parent dir under
// which uri/gh-sourced repos are expected to live (one subdir per repo) —
// path-sourced repos resolve to their explicit absolute path instead.
type Server struct {
	pb.UnimplementedSubCommandsServer
	ScratchDir string
}

// New constructs a Server after verifying git is on PATH and new enough via
// gitexec.ProbeGit. Returns an error if git is missing or too old; a
// warning is logged to stderr for usable-but-older-than-tested installs.
func New(scratchDir string) (*Server, error) {
	if _, err := gitexec.ProbeGit(); err != nil {
		return nil, err
	}
	return &Server{ScratchDir: scratchDir}, nil
}

// run executes `git <sub> args...` inside r's resolved local path and returns
// a populated RepoMsg. Resolution failures land in msg.Errs (gRPC error stays
// nil) so callers always get a structured response.
func (s *Server) run(ctx context.Context, r *pb.Repo, sub string, args ...string) *pb.RepoMsg {
	msg := gitexec.NewMsg(r)
	rv, err := gitexec.Resolve(s.ScratchDir, r)
	if err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	gitexec.Exec(ctx, rv.Path, msg, append([]string{sub}, args...)...)
	return msg
}

// runMkdir is run() for subcommands like `init` whose target dir may not yet
// exist — it ensures the dir is present before invoking git.
func (s *Server) runMkdir(ctx context.Context, r *pb.Repo, sub string, args ...string) *pb.RepoMsg {
	msg := gitexec.NewMsg(r)
	rv, err := gitexec.Resolve(s.ScratchDir, r)
	if err != nil {
		msg.Errs = append(msg.Errs, err.Error())
		return msg
	}
	if err := os.MkdirAll(rv.Path, 0o755); err != nil {
		msg.Errs = append(msg.Errs, fmt.Sprintf("mkdir %s: %v", rv.Path, err))
		return msg
	}
	gitexec.Exec(ctx, rv.Path, msg, append([]string{sub}, args...)...)
	return msg
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
