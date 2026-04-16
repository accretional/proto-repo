// Command ghbackup clones or updates every repo under a GitHub user or org
// into a local directory. Second and later runs are incremental: existing
// checkouts get `git fetch --all --prune` + `git pull --ff-only`, so no
// local work is ever clobbered (and --ff-only refuses divergent histories).
//
// Usage:
//
//	ghbackup --owner <login> --dest <dir> [--include-forks] [--include-archived]
//
// Auth is the gh CLI's token (reads $GH_TOKEN / $GITHUB_TOKEN / `gh auth
// token`). git itself authenticates via whatever credential helper is
// configured — run `gh auth setup-git` once if clones of private repos
// prompt for a password.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	pb "github.com/accretional/proto-repo/genpb"
	"github.com/accretional/proto-repo/importer"
	"github.com/accretional/proto-repo/scan"
	"google.golang.org/grpc"
)

// noopStream stands in for the streaming server grpc normally hands to
// Importer.Download. We're calling the service in-process, so we just
// want to pipe each RepoMsg to the console as it arrives instead of
// routing through a real network stream.
type noopStream struct {
	grpc.ServerStream
	ctx context.Context
	n   int
	ok  int
	bad int
}

func (s *noopStream) Context() context.Context { return s.ctx }

func (s *noopStream) Send(m *pb.RepoMsg) error {
	s.n++
	name := repoName(m.GetRepo())
	if errs := m.GetErrs(); len(errs) > 0 {
		s.bad++
		fmt.Printf("[%3d] FAIL %s — %s\n", s.n, name, strings.Join(errs, "; "))
		return nil
	}
	s.ok++
	fmt.Printf("[%3d] ok   %s\n", s.n, name)
	return nil
}

func repoName(r *pb.Repo) string {
	if src := r.GetSource(); src != nil {
		if uri := src.GetUri(); uri != "" {
			return uri
		}
		if gh := src.GetGh(); gh != nil {
			return gh.GetOwner() + "/" + gh.GetName()
		}
		if p := src.GetPath(); p != nil {
			return p.GetPath()
		}
		if o := src.GetGhOwner(); o != nil {
			return "gh_owner:" + o.GetOwner()
		}
	}
	return "<unknown>"
}

func main() {
	owner := flag.String("owner", "", "GitHub user or org login (required)")
	dest := flag.String("dest", "", "destination directory (required); repos land in <dest>/<name>")
	includeForks := flag.Bool("include-forks", false, "include forked repositories")
	includeArchived := flag.Bool("include-archived", false, "include archived repositories")
	flag.Parse()

	if *owner == "" || *dest == "" {
		fmt.Fprintln(os.Stderr, "ghbackup: --owner and --dest are required")
		flag.Usage()
		os.Exit(2)
	}
	if err := os.MkdirAll(*dest, 0o755); err != nil {
		log.Fatalf("mkdir dest: %v", err)
	}

	token := firstNonEmpty(os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"), scan.TokenFromGHCLI())
	if token == "" {
		fmt.Fprintln(os.Stderr, "ghbackup: warning — no GitHub token found; private repos will be skipped")
	}

	srv, err := importer.New(*dest)
	if err != nil {
		log.Fatalf("importer.New: %v", err)
	}
	srv.Github = scan.NewGithubClient(token)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	req := &pb.RepoList{Repos: []*pb.Repo{{
		Source: &pb.RepoSource{Source: &pb.RepoSource_GhOwner{GhOwner: &pb.GithubOwner{
			Owner: *owner,
			Options: &pb.GithubOptions{
				IncludeForks:    *includeForks,
				IncludeArchived: *includeArchived,
			},
		}}},
	}}}

	fmt.Printf("ghbackup: mirroring %s → %s (forks=%t, archived=%t)\n",
		*owner, *dest, *includeForks, *includeArchived)
	stream := &noopStream{ctx: ctx}
	if err := srv.Download(req, stream); err != nil {
		log.Fatalf("Download: %v", err)
	}
	fmt.Printf("\nghbackup: %d processed — %d ok, %d failed\n", stream.n, stream.ok, stream.bad)
	if stream.bad > 0 {
		os.Exit(1)
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
