package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Shortlog runs `git shortlog`. Defaults to HEAD when given no args because
// `git shortlog` with no commit range reads from stdin and would hang.
func (s *Server) Shortlog(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	args := req.GetArgs()
	if len(args) == 0 {
		args = []string{"HEAD"}
	}
	return s.run(ctx, req.GetRepo(), "shortlog", args...), nil
}
