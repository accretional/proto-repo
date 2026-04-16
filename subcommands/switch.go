package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Switch runs `git switch`. Args specify the branch to switch to (with -c
// to create).
func (s *Server) Switch(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "switch", req.GetArgs()...), nil
}
