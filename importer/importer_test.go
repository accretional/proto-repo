package importer

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/accretional/proto-repo/genpb"
	"github.com/accretional/proto-repo/internal/gitexec"
	"github.com/accretional/proto-repo/scan"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// seedRepo creates a tiny git repo at dir/<name> and returns its path. The
// resulting repo can be cloned via file:// URL.
func seedRepo(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "add", "."},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "seed"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// startServer wires Server into a bufconn-backed grpc.Server and returns a
// connected client + cleanup. If lister is non-nil it replaces the default
// GitHub client so gh_owner expansion can be driven by tests instead of
// hitting api.github.com.
func startServer(t *testing.T, scratch string, lister ...OwnerLister) (pb.ImporterClient, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	s, err := New(scratch)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(lister) > 0 {
		s.Github = lister[0]
	}
	pb.RegisterImporterServer(srv, s)
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return pb.NewImporterClient(conn), func() {
		conn.Close()
		srv.Stop()
		lis.Close()
	}
}

func collectRepoMsgs(t *testing.T, stream grpc.ServerStreamingClient[pb.RepoMsg]) []*pb.RepoMsg {
	t.Helper()
	var out []*pb.RepoMsg
	for {
		m, err := stream.Recv()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		out = append(out, m)
	}
}

func uriRepo(uri string) *pb.Repo {
	return &pb.Repo{Source: &pb.RepoSource{Source: &pb.RepoSource_Uri{Uri: uri}}}
}

func pathRepo(p string) *pb.Repo {
	return &pb.Repo{Source: &pb.RepoSource{Source: &pb.RepoSource_Path{Path: &pb.RepoPath{Path: p}}}}
}

func TestCloneWherePullZip(t *testing.T) {
	tmp := t.TempDir()
	src := seedRepo(t, filepath.Join(tmp, "src"), "demo")
	scratch := filepath.Join(tmp, "scratch")

	client, stop := startServer(t, scratch)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repos := &pb.RepoList{Repos: []*pb.Repo{uriRepo("file://" + src)}}

	// Clone — should land in <scratch>/demo with a working git checkout.
	cs, err := client.Clone(ctx, repos)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	msgs := collectRepoMsgs(t, cs)
	if len(msgs) != 1 {
		t.Fatalf("clone: expected 1 msg, got %d", len(msgs))
	}
	if errs := msgs[0].GetErrs(); len(errs) > 0 {
		t.Fatalf("clone errs: %v", errs)
	}
	clonedPath := filepath.Join(scratch, "demo")
	if !gitexec.IsGitRepo(clonedPath) {
		t.Fatalf("clone: expected git checkout at %s", clonedPath)
	}

	// Where — should report the same path.
	ws, err := client.Where(ctx, repos)
	if err != nil {
		t.Fatalf("Where: %v", err)
	}
	got, err := ws.Recv()
	if err != nil {
		t.Fatalf("Where recv: %v", err)
	}
	if got.GetPath() != clonedPath {
		t.Errorf("Where path = %q, want %q", got.GetPath(), clonedPath)
	}

	// Add a new commit upstream and Pull — checkout should advance.
	if err := os.WriteFile(filepath.Join(src, "extra.txt"), []byte("more\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-c", "user.email=t@t", "-c", "user.name=t", "add", "."},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "extra"},
	} {
		c := exec.Command("git", args...)
		c.Dir = src
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("upstream commit: %v\n%s", err, out)
		}
	}
	ps, err := client.Pull(ctx, repos)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	pmsgs := collectRepoMsgs(t, ps)
	if len(pmsgs) != 1 || len(pmsgs[0].GetErrs()) > 0 {
		t.Fatalf("pull failed: %+v", pmsgs)
	}
	if _, err := os.Stat(filepath.Join(clonedPath, "extra.txt")); err != nil {
		t.Errorf("pull did not advance checkout: %v", err)
	}

	// Zip — should write an archive containing demo/hello.txt + demo/extra.txt.
	mz, err := client.Zip(ctx, repos)
	if err != nil {
		t.Fatalf("Zip: %v", err)
	}
	if mz.GetNumFiles() < 2 {
		t.Errorf("zip: NumFiles = %d, want >= 2", mz.GetNumFiles())
	}
	if mz.GetSize() <= 0 {
		t.Errorf("zip: Size = %d, want > 0", mz.GetSize())
	}
	zr, err := zip.OpenReader(mz.GetPath())
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"demo/hello.txt", "demo/extra.txt"} {
		if !names[want] {
			t.Errorf("zip missing %q (have %v)", want, names)
		}
	}
	for n := range names {
		if filepath.HasPrefix(n, "demo/.git") {
			t.Errorf("zip should skip .git, but contains %q", n)
		}
	}
}

