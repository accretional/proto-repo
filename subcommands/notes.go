package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Notes runs `git notes`. With no args, lists existing notes.
func (s *Server) Notes(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "notes", req.GetArgs()...), nil
}
