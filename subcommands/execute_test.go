package subcommands

// Per-subcommand smoke tests for the Execute RPC: each subtest builds a
// typed Subcommand, calls client.Execute through the bufconn gRPC stack, and
// asserts on msg.Errs. Goal is to confirm every dispatch case (35 oneof
// branches plus the verb-based stash) reaches git without a builder bug.

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

// ---- helpers --------------------------------------------------------------

func execEnv(t *testing.T) {
	t.Helper()
	for _, kv := range [][2]string{
		{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@t"},
		{"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@t"},
	} {
		t.Setenv(kv[0], kv[1])
	}
}

// execClient builds a per-test gRPC client backed by a fresh server scratch
// dir. Returns the client, a 30s context, and the scratch root.
func execClient(t *testing.T) (pb.SubCommandsClient, context.Context, string) {
	t.Helper()
	scratch := t.TempDir()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	s, err := New(scratch)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pb.RegisterSubCommandsServer(srv, s)
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
	t.Cleanup(func() { conn.Close(); srv.Stop(); lis.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return pb.NewSubCommandsClient(conn), ctx, scratch
}

// initCommitRepo bootstraps a repo with one committed file at <scratch>/demo
// using only the Execute RPC.
func initCommitRepo(t *testing.T) (pb.SubCommandsClient, context.Context, *pb.Repo, string) {
	t.Helper()
	execEnv(t)
	client, ctx, scratch := execClient(t)
	repoDir := filepath.Join(scratch, "demo")
	r := pathRepo(repoDir)

	mustExec(t, client, ctx, "init", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Init{Init: &pb.GitInitArguments{InitialBranch: "main"}}})
	if err := os.WriteFile(filepath.Join(repoDir, "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustExec(t, client, ctx, "add", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Add{Add: &pb.GitAddArguments{Pathspec: &pb.Pathspec{Pathspec: []string{"."}}}}})
	mustExec(t, client, ctx, "commit", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Commit{Commit: &pb.GitCommitArguments{Message: &pb.MessageSource{Message: []string{"init"}}}}})
	return client, ctx, r, repoDir
}

func mustExec(t *testing.T, c pb.SubCommandsClient, ctx context.Context, label string, sub *pb.Subcommand) *pb.RepoMsg {
	t.Helper()
	msg, err := c.Execute(ctx, sub)
	if err != nil {
		t.Fatalf("%s: gRPC err: %v", label, err)
	}
	if errs := msg.GetErrs(); len(errs) > 0 {
		t.Fatalf("%s: msg.Errs: %v\nstderr: %v", label, errs, msg.GetStderr().GetLine())
	}
	return msg
}

// execReachedGit asserts the Execute call returned without gRPC error and
// that any Errs entries are git-execution errors (not builder/dispatch
// failures). Used for subcommands whose happy path requires state we won't
// set up (e.g., cherry-pick --abort needs a pick in progress).
func execReachedGit(t *testing.T, c pb.SubCommandsClient, ctx context.Context, label string, sub *pb.Subcommand) *pb.RepoMsg {
	t.Helper()
	msg, err := c.Execute(ctx, sub)
	if err != nil {
		t.Fatalf("%s: gRPC err: %v", label, err)
	}
	for _, e := range msg.GetErrs() {
		if !strings.HasPrefix(e, "git ") {
			t.Fatalf("%s: builder/dispatch error (didn't reach git): %v", label, e)
		}
	}
	return msg
}

// ---- per-subcommand smoke tests ------------------------------------------

func TestExecuteInit(t *testing.T) {
	execEnv(t)
	client, ctx, scratch := execClient(t)
	r := pathRepo(filepath.Join(scratch, "fresh"))
	mustExec(t, client, ctx, "init", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Init{Init: &pb.GitInitArguments{InitialBranch: "main", Quiet: pb.OptBool_OPT_BOOL_TRUE}}})
	if _, err := os.Stat(filepath.Join(scratch, "fresh", ".git")); err != nil {
		t.Fatalf(".git missing: %v", err)
	}
}

func TestExecuteAdd(t *testing.T) {
	client, ctx, r, dir := initCommitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustExec(t, client, ctx, "add", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Add{Add: &pb.GitAddArguments{
			Verbose:  pb.OptBool_OPT_BOOL_TRUE,
			Pathspec: &pb.Pathspec{Pathspec: []string{"new.txt"}},
		}}})
}

func TestExecuteArchive(t *testing.T) {
	client, ctx, r, dir := initCommitRepo(t)
	out := filepath.Join(dir, "head.tar")
	mustExec(t, client, ctx, "archive", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Archive{Archive: &pb.GitArchiveArguments{
			Format: "tar", Output: out, TreeIsh: "HEAD",
		}}})
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("archive output missing: %v", err)
	}
}

