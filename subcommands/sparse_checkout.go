package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// SparseCheckout runs `git sparse-checkout`. Defaults to `list` when no args
// are supplied, since the bare command errors out asking for a verb.
func (s *Server) SparseCheckout(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	args := req.GetArgs()
	if len(args) == 0 {
		args = []string{"list"}
	}
	return s.run(ctx, req.GetRepo(), "sparse-checkout", args...), nil
}
