package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Push runs `git push`. With no args, pushes the current branch to its
// upstream (errors if no upstream is configured).
func (s *Server) Push(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "push", req.GetArgs()...), nil
}
