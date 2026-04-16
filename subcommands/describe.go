package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Describe runs `git describe`. With no args, describes HEAD.
func (s *Server) Describe(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "describe", req.GetArgs()...), nil
}
