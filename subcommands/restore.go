package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Restore runs `git restore`. With no path args, restores the entire working
// tree (`git restore .`).
func (s *Server) Restore(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	args := req.GetArgs()
	if len(args) == 0 {
		args = []string{"."}
	}
	return s.run(ctx, req.GetRepo(), "restore", args...), nil
}
