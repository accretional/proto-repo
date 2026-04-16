package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Branch runs `git branch`. With no args, lists local branches.
func (s *Server) Branch(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "branch", req.GetArgs()...), nil
}
