package subcommands

// Argv-builder tests for the 25 subcommands not covered in structured_test.go:
// Archive, Backfill, Bundle, Checkout, CherryPick, Clean, Describe, Gc,
// Maintenance, Merge, Mv, Notes, Push, RangeDiff, Rebase, Restore, Revert,
// Rm, Shortlog, Show, SparseCheckout, Status, Submodule, Switch, Tag,
// Worktree. Each test asserts the exact argv produced by the builder.

import (
	"reflect"
	"testing"

	pb "github.com/accretional/proto-repo/genpb"
)

func TestArgvArchive(t *testing.T) {
	in := &pb.GitArchiveArguments{
		Format:            "tar.gz",
		Output:            "out.tgz",
		Prefix:            "proj/",
		AddFile:           []string{"NOTES", "LICENSE"},
		CompressionLevel:  9,
		TreeIsh:           "HEAD",
		Path:              []string{"src/"},
	}
	got := argvArchive(in)
	want := []string{
		"--format=tar.gz", "--prefix=proj/", "--output=out.tgz",
		"--add-file=NOTES", "--add-file=LICENSE",
		"-9",
		"HEAD",
		"--", "src/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvBackfill(t *testing.T) {
	got := argvBackfill(&pb.GitBackfillArguments{
		BatchSize: 500,
		Sparse:    pb.OptBool_OPT_BOOL_FALSE,
	})
	want := []string{"--batch-size=500", "--no-sparse"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestArgvBundleVerbs(t *testing.T) {
	// create
	got := argvBundle(&pb.GitBundleArguments{
		Verb: &pb.GitBundleArguments_Create{Create: &pb.BundleCreate{
			Quiet:       pb.OptBool_OPT_BOOL_TRUE,
			Version:     3,
			File:        "out.bundle",
			RevListArgs: []string{"--all"},
		}},
	})
	want := []string{"create", "--quiet", "--version=3", "out.bundle", "--all"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("create: got %v want %v", got, want)
	}

	// verify
	got = argvBundle(&pb.GitBundleArguments{
		Verb: &pb.GitBundleArguments_Verify{Verify: &pb.BundleVerify{File: "x.bundle"}},
	})
	if !reflect.DeepEqual(got, []string{"verify", "x.bundle"}) {
		t.Errorf("verify: %v", got)
	}

	// list-heads
	got = argvBundle(&pb.GitBundleArguments{
		Verb: &pb.GitBundleArguments_ListHeads{ListHeads: &pb.BundleListHeads{
			File: "x.bundle", Refname: []string{"refs/heads/main"},
		}},
	})
	if !reflect.DeepEqual(got, []string{"list-heads", "x.bundle", "refs/heads/main"}) {
		t.Errorf("list-heads: %v", got)
	}
}

func TestArgvCheckout(t *testing.T) {
	got := argvCheckout(&pb.GitCheckoutArguments{
		Quiet:     pb.OptBool_OPT_BOOL_TRUE,
		NewBranch: "topic",
		Track:     pb.BranchTrackMode_BRANCH_TRACK_MODE_DIRECT,
		Pathspec:  &pb.Pathspec{Pathspec: []string{"a.go"}},
	})
	want := []string{"--quiet", "-b", "topic", "--track=direct", "--", "a.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvCherryPickControlAndCommits(t *testing.T) {
	// control short-circuits everything else
	if got := argvCherryPick(&pb.GitCherryPickArguments{
		Control: pb.GitCherryPickArguments_CONTROL_CONTINUE,
		Edit:    pb.OptBool_OPT_BOOL_TRUE,
	}); !reflect.DeepEqual(got, []string{"--continue"}) {
		t.Errorf("control: %v", got)
	}
	// full picker
	got := argvCherryPick(&pb.GitCherryPickArguments{
		Edit:     pb.OptBool_OPT_BOOL_TRUE,
		Mainline: 2,
		Signoff:  pb.OptBool_OPT_BOOL_TRUE,
		GpgSign:  &pb.GpgSign{Sign: pb.OptBool_OPT_BOOL_TRUE},
		X:        pb.OptBool_OPT_BOOL_TRUE,
		Commits:  []string{"abc", "def"},
	})
	want := []string{"--edit", "-m", "2", "--signoff", "--gpg-sign", "-x", "abc", "def"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvClean(t *testing.T) {
	// Caller expressed destructive intent (--force) so no implicit dry-run.
	got := argvClean(&pb.GitCleanArguments{
		Force:          pb.OptBool_OPT_BOOL_TRUE,
		Directories:    pb.OptBool_OPT_BOOL_TRUE,
		Exclude:        []string{"*.log", "tmp"},
		IncludeIgnored: pb.OptBool_OPT_BOOL_TRUE,
		Pathspec:       &pb.Pathspec{Pathspec: []string{"build/"}},
	})
	want := []string{"--force", "-d", "-e", "*.log", "-e", "tmp", "-x", "--", "build/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

// TestArgvCleanSafeDefaults guards the safety default that mirrors clean.go:
// any request without --force/--interactive/--dry-run becomes a dry-run.
func TestArgvCleanSafeDefaults(t *testing.T) {
	cases := []struct {
		name string
		in   *pb.GitCleanArguments
		want []string
	}{
		{"nil request", nil, []string{"--dry-run"}},
		{"empty request", &pb.GitCleanArguments{}, []string{"--dry-run"}},
		{
			"directories without force still dry-runs",
			&pb.GitCleanArguments{Directories: pb.OptBool_OPT_BOOL_TRUE},
			[]string{"--dry-run", "-d"},
		},
		{
			"explicit dry-run is preserved (not duplicated)",
			&pb.GitCleanArguments{DryRun: pb.OptBool_OPT_BOOL_TRUE},
			[]string{"--dry-run"},
		},
		{
			"interactive bypasses dry-run injection",
			&pb.GitCleanArguments{Interactive: pb.OptBool_OPT_BOOL_TRUE},
			[]string{"--interactive"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := argvClean(c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestArgvDescribeAbbrevAndDirty(t *testing.T) {
	// abbrev_is_set=true with value=0 must emit --abbrev=0 (disables abbrev).
	got := argvDescribe(&pb.GitDescribeArguments{
		Tags:         pb.OptBool_OPT_BOOL_TRUE,
		Abbrev:       0,
		AbbrevIsSet:  true,
		DirtyMark:    "-dirty",
		Matches:      []string{"v*", "rel-*"},
		CommitIsh:    []string{"HEAD"},
	})
	want := []string{
		"--tags", "--abbrev=0", "--dirty=-dirty",
		"--match=v*", "--match=rel-*",
		"HEAD",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	// bare --dirty (no mark).
	got = argvDescribe(&pb.GitDescribeArguments{Dirty: pb.OptBool_OPT_BOOL_TRUE})
	if !reflect.DeepEqual(got, []string{"--dirty"}) {
		t.Errorf("bare dirty: %v", got)
	}
}

func TestArgvGc(t *testing.T) {
	got := argvGc(&pb.GitGcArguments{
		Auto:            pb.OptBool_OPT_BOOL_TRUE,
		Prune:           "now",
		Cruft:           pb.OptBool_OPT_BOOL_FALSE,
		CruftExpiration: "2.weeks.ago",
	})
	want := []string{"--auto", "--prune=now", "--no-cruft", "--cruft-expiration=2.weeks.ago"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvMaintenance(t *testing.T) {
	got := argvMaintenance(&pb.GitMaintenanceArguments{
		Verb:     pb.GitMaintenanceArguments_VERB_RUN,
		Schedule: "hourly",
		Task:     []string{"gc", "commit-graph"},
		Quiet:    pb.OptBool_OPT_BOOL_TRUE,
	})
	want := []string{"run", "--schedule=hourly", "--task=gc", "--task=commit-graph", "--quiet"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvMergeControlAndFull(t *testing.T) {
	if got := argvMerge(&pb.GitMergeArguments{
		Control: pb.GitMergeArguments_CONTROL_ABORT,
	}); !reflect.DeepEqual(got, []string{"--abort"}) {
		t.Errorf("control: %v", got)
	}
	got := argvMerge(&pb.GitMergeArguments{
		FastForward: pb.FastForward_FAST_FORWARD_NEVER,
		Strategy:    []string{"ours"},
		Message:     &pb.MessageSource{Message: []string{"merge msg"}},
		Commits:     []string{"topic"},
	})
	want := []string{"--no-ff", "-s", "ours", "-m", "merge msg", "topic"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("full: got %v\nwant %v", got, want)
	}
}

func TestArgvMv(t *testing.T) {
	got := argvMv(&pb.GitMvArguments{
		Force:       pb.OptBool_OPT_BOOL_TRUE,
		Sources:     []string{"a", "b"},
		Destination: "dst/",
	})
	want := []string{"--force", "a", "b", "dst/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestArgvNotes(t *testing.T) {
	got := argvNotes(&pb.GitNotesArguments{
		Ref:     "refs/notes/review",
		Verb:    pb.GitNotesArguments_VERB_ADD,
		Force:   pb.OptBool_OPT_BOOL_TRUE,
		Message: &pb.MessageSource{Message: []string{"looks good"}},
		Objects: []string{"HEAD"},
	})
	want := []string{"--ref=refs/notes/review", "add", "--force", "-m", "looks good", "HEAD"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvPushForceWithLeaseVariants(t *testing.T) {
	// value variant
	got := argvPush(&pb.GitPushArguments{
		ForceWithLease: "main:abc123",
		Repository:     "origin",
		Refspec:        []string{"main"},
	})
	want := []string{"--force-with-lease=main:abc123", "origin", "main"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("value: got %v\nwant %v", got, want)
	}
	// bare flag variant
	got = argvPush(&pb.GitPushArguments{
		ForceWithLeaseFlag: pb.OptBool_OPT_BOOL_TRUE,
		Repository:         "origin",
	})
	if !reflect.DeepEqual(got, []string{"--force-with-lease", "origin"}) {
		t.Errorf("bare: %v", got)
	}
}

func TestArgvRangeDiff(t *testing.T) {
	got := argvRangeDiff(&pb.GitRangeDiffArguments{
		CreationFactor:      pb.OptBool_OPT_BOOL_TRUE,
		CreationFactorValue: 75,
		NotesFlag:           pb.OptBool_OPT_BOOL_TRUE,
		Ranges:              []string{"A..B", "C..D"},
	})
	want := []string{"--creation-factor=75", "--notes", "A..B", "C..D"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvRebaseControlAndFull(t *testing.T) {
	if got := argvRebase(&pb.GitRebaseArguments{
		Control: pb.GitRebaseArguments_CONTROL_SKIP,
	}); !reflect.DeepEqual(got, []string{"--skip"}) {
		t.Errorf("control: %v", got)
	}
	got := argvRebase(&pb.GitRebaseArguments{
		Interactive:       pb.OptBool_OPT_BOOL_TRUE,
		Onto:              "main",
		RebaseMergesMode:  "rebase-cousins",
		Autosquash:        pb.OptBool_OPT_BOOL_TRUE,
		Execs:             []string{"make test"},
		Upstream:          "origin/main",
	})
	want := []string{
		"--interactive", "--onto=main",
		"--rebase-merges=rebase-cousins",
		"--autosquash",
		"-x", "make test",
		"origin/main",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("full: got %v\nwant %v", got, want)
	}
}

func TestArgvRestore(t *testing.T) {
	got := argvRestore(&pb.GitRestoreArguments{
		Source:   "HEAD~1",
		Staged:   pb.OptBool_OPT_BOOL_TRUE,
		Pathspec: &pb.Pathspec{Pathspec: []string{"f.go"}},
	})
	want := []string{"--source=HEAD~1", "--staged", "--", "f.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestArgvRevert(t *testing.T) {
	if got := argvRevert(&pb.GitRevertArguments{
		Control: pb.GitRevertArguments_CONTROL_QUIT,
	}); !reflect.DeepEqual(got, []string{"--quit"}) {
		t.Errorf("control: %v", got)
	}
	got := argvRevert(&pb.GitRevertArguments{
		Mainline: 1,
		NoCommit: pb.OptBool_OPT_BOOL_TRUE,
		Commits:  []string{"abc"},
	})
	want := []string{"-m", "1", "--no-commit", "abc"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestArgvRm(t *testing.T) {
	got := argvRm(&pb.GitRmArguments{
		Recursive: pb.OptBool_OPT_BOOL_TRUE,
		Cached:    pb.OptBool_OPT_BOOL_TRUE,
		Pathspec:  &pb.Pathspec{Pathspec: []string{"tmp/"}},
	})
	want := []string{"-r", "--cached", "--", "tmp/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestArgvShortlog(t *testing.T) {
	got := argvShortlog(&pb.GitShortlogArguments{
		Numbered:      pb.OptBool_OPT_BOOL_TRUE,
		Summary:       pb.OptBool_OPT_BOOL_TRUE,
		Email:         pb.OptBool_OPT_BOOL_TRUE,
		Abbrev:        7,
		Range:         "HEAD~5..HEAD",
		RevisionRange: []string{"main"},
	})
	want := []string{"--numbered", "--summary", "--email", "-c7", "HEAD~5..HEAD", "main"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvShow(t *testing.T) {
	got := argvShow(&pb.GitShowArguments{
		Formatting:    &pb.DiffFormatting{Stat: pb.OptBool_OPT_BOOL_TRUE},
		Pretty:        "fuller",
		AbbrevCommit:  pb.OptBool_OPT_BOOL_TRUE,
		NotesRef:      "refs/notes/review",
		ShowSignature: pb.OptBool_OPT_BOOL_TRUE,
		Objects:       []string{"HEAD", "HEAD~1"},
	})
	want := []string{
		"--stat",
		"--pretty=fuller", "--abbrev-commit",
		"--notes=refs/notes/review",
		"--show-signature",
		"HEAD", "HEAD~1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvSparseCheckout(t *testing.T) {
	got := argvSparseCheckout(&pb.GitSparseCheckoutArguments{
		Verb:     pb.GitSparseCheckoutArguments_VERB_SET,
		Cone:     pb.OptBool_OPT_BOOL_TRUE,
		Patterns: []string{"/src", "/docs"},
	})
	want := []string{"set", "--cone", "/src", "/docs"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvStatus(t *testing.T) {
	got := argvStatus(&pb.GitStatusArguments{
		Format:         pb.StatusFormat_STATUS_FORMAT_PORCELAIN_V1,
		Branch:         pb.OptBool_OPT_BOOL_TRUE,
		UntrackedFiles: pb.UntrackedFilesMode_UNTRACKED_FILES_MODE_ALL,
		Ignored:        pb.IgnoredMode_IGNORED_MODE_TRADITIONAL,
		Pathspec:       &pb.Pathspec{Pathspec: []string{"."}},
	})
	want := []string{
		"--porcelain", "--branch", "--untracked-files=all",
		"--ignored=traditional",
		"--", ".",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvSubmodule(t *testing.T) {
	got := argvSubmodule(&pb.GitSubmoduleArguments{
		Verb:      pb.GitSubmoduleArguments_VERB_UPDATE,
		Init:      pb.OptBool_OPT_BOOL_TRUE,
		Remote:    pb.OptBool_OPT_BOOL_TRUE,
		Recursive: pb.OptBool_OPT_BOOL_TRUE,
		Depth:     "1",
		Paths:     []string{"vendor/a"},
	})
	want := []string{"update", "--recursive", "--init", "--remote", "--depth=1", "vendor/a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvSwitch(t *testing.T) {
	// -c <new-branch> with tracked start-point
	got := argvSwitch(&pb.GitSwitchArguments{
		Create:     "topic",
		Track:      pb.BranchTrackMode_BRANCH_TRACK_MODE_DIRECT,
		StartPoint: "origin/main",
	})
	want := []string{"-c", "topic", "--track=direct", "origin/main"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}

	// --detach <branch>
	got = argvSwitch(&pb.GitSwitchArguments{Detach: pb.OptBool_OPT_BOOL_TRUE, Branch: "main"})
	if !reflect.DeepEqual(got, []string{"--detach", "main"}) {
		t.Errorf("detach: %v", got)
	}
}

func TestArgvTagCreateAndList(t *testing.T) {
	// create annotated
	got := argvTag(&pb.GitTagArguments{
		Annotate: pb.OptBool_OPT_BOOL_TRUE,
		Message:  &pb.MessageSource{Message: []string{"v1"}},
		TagName:  "v1",
		Object:   "HEAD",
	})
	want := []string{"-a", "-m", "v1", "v1", "HEAD"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("create: got %v\nwant %v", got, want)
	}

	// list with pattern
	got = argvTag(&pb.GitTagArguments{
		Action:   pb.GitTagArguments_ACTION_LIST,
		Patterns: []string{"v*"},
	})
	if !reflect.DeepEqual(got, []string{"--list", "v*"}) {
		t.Errorf("list: %v", got)
	}
}

func TestArgvWorktree(t *testing.T) {
	// nil → default to "list"
	if got := argvWorktree(nil); !reflect.DeepEqual(got, []string{"list"}) {
		t.Errorf("nil: %v", got)
	}

	// add with branch + path + commit-ish
	got := argvWorktree(&pb.GitWorktreeArguments{
		Verb:      pb.GitWorktreeArguments_VERB_ADD,
		Branch:    "topic",
		Path:      "../wt-topic",
		CommitIsh: "HEAD",
	})
	want := []string{"add", "-b", "topic", "../wt-topic", "HEAD"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("add: got %v\nwant %v", got, want)
	}
}

func TestArgvClone(t *testing.T) {
	// argvClone emits ONLY the flags — runClone supplies the URL and dest.
	got := argvClone(&pb.GitCloneArguments{
		Depth:            1,
		Branch:           "main",
		SingleBranch:     pb.OptBool_OPT_BOOL_TRUE,
		NoTags:           pb.OptBool_OPT_BOOL_TRUE,
		Filter:           "blob:none",
		Quiet:            pb.OptBool_OPT_BOOL_TRUE,
		RecurseSubmodules: pb.RecurseSubmodules_RECURSE_SUBMODULES_ON_DEMAND,
	})
	want := []string{
		"--branch=main",
		"--depth=1",
		"--single-branch",
		"--no-tags",
		"--recurse-submodules=on-demand",
		"--filter=blob:none",
		"--quiet",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvCloneNil(t *testing.T) {
	if got := argvClone(nil); len(got) != 0 {
		t.Errorf("nil input should return empty args, got %v", got)
	}
}

func TestArgvFetch(t *testing.T) {
	got := argvFetch(&pb.GitFetchArguments{
		All:        pb.OptBool_OPT_BOOL_TRUE,
		Prune:      pb.OptBool_OPT_BOOL_TRUE,
		Tags:       pb.OptBool_OPT_BOOL_TRUE,
		Depth:      10,
		Jobs:       4,
		Force:      pb.OptBool_OPT_BOOL_TRUE,
		Repository: "origin",
		Refspecs:   []string{"main", "dev"},
	})
	want := []string{
		"--all", "--depth=10", "--prune", "--tags", "--force",
		"-j", "4",
		"origin", "main", "dev",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvFetchTagsFalseDoesNotDoubleEmit(t *testing.T) {
	// Setting Tags=FALSE should emit --no-tags once; NoTags must be ignored
	// when Tags is already expressed.
	got := argvFetch(&pb.GitFetchArguments{
		Tags:   pb.OptBool_OPT_BOOL_FALSE,
		NoTags: pb.OptBool_OPT_BOOL_TRUE,
	})
	want := []string{"--no-tags"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvPull(t *testing.T) {
	got := argvPull(&pb.GitPullArguments{
		FastForward: pb.FastForward_FAST_FORWARD_ONLY,
		Autostash:   pb.OptBool_OPT_BOOL_TRUE,
		Quiet:       pb.OptBool_OPT_BOOL_TRUE,
		Repository:  "origin",
		Refspecs:    []string{"main"},
	})
	want := []string{"--ff-only", "--autostash", "--quiet", "origin", "main"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvPullRebaseMode(t *testing.T) {
	// rebase_mode takes precedence over no_rebase; setting both should emit
	// only --rebase=<mode>.
	got := argvPull(&pb.GitPullArguments{
		RebaseMode: "interactive",
		NoRebase:   pb.OptBool_OPT_BOOL_TRUE,
	})
	want := []string{"--rebase=interactive"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestArgvPullNoRebaseWhenRebaseModeUnset(t *testing.T) {
	got := argvPull(&pb.GitPullArguments{NoRebase: pb.OptBool_OPT_BOOL_TRUE})
	want := []string{"--no-rebase"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}
