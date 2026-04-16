package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Show runs `git show`. With no args, shows HEAD.
func (s *Server) Show(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "show", req.GetArgs()...), nil
}
