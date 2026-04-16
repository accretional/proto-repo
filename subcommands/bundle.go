package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Bundle runs `git bundle`. Args select the verb (create, verify,
// list-heads, unbundle) plus the bundle file and refspec.
func (s *Server) Bundle(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "bundle", req.GetArgs()...), nil
}