func TestExecuteBackfill(t *testing.T) {
	// Backfill only works on partial clones; a normal repo errors out. We
	// just want to confirm dispatch reached git.
	client, ctx, r, _ := initCommitRepo(t)
	execReachedGit(t, client, ctx, "backfill", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Backfill{Backfill: &pb.GitBackfillArguments{}}})
}

func TestExecuteBisect(t *testing.T) {
	// `bisect log` errors when no bisect is in progress; that's fine — we
	// just need to confirm the verb dispatch built `bisect log`.
	client, ctx, r, _ := initCommitRepo(t)
	execReachedGit(t, client, ctx, "bisect", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Bisect{Bisect: &pb.GitBisectArguments{
			Verb: &pb.GitBisectArguments_Log{Log: &pb.BisectLog{}},
		}}})
}

func TestExecuteBranch(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	msg := mustExec(t, client, ctx, "branch", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Branch{Branch: &pb.GitBranchArguments{
			Action: pb.GitBranchArguments_ACTION_LIST,
		}}})
	if !linesContain(msg.GetStdout().GetLine(), "main") {
		t.Errorf("branch list missing 'main': %v", msg.GetStdout().GetLine())
	}
}

func TestExecuteBundle(t *testing.T) {
	client, ctx, r, dir := initCommitRepo(t)
	bundlePath := filepath.Join(dir, "out.bundle")
	mustExec(t, client, ctx, "bundle", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Bundle{Bundle: &pb.GitBundleArguments{
			Verb: &pb.GitBundleArguments_Create{Create: &pb.BundleCreate{
				File: bundlePath, RevListArgs: []string{"--all"},
			}},
		}}})
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("bundle file missing: %v", err)
	}
}

func TestExecuteCheckout(t *testing.T) {
	// `checkout HEAD -- README` is a no-op on a clean tree.
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "checkout", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Checkout{Checkout: &pb.GitCheckoutArguments{
			StartPoint: "HEAD",
			Pathspec:   &pb.Pathspec{Pathspec: []string{"README"}},
		}}})
}

func TestExecuteCherryPick(t *testing.T) {
	// CherryPick --abort with no pick in progress errors but proves dispatch.
	client, ctx, r, _ := initCommitRepo(t)
	execReachedGit(t, client, ctx, "cherry-pick", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_CherryPick{CherryPick: &pb.GitCherryPickArguments{
			Control: pb.GitCherryPickArguments_CONTROL_ABORT,
		}}})
}

func TestExecuteClean(t *testing.T) {
	client, ctx, r, dir := initCommitRepo(t)
	junk := filepath.Join(dir, "junk.txt")
	if err := os.WriteFile(junk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// argvClean injects --dry-run when the caller expresses no destructive
	// intent. An empty Clean request must therefore NOT delete the untracked
	// file — same safety contract as the string-arg Clean RPC.
	mustExec(t, client, ctx, "clean", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Clean{Clean: &pb.GitCleanArguments{}}})
	if _, err := os.Stat(junk); err != nil {
		t.Errorf("structured Clean removed junk despite no --force: %v", err)
	}
}

func TestExecuteCommit(t *testing.T) {
	// Already exercised by initCommitRepo; here we test --amend.
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "commit-amend", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Commit{Commit: &pb.GitCommitArguments{
			Amend:        pb.OptBool_OPT_BOOL_TRUE,
			AllowEmpty:   pb.OptBool_OPT_BOOL_TRUE,
			Message:      &pb.MessageSource{Message: []string{"amended"}},
		}}})
}

func TestExecuteDescribe(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	msg := mustExec(t, client, ctx, "describe", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Describe{Describe: &pb.GitDescribeArguments{
			Always: pb.OptBool_OPT_BOOL_TRUE,
		}}})
	if len(msg.GetStdout().GetLine()) == 0 {
		t.Error("describe --always should emit at least the abbreviated SHA")
	}
}

func TestExecuteDiff(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "diff", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Diff{Diff: &pb.GitDiffArguments{
			Formatting: &pb.DiffFormatting{Stat: pb.OptBool_OPT_BOOL_TRUE},
		}}})
}

func TestExecuteGc(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "gc", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Gc{Gc: &pb.GitGcArguments{Quiet: pb.OptBool_OPT_BOOL_TRUE}}})
}

