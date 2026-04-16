package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// CherryPick runs `git cherry-pick`. Args specify the commit(s) to apply.
func (s *Server) CherryPick(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "cherry-pick", req.GetArgs()...), nil
}
