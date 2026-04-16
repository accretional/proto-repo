package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Rm runs `git rm`. Args specify paths to remove from working tree + index.
func (s *Server) Rm(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "rm", req.GetArgs()...), nil
}
