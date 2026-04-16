package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Clone runs `git clone <url> <dest>` where url and dest come from resolving
// the request's Repo. User-supplied args go between `clone` and the URL so
// flags like `--depth=1` or `--branch=main` work, but the URL/dest are not
// overridable. Path-source repos are rejected — they're already local.
func (s *Server) Clone(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.runClone(ctx, req.GetRepo(), req.GetArgs()...), nil
}