func TestExecuteLog(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	msg := mustExec(t, client, ctx, "log", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Log{Log: &pb.GitLogArguments{Oneline: pb.OptBool_OPT_BOOL_TRUE}}})
	if !linesContain(msg.GetStdout().GetLine(), "init") {
		t.Errorf("log missing 'init': %v", msg.GetStdout().GetLine())
	}
}

func TestExecuteMaintenance(t *testing.T) {
	// `maintenance run --task=gc --quiet` is a fast no-op on a tiny repo.
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "maintenance", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Maintenance{Maintenance: &pb.GitMaintenanceArguments{
			Verb:  pb.GitMaintenanceArguments_VERB_RUN,
			Task:  []string{"gc"},
			Quiet: pb.OptBool_OPT_BOOL_TRUE,
		}}})
}

func TestExecuteMerge(t *testing.T) {
	// merge --abort with no merge in progress reaches git but errors.
	client, ctx, r, _ := initCommitRepo(t)
	execReachedGit(t, client, ctx, "merge", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Merge{Merge: &pb.GitMergeArguments{
			Control: pb.GitMergeArguments_CONTROL_ABORT,
		}}})
}

func TestExecuteMv(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "mv", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Mv{Mv: &pb.GitMvArguments{
			Sources:     []string{"README"},
			Destination: "README.md",
		}}})
}

func TestExecuteNotes(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "notes-add", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Notes{Notes: &pb.GitNotesArguments{
			Verb:    pb.GitNotesArguments_VERB_ADD,
			Force:   pb.OptBool_OPT_BOOL_TRUE,
			Message: &pb.MessageSource{Message: []string{"reviewed"}},
			Objects: []string{"HEAD"},
		}}})
	mustExec(t, client, ctx, "notes-list", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Notes{Notes: &pb.GitNotesArguments{
			Verb: pb.GitNotesArguments_VERB_LIST,
		}}})
}

func TestExecutePush(t *testing.T) {
	// Spin up a bare repo as the "remote" and push our main branch into it.
	execEnv(t)
	client, ctx, scratch := execClient(t)
	bare := filepath.Join(scratch, "remote.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	mustExec(t, client, ctx, "init-bare", &pb.Subcommand{Repo: pathRepo(bare),
		Args: &pb.Subcommand_Init{Init: &pb.GitInitArguments{
			Bare: pb.OptBool_OPT_BOOL_TRUE, InitialBranch: "main",
		}}})

	// Source repo with one commit.
	srcDir := filepath.Join(scratch, "src")
	src := pathRepo(srcDir)
	mustExec(t, client, ctx, "init-src", &pb.Subcommand{Repo: src,
		Args: &pb.Subcommand_Init{Init: &pb.GitInitArguments{InitialBranch: "main"}}})
	if err := os.WriteFile(filepath.Join(srcDir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustExec(t, client, ctx, "add-src", &pb.Subcommand{Repo: src,
		Args: &pb.Subcommand_Add{Add: &pb.GitAddArguments{Pathspec: &pb.Pathspec{Pathspec: []string{"."}}}}})
	mustExec(t, client, ctx, "commit-src", &pb.Subcommand{Repo: src,
		Args: &pb.Subcommand_Commit{Commit: &pb.GitCommitArguments{Message: &pb.MessageSource{Message: []string{"init"}}}}})

	mustExec(t, client, ctx, "push", &pb.Subcommand{Repo: src,
		Args: &pb.Subcommand_Push{Push: &pb.GitPushArguments{
			Repository: bare,
			Refspec:    []string{"main:main"},
		}}})
}

func TestExecuteRangeDiff(t *testing.T) {
	// Make two commits, then range-diff a range against itself — empty diff,
	// no error.
	client, ctx, r, dir := initCommitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "b"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustExec(t, client, ctx, "add2", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Add{Add: &pb.GitAddArguments{Pathspec: &pb.Pathspec{Pathspec: []string{"."}}}}})
	mustExec(t, client, ctx, "commit2", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Commit{Commit: &pb.GitCommitArguments{Message: &pb.MessageSource{Message: []string{"two"}}}}})
	mustExec(t, client, ctx, "range-diff", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_RangeDiff{RangeDiff: &pb.GitRangeDiffArguments{
			Ranges: []string{"HEAD~1..HEAD", "HEAD~1..HEAD"},
		}}})
}

func TestExecuteRebase(t *testing.T) {
	// rebase --abort with no rebase in progress reaches git but errors.
	client, ctx, r, _ := initCommitRepo(t)
	execReachedGit(t, client, ctx, "rebase", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Rebase{Rebase: &pb.GitRebaseArguments{
			Control: pb.GitRebaseArguments_CONTROL_ABORT,
		}}})
}

