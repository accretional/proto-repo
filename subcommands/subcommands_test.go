package subcommands

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/accretional/proto-repo/genpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func startServer(t *testing.T, scratch string) (pb.SubCommandsClient, func()) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterSubCommandsServer(srv, New(scratch))
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
	return pb.NewSubCommandsClient(conn), func() {
		conn.Close()
		srv.Stop()
		lis.Close()
	}
}

func pathRepo(p string) *pb.Repo {
	return &pb.Repo{Source: &pb.RepoSource{Source: &pb.RepoSource_Path{Path: &pb.RepoPath{Path: p}}}}
}

// TestHappyPathWorkflow walks a fresh repo through init → write file → add →
// commit → branch → tag → log → status → describe → diff, validating each
// RPC's basic happy path. git author/committer identity is supplied via env
// vars so Commit doesn't need -c flags.
func TestHappyPathWorkflow(t *testing.T) {
	for _, kv := range [][2]string{
		{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@t"},
		{"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@t"},
	} {
		t.Setenv(kv[0], kv[1])
	}

	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "demo")
	r := pathRepo(repoDir)

	client, stop := startServer(t, tmp)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Init creates the directory and runs `git init` inside it.
	if msg, err := client.Init(ctx, &pb.SubCommandReq{Repo: r, Args: []string{"-b", "main"}}); err != nil {
		t.Fatalf("Init: %v", err)
	} else if errs := msg.GetErrs(); len(errs) > 0 {
		t.Fatalf("Init errs: %v", errs)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		t.Fatalf("Init didn't create .git: %v", err)
	}

	// Write a file, then Add (no args → "."), then Commit (no -m → default).
	if err := os.WriteFile(filepath.Join(repoDir, "hello.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if msg, err := client.Add(ctx, &pb.SubCommandReq{Repo: r}); err != nil {
		t.Fatalf("Add: %v", err)
	} else if errs := msg.GetErrs(); len(errs) > 0 {
		t.Fatalf("Add errs: %v", errs)
	}
	if msg, err := client.Commit(ctx, &pb.SubCommandReq{Repo: r}); err != nil {
		t.Fatalf("Commit: %v", err)
	} else if errs := msg.GetErrs(); len(errs) > 0 {
		t.Fatalf("Commit errs: %v\nstderr: %v", errs, msg.GetStderr().GetLine())
	}

	// Branch listing should include "main".
	bmsg, err := client.Branch(ctx, &pb.SubCommandReq{Repo: r})
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if !linesContain(bmsg.GetStdout().GetLine(), "main") {
		t.Errorf("Branch stdout missing 'main': %v", bmsg.GetStdout().GetLine())
	}

	// Annotated tag v1 (so Describe can find it without --tags), then list.
	if _, err := client.Tag(ctx, &pb.SubCommandReq{Repo: r, Args: []string{"-a", "v1", "-m", "v1"}}); err != nil {
		t.Fatalf("Tag create: %v", err)
	}
	tmsg, err := client.Tag(ctx, &pb.SubCommandReq{Repo: r})
	if err != nil {
		t.Fatalf("Tag list: %v", err)
	}
	if !linesContain(tmsg.GetStdout().GetLine(), "v1") {
		t.Errorf("Tag list missing 'v1': %v", tmsg.GetStdout().GetLine())
	}

	// Log should mention the default commit message "commit".
	lmsg, err := client.Log(ctx, &pb.SubCommandReq{Repo: r})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if !linesContain(lmsg.GetStdout().GetLine(), "commit") {
		t.Errorf("Log stdout missing 'commit': %v", lmsg.GetStdout().GetLine())
	}

	// Status on a clean tree.
	smsg, err := client.Status(ctx, &pb.SubCommandReq{Repo: r})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(smsg.GetErrs()) > 0 {
		t.Errorf("Status errs: %v", smsg.GetErrs())
	}

	// Describe should report v1 since we just tagged HEAD.
	dmsg, err := client.Describe(ctx, &pb.SubCommandReq{Repo: r})
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if !linesContain(dmsg.GetStdout().GetLine(), "v1") {
		t.Errorf("Describe missing 'v1': %v", dmsg.GetStdout().GetLine())
	}

	// Diff on a clean tree should produce no output and no error.
	if dm, err := client.Diff(ctx, &pb.SubCommandReq{Repo: r}); err != nil {
		t.Fatalf("Diff: %v", err)
	} else if len(dm.GetErrs()) > 0 {
		t.Errorf("Diff errs: %v", dm.GetErrs())
	}
}

// TestCleanDefaultsToDryRun guards the safety default in clean.go: the bare
// command must NOT actually delete files.
func TestCleanDefaultsToDryRun(t *testing.T) {
	for _, kv := range [][2]string{
		{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@t"},
		{"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@t"},
	} {
		t.Setenv(kv[0], kv[1])
	}
	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "demo")
	r := pathRepo(repoDir)

	client, stop := startServer(t, tmp)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := client.Init(ctx, &pb.SubCommandReq{Repo: r}); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(repoDir, "untracked.txt")
	if err := os.WriteFile(junk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Clean(ctx, &pb.SubCommandReq{Repo: r}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(junk); err != nil {
		t.Errorf("Clean default removed untracked file but should have been --dry-run only: %v", err)
	}
}

// TestResolveErrors flow through msg.Errs (no gRPC error).
func TestResolveErrors(t *testing.T) {
	tmp := t.TempDir()
	client, stop := startServer(t, tmp)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg, err := client.Status(ctx, &pb.SubCommandReq{Repo: &pb.Repo{}})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(msg.GetErrs()) == 0 {
		t.Errorf("expected resolve error in msg.Errs, got none")
	}
}

func linesContain(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}
