package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Stash runs `git stash`. With no args, equivalent to `git stash push`.
func (s *Server) Stash(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "stash", req.GetArgs()...), nil
}
