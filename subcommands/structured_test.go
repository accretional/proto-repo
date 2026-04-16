package subcommands

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	pb "github.com/accretional/proto-repo/genpb"
)

// ---- primitive helpers ----------------------------------------------------

func TestAddTriBool(t *testing.T) {
	cases := []struct {
		name string
		v    pb.OptBool
		on   string
		off  string
		want []string
	}{
		{"unspecified emits nothing", pb.OptBool_OPT_BOOL_UNSPECIFIED, "--ff", "--no-ff", nil},
		{"true emits on", pb.OptBool_OPT_BOOL_TRUE, "--ff", "--no-ff", []string{"--ff"}},
		{"false emits off", pb.OptBool_OPT_BOOL_FALSE, "--ff", "--no-ff", []string{"--no-ff"}},
		{"false with empty off is a no-op", pb.OptBool_OPT_BOOL_FALSE, "--foo", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := addTriBool(nil, c.on, c.off, c.v)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestAddEqIntAndAttachedInt(t *testing.T) {
	// present=false → nothing; present=true → "flag=N" / "flagN".
	none := &pb.OptInt{Present: false, Value: 42}
	zero := &pb.OptInt{Present: true, Value: 0}
	three := &pb.OptInt{Present: true, Value: 3}

	if got := addEqInt(nil, "--unified", none); got != nil {
		t.Errorf("none should emit nothing, got %v", got)
	}
	if got := addEqInt(nil, "--unified", zero); !reflect.DeepEqual(got, []string{"--unified=0"}) {
		t.Errorf("zero should emit --unified=0, got %v", got)
	}
	if got := addAttachedInt(nil, "-U", three); !reflect.DeepEqual(got, []string{"-U3"}) {
		t.Errorf("three should emit -U3, got %v", got)
	}
	if got := addEqInt(nil, "--unified", nil); got != nil {
		t.Errorf("nil pointer should emit nothing, got %v", got)
	}
}

func TestAddPathspec(t *testing.T) {
	// With pathspecs: `-- a b`. With file: `--pathspec-from-file=list`.
	p := &pb.Pathspec{
		Pathspec:           []string{"a", "b"},
		PathspecFromFile:   "list",
		PathspecFileNul:    pb.OptBool_OPT_BOOL_TRUE,
	}
	got := addPathspec(nil, p)
	want := []string{"--pathspec-from-file=list", "--pathspec-file-nul", "--", "a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}

	// Empty pathspec → no `--` sentinel emitted.
	if got := addPathspec(nil, &pb.Pathspec{}); got != nil {
		t.Errorf("empty pathspec should emit nothing, got %v", got)
	}
}

func TestAddMessageSource(t *testing.T) {
	// Multiple -m values get one -m each.
	m := &pb.MessageSource{Message: []string{"first", "second"}, File: "MSG"}
	got := addMessageSource(nil, m)
	want := []string{"-m", "first", "-m", "second", "-F", "MSG"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestAddGpgSign(t *testing.T) {
	// sign=TRUE with key → --gpg-sign=<key>; without key → bare --gpg-sign.
	if got := addGpgSign(nil, &pb.GpgSign{Sign: pb.OptBool_OPT_BOOL_TRUE, KeyId: "ABCD"}); !reflect.DeepEqual(got, []string{"--gpg-sign=ABCD"}) {
		t.Errorf("keyed sign: %v", got)
	}
	if got := addGpgSign(nil, &pb.GpgSign{Sign: pb.OptBool_OPT_BOOL_TRUE}); !reflect.DeepEqual(got, []string{"--gpg-sign"}) {
		t.Errorf("bare sign: %v", got)
	}
	if got := addGpgSign(nil, &pb.GpgSign{Sign: pb.OptBool_OPT_BOOL_FALSE}); !reflect.DeepEqual(got, []string{"--no-gpg-sign"}) {
		t.Errorf("no-sign: %v", got)
	}
	if got := addGpgSign(nil, nil); got != nil {
		t.Errorf("nil GpgSign: %v", got)
	}
}

// ---- per-subcommand argv builders ----------------------------------------

func TestArgvAdd(t *testing.T) {
	cases := []struct {
		name string
		in   *pb.GitAddArguments
		want []string
	}{
		{"nil returns empty", nil, nil},
		{"empty returns empty", &pb.GitAddArguments{}, nil},
		{
			"common flags + pathspec",
			&pb.GitAddArguments{
				Verbose:     pb.OptBool_OPT_BOOL_TRUE,
				Update:      pb.OptBool_OPT_BOOL_TRUE,
				IntentToAdd: pb.OptBool_OPT_BOOL_TRUE,
				Pathspec:    &pb.Pathspec{Pathspec: []string{"src/"}},
			},
			[]string{"--verbose", "--update", "--intent-to-add", "--", "src/"},
		},
		{
			"all tri-state on",
			&pb.GitAddArguments{All: pb.OptBool_OPT_BOOL_TRUE},
			[]string{"--all"},
		},
		{
			"all tri-state off emits --no-all",
			&pb.GitAddArguments{All: pb.OptBool_OPT_BOOL_FALSE},
			[]string{"--no-all"},
		},
		{
			"chmod +x enum",
			&pb.GitAddArguments{Chmod: pb.ChmodExecutable_CHMOD_EXECUTABLE_ADD},
			[]string{"--chmod=+x"},
		},
		{
			"chmod -x enum",
			&pb.GitAddArguments{Chmod: pb.ChmodExecutable_CHMOD_EXECUTABLE_REMOVE},
			[]string{"--chmod=-x"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := argvAdd(c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestArgvCommit(t *testing.T) {
	in := &pb.GitCommitArguments{
		All:            pb.OptBool_OPT_BOOL_TRUE,
		Identity:       &pb.IdentityOverride{Author: "A <a@a>", Date: "2026-01-01"},
		Message:        &pb.MessageSource{Message: []string{"msg"}},
		GpgSign:        &pb.GpgSign{Sign: pb.OptBool_OPT_BOOL_FALSE},
		UntrackedFiles: pb.UntrackedFilesMode_UNTRACKED_FILES_MODE_ALL,
		StatusFormat:   pb.StatusFormat_STATUS_FORMAT_PORCELAIN_V2,
		Pathspec:       &pb.Pathspec{Pathspec: []string{"f.go"}},
	}
	got := argvCommit(in)
	want := []string{
		"--all",
		"--author=A <a@a>", "--date=2026-01-01",
		"-m", "msg",
		"--no-gpg-sign",
		"--untracked-files=all",
		"--porcelain=v2",
		"--", "f.go",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvDiffWithFormatting(t *testing.T) {
	// Covers DiffFormatting OptInt + ColorWhen + stat + an --output-indicator.
	in := &pb.GitDiffArguments{
		Formatting: &pb.DiffFormatting{
			Patch:               pb.OptBool_OPT_BOOL_TRUE,
			Unified:             &pb.OptInt{Present: true, Value: 5},
			OutputIndicatorNew:  ">",
			Stat:                pb.OptBool_OPT_BOOL_TRUE,
			Color:               pb.ColorWhen_COLOR_WHEN_NEVER,
		},
		Cached:  pb.OptBool_OPT_BOOL_TRUE,
		Commits: []string{"HEAD~1", "HEAD"},
	}
	got := argvDiff(in)
	want := []string{
		"--patch", "--unified=5", "--output-indicator-new=>", "--stat", "--color=never",
		"--cached",
		"HEAD~1", "HEAD",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvLogPatternsAndOptInt(t *testing.T) {
	// branches_pattern preferred over bare --branches; max-count OptInt present
	// with value 0 must still emit --max-count=0.
	in := &pb.GitLogArguments{
		MaxCount:        &pb.OptInt{Present: true, Value: 0},
		BranchesPattern: "release/*",
		Grep:            []string{"fix", "bug"},
		RevisionRange:   []string{"main..HEAD"},
	}
	got := argvLog(in)
	// Builder order: limiting (grep) comes before revision selection (branches).
	want := []string{
		"--max-count=0",
		"--grep=fix", "--grep=bug",
		"--branches=release/*",
		"main..HEAD",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvBranchActionForce(t *testing.T) {
	// Delete + force picks -D, not -d; plain force emits --force earlier.
	got := argvBranch(&pb.GitBranchArguments{
		Action: pb.GitBranchArguments_ACTION_DELETE,
		Force:  pb.OptBool_OPT_BOOL_TRUE,
		Names:  []string{"topic"},
	})
	want := []string{"--force", "-D", "topic"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}

	// Move without force picks -m.
	got = argvBranch(&pb.GitBranchArguments{
		Action: pb.GitBranchArguments_ACTION_MOVE,
		Names:  []string{"old", "new"},
	})
	want = []string{"-m", "old", "new"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestArgvBisectVerbs(t *testing.T) {
	// reset with commit
	got := argvBisect(&pb.GitBisectArguments{
		Verb: &pb.GitBisectArguments_Reset_{Reset_: &pb.BisectReset{Commit: "main"}},
	})
	if !reflect.DeepEqual(got, []string{"reset", "main"}) {
		t.Errorf("reset verb: %v", got)
	}
	// start with term/bad/good
	got = argvBisect(&pb.GitBisectArguments{
		Verb: &pb.GitBisectArguments_Start{Start: &pb.BisectStart{
			TermNew: "broken", Bad: "HEAD", Good: []string{"v1"},
		}},
	})
	want := []string{"start", "--term-new=broken", "HEAD", "v1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("start verb: got %v want %v", got, want)
	}
}

func TestArgvStashVerbs(t *testing.T) {
	// push -m "wip" with pathspec
	got := argvStash(&pb.GitStashArguments{
		Verb: &pb.GitStashArguments_Push{Push: &pb.StashPush{
			Message:  "wip",
			Pathspec: &pb.Pathspec{Pathspec: []string{"a.go"}},
		}},
	})
	want := []string{"push", "-m", "wip", "--", "a.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("push: got %v want %v", got, want)
	}
	// list with a log option passthrough
	got = argvStash(&pb.GitStashArguments{
		Verb: &pb.GitStashArguments_List{List: &pb.StashList{LogOptions: []string{"--oneline"}}},
	})
	if !reflect.DeepEqual(got, []string{"list", "--oneline"}) {
		t.Errorf("list: %v", got)
	}
}

func TestArgvResetMode(t *testing.T) {
	got := argvReset(&pb.GitResetArguments{
		Mode:     pb.GitResetArguments_MODE_HARD,
		Commit:   "HEAD~1",
		Pathspec: &pb.Pathspec{Pathspec: []string{"x"}},
	})
	want := []string{"--hard", "HEAD~1", "--", "x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestArgvInitShared(t *testing.T) {
	// shared with a perms string emits --shared=<perms>.
	got := argvInit(&pb.GitInitArguments{Shared: "0660", Directory: "r"})
	if !reflect.DeepEqual(got, []string{"--shared=0660", "r"}) {
		t.Errorf("perms: %v", got)
	}
	// shared with only the flag emits bare --shared.
	got = argvInit(&pb.GitInitArguments{SharedFlag: pb.OptBool_OPT_BOOL_TRUE})
	if !reflect.DeepEqual(got, []string{"--shared"}) {
		t.Errorf("bare: %v", got)
	}
}

// ---- buildArgv (oneof dispatch) ------------------------------------------

func TestBuildArgvDispatch(t *testing.T) {
	cases := []struct {
		name    string
		in      *pb.Subcommand
		subWant string
		argsWant []string
	}{
		{
			"reset (reserved oneof name)",
			&pb.Subcommand{Args: &pb.Subcommand_Reset_{Reset_: &pb.GitResetArguments{
				Mode: pb.GitResetArguments_MODE_SOFT,
			}}},
			"reset",
			[]string{"--soft"},
		},
		{
			"cherry-pick hyphenates command name",
			&pb.Subcommand{Args: &pb.Subcommand_CherryPick{CherryPick: &pb.GitCherryPickArguments{
				Control: pb.GitCherryPickArguments_CONTROL_ABORT,
			}}},
			"cherry-pick",
			[]string{"--abort"},
		},
		{
			"sparse-checkout hyphenates",
			&pb.Subcommand{Args: &pb.Subcommand_SparseCheckout{SparseCheckout: &pb.GitSparseCheckoutArguments{
				Verb: pb.GitSparseCheckoutArguments_VERB_LIST,
			}}},
			"sparse-checkout",
			[]string{"list"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sub, argv, err := buildArgv(c.in)
			if err != nil {
				t.Fatalf("buildArgv err: %v", err)
			}
			if sub != c.subWant {
				t.Errorf("sub: got %q want %q", sub, c.subWant)
			}
			if !reflect.DeepEqual(argv, c.argsWant) {
				t.Errorf("argv: got %v want %v", argv, c.argsWant)
			}
		})
	}

	if _, _, err := buildArgv(&pb.Subcommand{}); err == nil {
		t.Error("empty oneof should error")
	}
}

// ---- end-to-end Execute ---------------------------------------------------

// TestExecuteHappyPath drives init → add → commit → log through the Execute
// RPC using the typed Subcommand wrapper, confirming the argv builders chain
// correctly into s.run / s.runMkdir.
func TestExecuteHappyPath(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Init with a structured initial-branch flag.
	msg, err := client.Execute(ctx, &pb.Subcommand{
		Repo: r,
		Args: &pb.Subcommand_Init{Init: &pb.GitInitArguments{InitialBranch: "main"}},
	})
	if err != nil {
		t.Fatalf("Execute Init: %v", err)
	}
	if errs := msg.GetErrs(); len(errs) > 0 {
		t.Fatalf("Init errs: %v", errs)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		t.Fatalf("Init didn't create .git: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "hi.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add ".": supplied via Pathspec.
	if _, err := client.Execute(ctx, &pb.Subcommand{
		Repo: r,
		Args: &pb.Subcommand_Add{Add: &pb.GitAddArguments{
			Pathspec: &pb.Pathspec{Pathspec: []string{"."}},
		}},
	}); err != nil {
		t.Fatalf("Execute Add: %v", err)
	}

	// Commit with a structured -m.
	if msg, err := client.Execute(ctx, &pb.Subcommand{
		Repo: r,
		Args: &pb.Subcommand_Commit{Commit: &pb.GitCommitArguments{
			Message: &pb.MessageSource{Message: []string{"structured"}},
		}},
	}); err != nil {
		t.Fatalf("Execute Commit: %v", err)
	} else if errs := msg.GetErrs(); len(errs) > 0 {
		t.Fatalf("Commit errs: %v\nstderr: %v", errs, msg.GetStderr().GetLine())
	}

	// Log --oneline via DiffFormatting-less path.
	logMsg, err := client.Execute(ctx, &pb.Subcommand{
		Repo: r,
		Args: &pb.Subcommand_Log{Log: &pb.GitLogArguments{Oneline: pb.OptBool_OPT_BOOL_TRUE}},
	})
	if err != nil {
		t.Fatalf("Execute Log: %v", err)
	}
	if !linesContain(logMsg.GetStdout().GetLine(), "structured") {
		t.Errorf("log stdout missing 'structured': %v", logMsg.GetStdout().GetLine())
	}
}

// TestExecuteEmptyOneofReportsError verifies that an unset args oneof yields a
// resolve-style error in msg.Errs (no gRPC error).
func TestExecuteEmptyOneofReportsError(t *testing.T) {
	tmp := t.TempDir()
	client, stop := startServer(t, tmp)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg, err := client.Execute(ctx, &pb.Subcommand{Repo: pathRepo(tmp)})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	errs := msg.GetErrs()
	if len(errs) == 0 {
		t.Fatal("expected an error in msg.Errs")
	}
	if !strings.Contains(errs[0], "oneof") {
		t.Errorf("error should mention oneof: %v", errs)
	}
}