func TestDownloadIsClonePlusPull(t *testing.T) {
	tmp := t.TempDir()
	src := seedRepo(t, filepath.Join(tmp, "src"), "demo")
	scratch := filepath.Join(tmp, "scratch")

	client, stop := startServer(t, scratch)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repos := &pb.RepoList{Repos: []*pb.Repo{uriRepo("file://" + src)}}

	// First Download = clone.
	s1, err := client.Download(ctx, repos)
	if err != nil {
		t.Fatalf("Download #1: %v", err)
	}
	if msgs := collectRepoMsgs(t, s1); len(msgs) != 1 || len(msgs[0].GetErrs()) > 0 {
		t.Fatalf("Download #1 errs: %+v", msgs)
	}
	if !gitexec.IsGitRepo(filepath.Join(scratch, "demo")) {
		t.Fatalf("Download #1 didn't create checkout")
	}
	// Second Download = pull (no error against unchanged upstream).
	s2, err := client.Download(ctx, repos)
	if err != nil {
		t.Fatalf("Download #2: %v", err)
	}
	if msgs := collectRepoMsgs(t, s2); len(msgs) != 1 || len(msgs[0].GetErrs()) > 0 {
		t.Fatalf("Download #2 errs: %+v", msgs)
	}
}

// seedRepoWithBranch builds on seedRepo by adding a second commit on a named
// branch, then resetting main back to the first commit. Returns (path,
// mainSHA, featureSHA) so tests can assert HEAD landed on a specific rev.
func seedRepoWithBranch(t *testing.T, parent, name, branch string) (string, string, string) {
	t.Helper()
	dir := seedRepo(t, parent, name)
	run := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	mainSHA := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("-c", "user.email=t@t", "-c", "user.name=t", "add", ".")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "feat")
	featureSHA := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("checkout", "-q", "main")
	return dir, mainSHA, featureSHA
}

