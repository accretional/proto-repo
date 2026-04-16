package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// RangeDiff runs `git range-diff`. Args specify the two commit ranges.
func (s *Server) RangeDiff(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "range-diff", req.GetArgs()...), nil
}
