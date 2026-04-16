package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Status runs `git status`. With no args, prints working-tree status.
func (s *Server) Status(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "status", req.GetArgs()...), nil
}
