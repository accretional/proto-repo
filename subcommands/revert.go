package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Revert runs `git revert`. Args specify the commit(s) to revert.
func (s *Server) Revert(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "revert", req.GetArgs()...), nil
}
