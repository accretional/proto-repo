// Command ghbackup clones or updates GitHub repositories into a local
// directory. Second and later runs are incremental: existing checkouts
// get `git fetch --all --prune` + `git pull --ff-only`, so no local work
// is ever clobbered (and --ff-only refuses divergent histories).
//
// Usage:
//
//	ghbackup --owner <login> --dest <dir> [--include-forks] [--include-archived]
//	ghbackup --list-file <path> --dest <dir>
//
// Two source modes:
//
//   - --owner: expand one GitHub user/org to every repo they own.
//   - --list-file: read an explicit list of repos (owner/name or github
//     URL, one per line; '#' introduces a comment).
//
// Cron-friendly flags:
//
//   - --quiet: suppress per-repo "ok" lines; failures + summary remain.
//   - --lock-file <path>: acquire an exclusive lock before running; if the
//     file already exists another run is assumed to be active and ghbackup
//     exits 0 without doing work (so cron stays quiet on overlap).
//
// Auth is the gh CLI's token (reads $GH_TOKEN / $GITHUB_TOKEN / `gh auth
// token`). git itself authenticates via whatever credential helper is
// configured — run `gh auth setup-git` once if clones of private repos
// prompt for a password.
package main

import (
	"bufio"
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
	ctx   context.Context
	quiet bool
	n     int
	ok    int
	bad   int
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
	if !s.quiet {
		fmt.Printf("[%3d] ok   %s\n", s.n, name)
	}
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
	owner := flag.String("owner", "", "GitHub user or org login; expands to every repo they own")
	listFile := flag.String("list-file", "", "path to a file listing repos (owner/name or URL, one per line, '#' comments)")
	dest := flag.String("dest", "", "destination directory (required); repos land in <dest>/<name>")
	includeForks := flag.Bool("include-forks", false, "include forked repositories (only with --owner)")
	includeArchived := flag.Bool("include-archived", false, "include archived repositories (only with --owner)")
	quiet := flag.Bool("quiet", false, "suppress per-repo ok lines; keep failures + final summary")
	lockFile := flag.String("lock-file", "", "acquire this path as an exclusive lock; exit 0 if another run holds it")
	flag.Parse()

	if *dest == "" {
		fmt.Fprintln(os.Stderr, "ghbackup: --dest is required")
		flag.Usage()
		os.Exit(2)
	}
	if (*owner == "") == (*listFile == "") {
		fmt.Fprintln(os.Stderr, "ghbackup: exactly one of --owner or --list-file is required")
		flag.Usage()
		os.Exit(2)
	}
	if *listFile != "" && (*includeForks || *includeArchived) {
		fmt.Fprintln(os.Stderr, "ghbackup: --include-forks/--include-archived only apply to --owner")
		os.Exit(2)
	}

	if *lockFile != "" {
		release, err := acquireLock(*lockFile)
		if err != nil {
			// Held by another run — intentional no-op so cron stays silent on overlap.
			fmt.Fprintf(os.Stderr, "ghbackup: lock %s held (%v); skipping\n", *lockFile, err)
			os.Exit(0)
		}
		defer release()
	}

	if err := os.MkdirAll(*dest, 0o755); err != nil {
		log.Fatalf("mkdir dest: %v", err)
	}

	req, err := buildRequest(*owner, *listFile, *includeForks, *includeArchived)
	if err != nil {
		log.Fatalf("ghbackup: %v", err)
	}

	token := firstNonEmpty(os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"), scan.TokenFromGHCLI())
	if token == "" && *owner != "" {
		fmt.Fprintln(os.Stderr, "ghbackup: warning — no GitHub token found; private repos will be skipped")
	}

	srv, err := importer.New(*dest)
	if err != nil {
		log.Fatalf("importer.New: %v", err)
	}
	srv.Github = scan.NewGithubClient(token)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch {
	case *owner != "":
		fmt.Printf("ghbackup: mirroring %s → %s (forks=%t, archived=%t)\n",
			*owner, *dest, *includeForks, *includeArchived)
	case *listFile != "":
		fmt.Printf("ghbackup: mirroring %d repos from %s → %s\n", len(req.Repos), *listFile, *dest)
	}

	stream := &noopStream{ctx: ctx, quiet: *quiet}
	if err := srv.Download(req, stream); err != nil {
		log.Fatalf("Download: %v", err)
	}
	fmt.Printf("\nghbackup: %d processed — %d ok, %d failed\n", stream.n, stream.ok, stream.bad)
	if stream.bad > 0 {
		os.Exit(1)
	}
}

// buildRequest shapes a RepoList from exactly one of the two source modes.
// Caller has already enforced the mutual-exclusion invariant.
func buildRequest(owner, listFile string, includeForks, includeArchived bool) (*pb.RepoList, error) {
	if owner != "" {
		return &pb.RepoList{Repos: []*pb.Repo{{
			Source: &pb.RepoSource{Source: &pb.RepoSource_GhOwner{GhOwner: &pb.GithubOwner{
				Owner: owner,
				Options: &pb.GithubOptions{
					IncludeForks:    includeForks,
					IncludeArchived: includeArchived,
				},
			}}},
		}}}, nil
	}
	f, err := os.Open(listFile)
	if err != nil {
		return nil, fmt.Errorf("open list file: %w", err)
	}
	defer f.Close()
	repos, err := parseList(f)
	if err != nil {
		return nil, fmt.Errorf("parse list file %s: %w", listFile, err)
	}
	if len(repos) == 0 {
		return nil, fmt.Errorf("list file %s contained no repos", listFile)
	}
	return &pb.RepoList{Repos: repos}, nil
}

// parseList reads owner/name entries from r. Each non-empty, non-comment
// line must match "owner/name" or "https://github.com/owner/name[.git]".
// Inline '#' comments and trailing whitespace are stripped.
func parseList(r *os.File) ([]*pb.Repo, error) {
	var out []*pb.Repo
	sc := bufio.NewScanner(r)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		owner, name, err := parseRepoRef(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		out = append(out, &pb.Repo{Source: &pb.RepoSource{
			Source: &pb.RepoSource_Gh{Gh: &pb.GithubRepo{Owner: owner, Name: name}},
		}})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// parseRepoRef accepts "owner/name" or a full github URL and returns the
// owner/name pair. Trailing ".git" and slashes are stripped.
func parseRepoRef(s string) (string, string, error) {
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimSuffix(s, "/")
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "git@github.com:"} {
		s = strings.TrimPrefix(s, prefix)
	}
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected owner/name or github URL, got %q", s)
	}
	return parts[0], parts[1], nil
}

// acquireLock creates path with O_EXCL and returns a release func that
// removes it. Fails if the file already exists — we treat that as "another
// run is holding the lock" without probing PIDs. Stale locks require manual
// cleanup, which is the right tradeoff for a tool that hurts nothing by
// skipping a run but would cause real damage if two ran concurrently on
// the same dest.
func acquireLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Close()
	return func() { _ = os.Remove(path) }, nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
