package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Merge runs `git merge`. Args specify the branch(es) to merge in.
func (s *Server) Merge(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "merge", req.GetArgs()...), nil
}
