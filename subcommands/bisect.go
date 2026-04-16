package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Bisect runs `git bisect`. Args choose the bisect verb (start, good, bad,
// reset, log, …) and any refs.
func (s *Server) Bisect(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "bisect", req.GetArgs()...), nil
}
