package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Clean runs `git clean`. Defaults to --dry-run when no flags are supplied,
// because the unguarded form deletes untracked files irrecoverably.
func (s *Server) Clean(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	args := req.GetArgs()
	if len(args) == 0 {
		args = []string{"--dry-run"}
	}
	return s.run(ctx, req.GetRepo(), "clean", args...), nil
}
