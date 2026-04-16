package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Submodule runs `git submodule`. With no args, prints submodule status.
func (s *Server) Submodule(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "submodule", req.GetArgs()...), nil
}
