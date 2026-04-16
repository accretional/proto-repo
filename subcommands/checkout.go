package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Checkout runs `git checkout`. Args specify the ref or paths to check out.
func (s *Server) Checkout(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "checkout", req.GetArgs()...), nil
}
