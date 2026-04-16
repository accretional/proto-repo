package importer

import (
	"archive/zip"
	"context"
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
// connected client + cleanup.
func startServer(t *testing.T, scratch string) (pb.ImporterClient, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	s, err := New(scratch)
	if err != nil {
		t.Fatalf("New: %v", err)
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

