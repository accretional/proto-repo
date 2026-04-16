package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Log runs `git log`. With no args, caps output at 20 commits to keep the
// happy-path response from blowing up on long histories.
func (s *Server) Log(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	args := req.GetArgs()
	if len(args) == 0 {
		args = []string{"-n", "20"}
	}
	return s.run(ctx, req.GetRepo(), "log", args...), nil
}