func TestExecuteReset(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "reset", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Reset_{Reset_: &pb.GitResetArguments{
			Mode:   pb.GitResetArguments_MODE_MIXED,
			Commit: "HEAD",
		}}})
}

func TestExecuteRestore(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	// Restore README from HEAD — no-op, succeeds.
	mustExec(t, client, ctx, "restore", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Restore{Restore: &pb.GitRestoreArguments{
			Source:   "HEAD",
			Worktree: pb.OptBool_OPT_BOOL_TRUE,
			Pathspec: &pb.Pathspec{Pathspec: []string{"README"}},
		}}})
}

func TestExecuteRevert(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	execReachedGit(t, client, ctx, "revert", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Revert{Revert: &pb.GitRevertArguments{
			Control: pb.GitRevertArguments_CONTROL_ABORT,
		}}})
}

func TestExecuteRm(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "rm", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Rm{Rm: &pb.GitRmArguments{
			Cached:   pb.OptBool_OPT_BOOL_TRUE,
			Pathspec: &pb.Pathspec{Pathspec: []string{"README"}},
		}}})
}

func TestExecuteShortlog(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "shortlog", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Shortlog{Shortlog: &pb.GitShortlogArguments{
			Numbered: pb.OptBool_OPT_BOOL_TRUE,
			Summary:  pb.OptBool_OPT_BOOL_TRUE,
			Range:    "HEAD",
		}}})
}

func TestExecuteShow(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	msg := mustExec(t, client, ctx, "show", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Show{Show: &pb.GitShowArguments{
			Oneline: pb.OptBool_OPT_BOOL_TRUE,
			Objects: []string{"HEAD"},
		}}})
	if !linesContain(msg.GetStdout().GetLine(), "init") {
		t.Errorf("show missing 'init': %v", msg.GetStdout().GetLine())
	}
}

func TestExecuteSparseCheckout(t *testing.T) {
	// `sparse-checkout disable` is a safe no-op on a fresh repo.
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "sparse-checkout", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_SparseCheckout{SparseCheckout: &pb.GitSparseCheckoutArguments{
			Verb: pb.GitSparseCheckoutArguments_VERB_DISABLE,
		}}})
}

func TestExecuteStash(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	// list on empty stash succeeds with no output
	mustExec(t, client, ctx, "stash", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Stash{Stash: &pb.GitStashArguments{
			Verb: &pb.GitStashArguments_List{List: &pb.StashList{}},
		}}})
}

func TestExecuteStatus(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "status", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Status{Status: &pb.GitStatusArguments{
			Format: pb.StatusFormat_STATUS_FORMAT_PORCELAIN_V1,
			Branch: pb.OptBool_OPT_BOOL_TRUE,
		}}})
}

func TestExecuteSubmodule(t *testing.T) {
	// `submodule status` on a repo with no submodules → empty, no error.
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "submodule", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Submodule{Submodule: &pb.GitSubmoduleArguments{
			Verb: pb.GitSubmoduleArguments_VERB_STATUS,
		}}})
}

func TestExecuteSwitch(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "switch-create", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Switch{Switch: &pb.GitSwitchArguments{
			Create: "topic",
		}}})
	mustExec(t, client, ctx, "switch-back", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Switch{Switch: &pb.GitSwitchArguments{
			Branch: "main",
		}}})
}

func TestExecuteTag(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	mustExec(t, client, ctx, "tag-create", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Tag{Tag: &pb.GitTagArguments{
			Annotate: pb.OptBool_OPT_BOOL_TRUE,
			Message:  &pb.MessageSource{Message: []string{"v1"}},
			TagName:  "v1",
		}}})
	msg := mustExec(t, client, ctx, "tag-list", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Tag{Tag: &pb.GitTagArguments{
			Action: pb.GitTagArguments_ACTION_LIST,
		}}})
	if !linesContain(msg.GetStdout().GetLine(), "v1") {
		t.Errorf("tag list missing v1: %v", msg.GetStdout().GetLine())
	}
}

func TestExecuteWorktree(t *testing.T) {
	client, ctx, r, _ := initCommitRepo(t)
	msg := mustExec(t, client, ctx, "worktree-list", &pb.Subcommand{Repo: r,
		Args: &pb.Subcommand_Worktree{Worktree: &pb.GitWorktreeArguments{
			Verb: pb.GitWorktreeArguments_VERB_LIST,
		}}})
	if len(msg.GetStdout().GetLine()) == 0 {
		t.Error("worktree list should print at least the main worktree")
	}
}
