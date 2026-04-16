package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Pull runs `git pull`. Defaults to `--ff-only` when no args are given so a
// bare call can't silently create a merge commit the caller didn't ask for.
func (s *Server) Pull(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	args := req.GetArgs()
	if !hasFlag(args, "--ff", "--no-ff", "--ff-only", "--rebase", "--no-rebase") {
		args = append([]string{"--ff-only"}, args...)
	}
	return s.run(ctx, req.GetRepo(), "pull", args...), nil
}
