package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Diff runs `git diff`. With no args, shows unstaged changes.
func (s *Server) Diff(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "diff", req.GetArgs()...), nil
}
