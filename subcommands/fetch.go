package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Fetch runs `git fetch`. Defaults to `--all --prune` when no args are given,
// matching the importer's lifecycle-update behavior.
func (s *Server) Fetch(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	args := req.GetArgs()
	if len(args) == 0 {
		args = []string{"--all", "--prune"}
	}
	return s.run(ctx, req.GetRepo(), "fetch", args...), nil
}
