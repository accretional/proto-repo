package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Add runs `git add`. Happy path with no args stages everything (`git add .`).
func (s *Server) Add(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	args := req.GetArgs()
	if len(args) == 0 {
		args = []string{"."}
	}
	return s.run(ctx, req.GetRepo(), "add", args...), nil
}
