package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Maintenance runs `git maintenance`. Defaults to `run` when no verb is
// supplied, since the bare command prints usage and exits non-zero.
func (s *Server) Maintenance(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	args := req.GetArgs()
	if len(args) == 0 {
		args = []string{"run"}
	}
	return s.run(ctx, req.GetRepo(), "maintenance", args...), nil
}
