package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Backfill runs `git backfill` to download missing objects in a partial
// clone. Happy path takes no args.
func (s *Server) Backfill(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "backfill", req.GetArgs()...), nil
}
