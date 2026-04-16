package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Archive runs `git archive`. Args are passed through verbatim — callers
// must at minimum supply a tree-ish (e.g. "HEAD"); use --output / --format
// to control destination and packaging.
func (s *Server) Archive(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "archive", req.GetArgs()...), nil
}
