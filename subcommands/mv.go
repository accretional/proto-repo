package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Mv runs `git mv`. Args specify source(s) and destination.
func (s *Server) Mv(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "mv", req.GetArgs()...), nil
}
