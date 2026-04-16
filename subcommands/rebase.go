package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Rebase runs `git rebase`. Args specify upstream and optional branch.
func (s *Server) Rebase(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "rebase", req.GetArgs()...), nil
}
