package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Tag runs `git tag`. With no args, lists tags.
func (s *Server) Tag(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	return s.run(ctx, req.GetRepo(), "tag", req.GetArgs()...), nil
}
