package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Gc runs `git gc` to clean up loose objects and optimize the repo.
func (s *Server) Gc(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "gc", req.GetArgs()...), nil
}
