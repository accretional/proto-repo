package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Init runs `git init`. Special-cased: creates the resolved directory if it
// doesn't exist yet, since callers commonly Init a brand-new repo.
func (s *Server) Init(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.runMkdir(ctx, req.GetRepo(), "init", req.GetArgs()...), nil
}