func headState(t *testing.T, dir string) (sha, ref string) {
	t.Helper()
	sha = strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	ref = strings.TrimSpace(runGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"))
	return
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestCloneBranchOnly(t *testing.T) {
	tmp := t.TempDir()
	src, _, featureSHA := seedRepoWithBranch(t, filepath.Join(tmp, "src"), "demo", "feature")
	scratch := filepath.Join(tmp, "scratch")
	client, stop := startServer(t, scratch)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	r := uriRepo("file://" + src)
	r.Branch = "feature"
	stream, err := client.Clone(ctx, &pb.RepoList{Repos: []*pb.Repo{r}})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	msgs := collectRepoMsgs(t, stream)
	if len(msgs) != 1 || len(msgs[0].GetErrs()) > 0 {
		t.Fatalf("clone errs: %+v", msgs)
	}
	clonedPath := filepath.Join(scratch, "demo")
	sha, ref := headState(t, clonedPath)
	if ref != "feature" {
		t.Errorf("HEAD ref = %q, want feature", ref)
	}
	if sha != featureSHA {
		t.Errorf("HEAD sha = %q, want %q", sha, featureSHA)
	}
}

func TestCloneCommitOnly(t *testing.T) {
	tmp := t.TempDir()
	src, mainSHA, _ := seedRepoWithBranch(t, filepath.Join(tmp, "src"), "demo", "feature")
	scratch := filepath.Join(tmp, "scratch")
	client, stop := startServer(t, scratch)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	r := uriRepo("file://" + src)
	r.Commit = mainSHA
	stream, err := client.Clone(ctx, &pb.RepoList{Repos: []*pb.Repo{r}})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	msgs := collectRepoMsgs(t, stream)
	if len(msgs) != 1 || len(msgs[0].GetErrs()) > 0 {
		t.Fatalf("clone errs: %+v", msgs)
	}
	clonedPath := filepath.Join(scratch, "demo")
	sha, ref := headState(t, clonedPath)
	if ref != "HEAD" {
		t.Errorf("HEAD ref = %q, want HEAD (detached)", ref)
	}
	if sha != mainSHA {
		t.Errorf("HEAD sha = %q, want %q", sha, mainSHA)
	}
}

// TestCloneCommitWinsOverBranch verifies Commit takes precedence when both
// are set: HEAD ends up detached at Commit, not on Branch. The old code did
// a wasted branch checkout first, which could also surface spurious errors.
func TestCloneCommitWinsOverBranch(t *testing.T) {
	tmp := t.TempDir()
	src, mainSHA, featureSHA := seedRepoWithBranch(t, filepath.Join(tmp, "src"), "demo", "feature")
	scratch := filepath.Join(tmp, "scratch")
	client, stop := startServer(t, scratch)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	r := uriRepo("file://" + src)
	r.Branch = "feature"
	r.Commit = mainSHA
	stream, err := client.Clone(ctx, &pb.RepoList{Repos: []*pb.Repo{r}})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	msgs := collectRepoMsgs(t, stream)
	if len(msgs) != 1 || len(msgs[0].GetErrs()) > 0 {
		t.Fatalf("clone errs: %+v", msgs)
	}
	clonedPath := filepath.Join(scratch, "demo")
	sha, ref := headState(t, clonedPath)
	if sha != mainSHA {
		t.Errorf("HEAD sha = %q, want %q (commit wins over branch)", sha, mainSHA)
	}
	if ref != "HEAD" {
		t.Errorf("HEAD ref = %q, want HEAD (detached)", ref)
	}
	if sha == featureSHA {
		t.Errorf("HEAD landed on feature sha %q — Branch should have been ignored", featureSHA)
	}
}

func TestWherePathSourceReturnsItself(t *testing.T) {
	tmp := t.TempDir()
	src := seedRepo(t, filepath.Join(tmp, "src"), "demo")
	scratch := filepath.Join(tmp, "scratch")

	client, stop := startServer(t, scratch)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Where(ctx, &pb.RepoList{Repos: []*pb.Repo{pathRepo(src)}})
	if err != nil {
		t.Fatalf("Where: %v", err)
	}
	got, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	if got.GetPath() != src {
		t.Errorf("Where(path) = %q, want %q", got.GetPath(), src)
	}
}

func ghRepo(owner, name string) *pb.Repo {
	return &pb.Repo{Source: &pb.RepoSource{Source: &pb.RepoSource_Gh{Gh: &pb.GithubRepo{Owner: owner, Name: name}}}}
}

// TestWhereGhSourceResolves exercises the gh-source branch of Resolve
// without any network I/O — Where returns the on-disk path the clone
// would land at, which is <scratch>/<name>.
func TestWhereGhSourceResolves(t *testing.T) {
	tmp := t.TempDir()
	scratch := filepath.Join(tmp, "scratch")
	client, stop := startServer(t, scratch)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Where(ctx, &pb.RepoList{Repos: []*pb.Repo{ghRepo("octocat", "hello")}})
	if err != nil {
		t.Fatalf("Where: %v", err)
	}
	got, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	want := filepath.Join(scratch, "hello")
	if got.GetPath() != want {
		t.Errorf("Where(gh) = %q, want %q", got.GetPath(), want)
	}
}

// TestCloneOverExisting confirms that cloning twice is a no-op on the
// second call (not an error, not a re-clone) — importer's clone() treats
// an existing .git as "already cloned".
func TestCloneOverExisting(t *testing.T) {
	tmp := t.TempDir()
	src := seedRepo(t, filepath.Join(tmp, "src"), "demo")
	scratch := filepath.Join(tmp, "scratch")
	client, stop := startServer(t, scratch)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repos := &pb.RepoList{Repos: []*pb.Repo{uriRepo("file://" + src)}}
	for i := 0; i < 2; i++ {
		stream, err := client.Clone(ctx, repos)
		if err != nil {
			t.Fatalf("Clone #%d: %v", i+1, err)
		}
		msgs := collectRepoMsgs(t, stream)
		if len(msgs) != 1 || len(msgs[0].GetErrs()) > 0 {
			t.Fatalf("Clone #%d errs: %+v", i+1, msgs)
		}
	}
	got := runGit(t, filepath.Join(scratch, "demo"), "log", "--oneline")
	if strings.Count(got, "\n") != 1 {
		t.Errorf("expected 1 commit after double-clone, got log:\n%s", got)
	}
}

// TestPullOnMissingCheckout verifies Pull returns a structured error when
// the target path isn't a git checkout — it should not try to run git and
// it should surface the condition via msg.Errs rather than a gRPC error.
func TestPullOnMissingCheckout(t *testing.T) {
	tmp := t.TempDir()
	scratch := filepath.Join(tmp, "scratch")
	client, stop := startServer(t, scratch)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.Pull(ctx, &pb.RepoList{Repos: []*pb.Repo{uriRepo("file:///nonexistent/nope")}})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	msgs := collectRepoMsgs(t, stream)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 msg, got %d", len(msgs))
	}
	errs := msgs[0].GetErrs()
	if len(errs) == 0 {
		t.Fatalf("expected error about missing checkout, got none")
	}
	if !strings.Contains(errs[0], "not a git checkout") {
		t.Errorf("error = %q, want substring %q", errs[0], "not a git checkout")
	}
}

// TestZipWithPathSource verifies Zip treats path-source repos like
// already-present checkouts: files get archived under the basename,
// .git is skipped.
func TestZipWithPathSource(t *testing.T) {
	tmp := t.TempDir()
	src := seedRepo(t, filepath.Join(tmp, "src"), "demo")
	scratch := filepath.Join(tmp, "scratch")
	client, stop := startServer(t, scratch)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mz, err := client.Zip(ctx, &pb.RepoList{Repos: []*pb.Repo{pathRepo(src)}})
	if err != nil {
		t.Fatalf("Zip: %v", err)
	}
	if mz.GetNumFiles() < 1 {
		t.Errorf("NumFiles = %d, want >= 1", mz.GetNumFiles())
	}
	zr, err := zip.OpenReader(mz.GetPath())
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	var hasHello bool
	for _, f := range zr.File {
		if f.Name == "demo/hello.txt" {
			hasHello = true
		}
		if strings.HasPrefix(f.Name, "demo/.git") {
			t.Errorf("zip should skip .git, contains %q", f.Name)
		}
	}
	if !hasHello {
		t.Errorf("zip missing demo/hello.txt")
	}
}

// TestMultiRepoStreams confirms every streaming RPC emits one message per
// input Repo, in order — the current code iterates req.GetRepos() and sends
// per-repo, but nothing guarded against a regression to batch-mode.
func TestMultiRepoStreams(t *testing.T) {
	tmp := t.TempDir()
	srcA := seedRepo(t, filepath.Join(tmp, "src"), "alpha")
	srcB := seedRepo(t, filepath.Join(tmp, "src"), "beta")
	scratch := filepath.Join(tmp, "scratch")
	client, stop := startServer(t, scratch)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repos := &pb.RepoList{Repos: []*pb.Repo{
		uriRepo("file://" + srcA),
		uriRepo("file://" + srcB),
	}}

	cs, err := client.Clone(ctx, repos)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	cmsgs := collectRepoMsgs(t, cs)
	if len(cmsgs) != 2 {
		t.Fatalf("Clone msgs = %d, want 2", len(cmsgs))
	}
	for i, want := range []string{"alpha", "beta"} {
		got := cmsgs[i].GetRepo().GetSource().GetUri()
		if !strings.HasSuffix(got, "/"+want) {
			t.Errorf("Clone msg[%d] uri = %q, want suffix %q", i, got, "/"+want)
		}
	}

	ps, err := client.Pull(ctx, repos)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	pmsgs := collectRepoMsgs(t, ps)
	if len(pmsgs) != 2 {
		t.Fatalf("Pull msgs = %d, want 2", len(pmsgs))
	}
	for i, m := range pmsgs {
		if len(m.GetErrs()) > 0 {
			t.Errorf("Pull msg[%d] errs: %v", i, m.GetErrs())
		}
	}

	ws, err := client.Where(ctx, repos)
	if err != nil {
		t.Fatalf("Where: %v", err)
	}
	var paths []string
	for {
		m, err := ws.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Where recv: %v", err)
		}
		paths = append(paths, m.GetPath())
	}
	if len(paths) != 2 {
		t.Fatalf("Where paths = %d, want 2", len(paths))
	}
	if !strings.HasSuffix(paths[0], "/alpha") || !strings.HasSuffix(paths[1], "/beta") {
		t.Errorf("Where order wrong: %v", paths)
	}

	mz, err := client.Zip(ctx, repos)
	if err != nil {
		t.Fatalf("Zip: %v", err)
	}
	zr, err := zip.OpenReader(mz.GetPath())
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"alpha/hello.txt", "beta/hello.txt"} {
		if !names[want] {
			t.Errorf("zip missing %q (have %v)", want, names)
		}
	}
}

