package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Maintenance runs `git maintenance`. Args choose the verb (run, register,
// unregister, start, stop) — required since there is no usable default.
func (s *Server) Maintenance(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "maintenance", req.GetArgs()...), nil
}
