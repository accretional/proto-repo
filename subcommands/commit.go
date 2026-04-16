package subcommands

import (
	"context"

	pb "github.com/accretional/proto-repo/genpb"
)

// Commit runs `git commit`. If no -m/--message/--file is given, supplies a
// minimal default so the happy path doesn't open $EDITOR (which would hang
// because we don't attach a tty).
func (s *Server) Commit(ctx context.Context, req *pb.SubCommandReq) (*pb.RepoMsg, error) {
	args := req.GetArgs()
	if !hasFlag(args, "-m", "--message", "-F", "--file", "-C", "--reuse-message") {
		args = append(args, "-m", "commit")
	}
	return s.run(ctx, req.GetRepo(), "commit", args...), nil
}