// ---- gh_owner expansion ---------------------------------------------------

// fakeLister returns canned responses from an in-memory map keyed by owner.
// Set err to fail the next ListRepos call unconditionally.
type fakeLister struct {
	byOwner map[string][]scan.Repo
	err     error
}

func (f *fakeLister) ListRepos(_ context.Context, owner string) ([]scan.Repo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byOwner[owner], nil
}

func ghOwnerRepo(owner string, opts *pb.GithubOptions) *pb.Repo {
	return &pb.Repo{Source: &pb.RepoSource{Source: &pb.RepoSource_GhOwner{
		GhOwner: &pb.GithubOwner{Owner: owner, Options: opts},
	}}}
}

// TestWhereGhOwnerExpansion confirms a single gh_owner input expands to one
// RepoPath per listed repo, using the CloneURL to derive the local path
// via <scratch>/<name-from-url>.
func TestWhereGhOwnerExpansion(t *testing.T) {
	tmp := t.TempDir()
	scratch := filepath.Join(tmp, "scratch")
	lister := &fakeLister{byOwner: map[string][]scan.Repo{
		"octo": {
			{Owner: "octo", Name: "one", CloneURL: "https://github.com/octo/one.git"},
			{Owner: "octo", Name: "two", CloneURL: "https://github.com/octo/two.git"},
		},
	}}
	client, stop := startServer(t, scratch, lister)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Where(ctx, &pb.RepoList{Repos: []*pb.Repo{ghOwnerRepo("octo", nil)}})
	if err != nil {
		t.Fatalf("Where: %v", err)
	}
	var got []string
	for {
		m, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		got = append(got, m.GetPath())
	}
	want := []string{filepath.Join(scratch, "one"), filepath.Join(scratch, "two")}
	if len(got) != len(want) {
		t.Fatalf("got %d paths, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("paths[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestExpandOwnersFiltersForksAndArchived verifies the default behavior
// (both flags off) excludes forks and archived repos; the opt-in variants
// include them. Exercised end-to-end via Where.
func TestExpandOwnersFiltersForksAndArchived(t *testing.T) {
	tmp := t.TempDir()
	scratch := filepath.Join(tmp, "scratch")
	lister := &fakeLister{byOwner: map[string][]scan.Repo{
		"acme": {
			{Name: "plain", CloneURL: "https://github.com/acme/plain.git"},
			{Name: "forky", CloneURL: "https://github.com/acme/forky.git", Fork: true},
			{Name: "stale", CloneURL: "https://github.com/acme/stale.git", Archived: true},
		},
	}}
	client, stop := startServer(t, scratch, lister)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	countPaths := func(repo *pb.Repo) []string {
		stream, err := client.Where(ctx, &pb.RepoList{Repos: []*pb.Repo{repo}})
		if err != nil {
			t.Fatalf("Where: %v", err)
		}
		var paths []string
		for {
			m, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("recv: %v", err)
			}
			paths = append(paths, filepath.Base(m.GetPath()))
		}
		return paths
	}

	if got := countPaths(ghOwnerRepo("acme", nil)); len(got) != 1 || got[0] != "plain" {
		t.Errorf("default expansion = %v, want just [plain]", got)
	}
	if got := countPaths(ghOwnerRepo("acme", &pb.GithubOptions{IncludeForks: true})); len(got) != 2 {
		t.Errorf("include_forks expansion = %v, want 2 (plain+forky)", got)
	}
	if got := countPaths(ghOwnerRepo("acme", &pb.GithubOptions{IncludeArchived: true})); len(got) != 2 {
		t.Errorf("include_archived expansion = %v, want 2 (plain+stale)", got)
	}
	if got := countPaths(ghOwnerRepo("acme", &pb.GithubOptions{IncludeForks: true, IncludeArchived: true})); len(got) != 3 {
		t.Errorf("both-flags expansion = %v, want 3", got)
	}
}

// TestDownloadGhOwnerExpansion drives a full expand-then-clone cycle
// against local file:// repos — the fake lister returns CloneURLs pointing
// at seeded upstreams on disk, so the server clones without touching the
// network.
func TestDownloadGhOwnerExpansion(t *testing.T) {
	tmp := t.TempDir()
	srcA := seedRepo(t, filepath.Join(tmp, "src"), "alpha")
	srcB := seedRepo(t, filepath.Join(tmp, "src"), "beta")
	scratch := filepath.Join(tmp, "scratch")
	lister := &fakeLister{byOwner: map[string][]scan.Repo{
		"team": {
			{Owner: "team", Name: "alpha", CloneURL: "file://" + srcA},
			{Owner: "team", Name: "beta", CloneURL: "file://" + srcB},
		},
	}}
	client, stop := startServer(t, scratch, lister)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := client.Download(ctx, &pb.RepoList{Repos: []*pb.Repo{ghOwnerRepo("team", nil)}})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	msgs := collectRepoMsgs(t, stream)
	if len(msgs) != 2 {
		t.Fatalf("got %d msgs, want 2", len(msgs))
	}
	for i, m := range msgs {
		if len(m.GetErrs()) > 0 {
			t.Errorf("msg[%d] errs: %v", i, m.GetErrs())
		}
	}
	for _, name := range []string{"alpha", "beta"} {
		if !gitexec.IsGitRepo(filepath.Join(scratch, name)) {
			t.Errorf("expected checkout at %s", filepath.Join(scratch, name))
		}
	}
}

// TestExpandOwnersListingFailure confirms a listing error surfaces as a
// RepoMsg with errs set (not a gRPC error), leaving the stream intact so
// other repos in the request still proceed.
func TestExpandOwnersListingFailure(t *testing.T) {
	tmp := t.TempDir()
	src := seedRepo(t, filepath.Join(tmp, "src"), "demo")
	scratch := filepath.Join(tmp, "scratch")
	lister := &fakeLister{err: fmt.Errorf("rate limited")}
	client, stop := startServer(t, scratch, lister)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repos := &pb.RepoList{Repos: []*pb.Repo{
		ghOwnerRepo("broken", nil),
		uriRepo("file://" + src),
	}}
	stream, err := client.Clone(ctx, repos)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	msgs := collectRepoMsgs(t, stream)
	if len(msgs) != 2 {
		t.Fatalf("got %d msgs, want 2", len(msgs))
	}
	if errs := msgs[0].GetErrs(); len(errs) == 0 || !strings.Contains(errs[0], "rate limited") {
		t.Errorf("expected rate-limit error in first msg, got %v", errs)
	}
	if errs := msgs[1].GetErrs(); len(errs) > 0 {
		t.Errorf("second repo shouldn't have errored: %v", errs)
	}
}
