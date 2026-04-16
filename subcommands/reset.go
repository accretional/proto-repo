package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Reset runs `git reset`. With no args, mixed-resets the index to HEAD.
func (s *Server) Reset(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "reset", req.GetArgs()...), nil
}
