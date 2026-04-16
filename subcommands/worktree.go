package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Worktree runs `git worktree`. Defaults to `list` when no verb is supplied,
// since the bare command errors out asking for one.
func (s *Server) Worktree(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	args := req.GetArgs()
	if len(args) == 0 {
		args = []string{"list"}
	}
	return s.run(ctx, req.GetRepo(), "worktree", args...), nil
}
