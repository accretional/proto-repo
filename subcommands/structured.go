package subcommands

import (
	"context"
	"fmt"
	"strconv"

	pb "github.com/accretional/proto-repo/genpb"
	"github.com/accretional/proto-repo/internal/gitexec"
)

// Execute dispatches a structured Subcommand: inspects the args oneof,
// converts the typed fields back into a git argv, then runs it inside the
// request's resolved repo path. Equivalent to the per-subcommand RPCs but
// callers don't have to do shell-style arg formatting.
func (s *Server) Execute(ctx context.Context, req *pb.Subcommand) (*pb.RepoMsg, error) {
	sub, argv, err := buildArgv(req)
	if err != nil {
		msg := gitexec.NewMsg(req.GetRepo())
		msg.Errs = append(msg.Errs, err.Error())
		return msg, nil
	}
	if sub == "init" {
		return s.runMkdir(ctx, req.GetRepo(), sub, argv...), nil
	}
	return s.run(ctx, req.GetRepo(), sub, argv...), nil
}

// buildArgv returns (subcommand, args, error) from the structured request.
func buildArgv(req *pb.Subcommand) (string, []string, error) {
	switch a := req.GetArgs().(type) {
	case *pb.Subcommand_Add:
		return "add", argvAdd(a.Add), nil
	case *pb.Subcommand_Archive:
		return "archive", argvArchive(a.Archive), nil
	case *pb.Subcommand_Backfill:
		return "backfill", argvBackfill(a.Backfill), nil
	case *pb.Subcommand_Bisect:
		return "bisect", argvBisect(a.Bisect), nil
	case *pb.Subcommand_Branch:
		return "branch", argvBranch(a.Branch), nil
	case *pb.Subcommand_Bundle:
		return "bundle", argvBundle(a.Bundle), nil
	case *pb.Subcommand_Checkout:
		return "checkout", argvCheckout(a.Checkout), nil
	case *pb.Subcommand_CherryPick:
		return "cherry-pick", argvCherryPick(a.CherryPick), nil
	case *pb.Subcommand_Clean:
		return "clean", argvClean(a.Clean), nil
	case *pb.Subcommand_Commit:
		return "commit", argvCommit(a.Commit), nil
	case *pb.Subcommand_Describe:
		return "describe", argvDescribe(a.Describe), nil
	case *pb.Subcommand_Diff:
		return "diff", argvDiff(a.Diff), nil
	case *pb.Subcommand_Gc:
		return "gc", argvGc(a.Gc), nil
	case *pb.Subcommand_Init:
		return "init", argvInit(a.Init), nil
	case *pb.Subcommand_Log:
		return "log", argvLog(a.Log), nil
	case *pb.Subcommand_Maintenance:
		return "maintenance", argvMaintenance(a.Maintenance), nil
	case *pb.Subcommand_Merge:
		return "merge", argvMerge(a.Merge), nil
	case *pb.Subcommand_Mv:
		return "mv", argvMv(a.Mv), nil
	case *pb.Subcommand_Notes:
		return "notes", argvNotes(a.Notes), nil
	case *pb.Subcommand_Push:
		return "push", argvPush(a.Push), nil
	case *pb.Subcommand_RangeDiff:
		return "range-diff", argvRangeDiff(a.RangeDiff), nil
	case *pb.Subcommand_Rebase:
		return "rebase", argvRebase(a.Rebase), nil
	case *pb.Subcommand_Reset_:
		return "reset", argvReset(a.Reset_), nil
	case *pb.Subcommand_Restore:
		return "restore", argvRestore(a.Restore), nil
	case *pb.Subcommand_Revert:
		return "revert", argvRevert(a.Revert), nil
	case *pb.Subcommand_Rm:
		return "rm", argvRm(a.Rm), nil
	case *pb.Subcommand_Shortlog:
		return "shortlog", argvShortlog(a.Shortlog), nil
	case *pb.Subcommand_Show:
		return "show", argvShow(a.Show), nil
	case *pb.Subcommand_SparseCheckout:
		return "sparse-checkout", argvSparseCheckout(a.SparseCheckout), nil
	case *pb.Subcommand_Stash:
		return "stash", argvStash(a.Stash), nil
	case *pb.Subcommand_Status:
		return "status", argvStatus(a.Status), nil
	case *pb.Subcommand_Submodule:
		return "submodule", argvSubmodule(a.Submodule), nil
	case *pb.Subcommand_Switch:
		return "switch", argvSwitch(a.Switch), nil
	case *pb.Subcommand_Tag:
		return "tag", argvTag(a.Tag), nil
	case *pb.Subcommand_Worktree:
		return "worktree", argvWorktree(a.Worktree), nil
	case nil:
		return "", nil, fmt.Errorf("Subcommand.args oneof is empty")
	default:
		return "", nil, fmt.Errorf("unsupported Subcommand.args type %T", a)
	}
}

// ---- primitive helpers ----------------------------------------------------

// addTriBool emits `on` when v is TRUE, `off` when v is FALSE, nothing when
// UNSPECIFIED. Pass "" for off if the flag has no negation form.
func addTriBool(args []string, on, off string, v pb.OptBool) []string {
	switch v {
	case pb.OptBool_OPT_BOOL_TRUE:
		return append(args, on)
	case pb.OptBool_OPT_BOOL_FALSE:
		if off != "" {
			return append(args, off)
		}
	}
	return args
}

// addFlagIfTrue is a shorthand for addTriBool with no off-form.
func addFlagIfTrue(args []string, flag string, v pb.OptBool) []string {
	if v == pb.OptBool_OPT_BOOL_TRUE {
		return append(args, flag)
	}
	return args
}

// addFlagIfFalse emits `flag` only when v == FALSE (for explicit --no-* flags
// modelled as standalone OptBool fields).
func addFlagIfFalse(args []string, flag string, v pb.OptBool) []string {
	if v == pb.OptBool_OPT_BOOL_FALSE {
		return append(args, flag)
	}
	return args
}

// addEqVal emits `flag=value` when value != "".
func addEqVal(args []string, flag, value string) []string {
	if value != "" {
		return append(args, flag+"="+value)
	}
	return args
}

// addSeparateArg emits `flag value` when value != "".
func addSeparateArg(args []string, flag, value string) []string {
	if value != "" {
		return append(args, flag, value)
	}
	return args
}

// addEqInt emits `flag=<value>` when the OptInt is present.
func addEqInt(args []string, flag string, v *pb.OptInt) []string {
	if v != nil && v.GetPresent() {
		return append(args, flag+"="+strconv.FormatInt(int64(v.GetValue()), 10))
	}
	return args
}

// addAttachedInt emits `flagN` (no separator) when present, e.g. -U3.
func addAttachedInt(args []string, flag string, v *pb.OptInt) []string {
	if v != nil && v.GetPresent() {
		return append(args, flag+strconv.FormatInt(int64(v.GetValue()), 10))
	}
	return args
}

// addAttachedString emits `flagvalue` (no separator/equals) when value != "".
func addAttachedString(args []string, flag, value string) []string {
	if value != "" {
		return append(args, flag+value)
	}
	return args
}

// addRepeatedEq emits one `flag=<v>` per element.
func addRepeatedEq(args []string, flag string, values []string) []string {
	for _, v := range values {
		args = append(args, flag+"="+v)
	}
	return args
}

func addPathspec(args []string, p *pb.Pathspec) []string {
	if p == nil {
		return args
	}
	args = addEqVal(args, "--pathspec-from-file", p.GetPathspecFromFile())
	args = addFlagIfTrue(args, "--pathspec-file-nul", p.GetPathspecFileNul())
	if len(p.GetPathspec()) > 0 {
		args = append(args, "--")
		args = append(args, p.GetPathspec()...)
	}
	return args
}

func addMessageSource(args []string, m *pb.MessageSource) []string {
	if m == nil {
		return args
	}
	for _, msg := range m.GetMessage() {
		args = append(args, "-m", msg)
	}
	args = addSeparateArg(args, "-F", m.GetFile())
	args = addSeparateArg(args, "-C", m.GetReuseMessage())
	args = addSeparateArg(args, "-c", m.GetReeditMessage())
	args = addEqVal(args, "--squash", m.GetSquash())
	args = addEqVal(args, "--fixup", m.GetFixup())
	args = addSeparateArg(args, "--template", m.GetTemplate())
	return args
}

func addGpgSign(args []string, g *pb.GpgSign) []string {
	if g == nil {
		return args
	}
	switch g.GetSign() {
	case pb.OptBool_OPT_BOOL_TRUE:
		if k := g.GetKeyId(); k != "" {
			args = append(args, "--gpg-sign="+k)
		} else {
			args = append(args, "--gpg-sign")
		}
	case pb.OptBool_OPT_BOOL_FALSE:
		args = append(args, "--no-gpg-sign")
	}
	return args
}

func addIdentity(args []string, id *pb.IdentityOverride) []string {
	if id == nil {
		return args
	}
	args = addEqVal(args, "--author", id.GetAuthor())
	args = addEqVal(args, "--date", id.GetDate())
	return args
}

func colorWhenFlag(c pb.ColorWhen) (string, bool) {
	switch c {
	case pb.ColorWhen_COLOR_WHEN_ALWAYS:
		return "--color=always", true
	case pb.ColorWhen_COLOR_WHEN_NEVER:
		return "--color=never", true
	case pb.ColorWhen_COLOR_WHEN_AUTO:
		return "--color=auto", true
	}
	return "", false
}

func decorateFlag(d pb.Decorate) (string, bool) {
	switch d {
	case pb.Decorate_DECORATE_SHORT:
		return "--decorate=short", true
	case pb.Decorate_DECORATE_FULL:
		return "--decorate=full", true
	case pb.Decorate_DECORATE_AUTO:
		return "--decorate=auto", true
	case pb.Decorate_DECORATE_NO:
		return "--decorate=no", true
	}
	return "", false
}

func trackFlag(t pb.BranchTrackMode) (string, bool) {
	switch t {
	case pb.BranchTrackMode_BRANCH_TRACK_MODE_DIRECT:
		return "--track=direct", true
	case pb.BranchTrackMode_BRANCH_TRACK_MODE_INHERIT:
		return "--track=inherit", true
	case pb.BranchTrackMode_BRANCH_TRACK_MODE_NO:
		return "--no-track", true
	}
	return "", false
}

func recurseSubFlag(r pb.RecurseSubmodules) (string, bool) {
	switch r {
	case pb.RecurseSubmodules_RECURSE_SUBMODULES_NO:
		return "--recurse-submodules=no", true
	case pb.RecurseSubmodules_RECURSE_SUBMODULES_YES:
		return "--recurse-submodules=yes", true
	case pb.RecurseSubmodules_RECURSE_SUBMODULES_ON_DEMAND:
		return "--recurse-submodules=on-demand", true
	case pb.RecurseSubmodules_RECURSE_SUBMODULES_CHECK:
		return "--recurse-submodules=check", true
	case pb.RecurseSubmodules_RECURSE_SUBMODULES_ONLY:
		return "--recurse-submodules=only", true
	}
	return "", false
}

func untrackedFilesFlag(u pb.UntrackedFilesMode) (string, bool) {
	switch u {
	case pb.UntrackedFilesMode_UNTRACKED_FILES_MODE_NO:
		return "--untracked-files=no", true
	case pb.UntrackedFilesMode_UNTRACKED_FILES_MODE_NORMAL:
		return "--untracked-files=normal", true
	case pb.UntrackedFilesMode_UNTRACKED_FILES_MODE_ALL:
		return "--untracked-files=all", true
	}
	return "", false
}

func fastForwardFlag(f pb.FastForward) (string, bool) {
	switch f {
	case pb.FastForward_FAST_FORWARD_ALLOW:
		return "--ff", true
	case pb.FastForward_FAST_FORWARD_NEVER:
		return "--no-ff", true
	case pb.FastForward_FAST_FORWARD_ONLY:
		return "--ff-only", true
	}
	return "", false
}

// ---- DiffFormatting -------------------------------------------------------
// Shared by diff, log, show. ~97 flags, all translated verbatim.
func addDiffFormatting(args []string, d *pb.DiffFormatting) []string {
	if d == nil {
		return args
	}
	// Output shape
	args = addTriBool(args, "--patch", "--no-patch", d.GetPatch())
	args = addFlagIfTrue(args, "--no-patch", d.GetNoPatch())
	args = addEqInt(args, "--unified", d.GetUnified())
	args = addEqVal(args, "--output", d.GetOutput())
	args = addEqVal(args, "--output-indicator-new", d.GetOutputIndicatorNew())
	args = addEqVal(args, "--output-indicator-old", d.GetOutputIndicatorOld())
	args = addEqVal(args, "--output-indicator-context", d.GetOutputIndicatorContext())
	args = addFlagIfTrue(args, "--raw", d.GetRaw())
	args = addFlagIfTrue(args, "--patch-with-raw", d.GetPatchWithRaw())

	// Algorithm
	args = addTriBool(args, "--indent-heuristic", "--no-indent-heuristic", d.GetIndentHeuristic())
	args = addFlagIfTrue(args, "--minimal", d.GetMinimal())
	args = addFlagIfTrue(args, "--patience", d.GetPatience())
	args = addFlagIfTrue(args, "--histogram", d.GetHistogram())
	args = addRepeatedEq(args, "--anchored", d.GetAnchored())
	args = addEqVal(args, "--diff-algorithm", d.GetDiffAlgorithm())

	// Stat / summary
	if spec := d.GetStatSpec(); spec != "" {
		args = append(args, "--stat="+spec)
	} else {
		args = addFlagIfTrue(args, "--stat", d.GetStat())
	}
	args = addEqInt(args, "--stat-width", d.GetStatWidth())
	args = addEqInt(args, "--stat-name-width", d.GetStatNameWidth())
	args = addEqInt(args, "--stat-count", d.GetStatCount())
	args = addFlagIfTrue(args, "--compact-summary", d.GetCompactSummary())
	args = addFlagIfTrue(args, "--numstat", d.GetNumstat())
	args = addFlagIfTrue(args, "--shortstat", d.GetShortstat())
	if params := d.GetDirstatParams(); params != "" {
		args = append(args, "--dirstat="+params)
	} else {
		args = addFlagIfTrue(args, "--dirstat", d.GetDirstat())
	}
	args = addFlagIfTrue(args, "--cumulative", d.GetCumulative())
	if params := d.GetDirstatByFileParams(); params != "" {
		args = append(args, "--dirstat-by-file="+params)
	} else {
		args = addFlagIfTrue(args, "--dirstat-by-file", d.GetDirstatByFile())
	}
	args = addFlagIfTrue(args, "--summary", d.GetSummary())
	args = addFlagIfTrue(args, "--patch-with-stat", d.GetPatchWithStat())
	args = addFlagIfTrue(args, "-z", d.GetZ())
	args = addFlagIfTrue(args, "--name-only", d.GetNameOnly())
	args = addFlagIfTrue(args, "--name-status", d.GetNameStatus())
	if fmt := d.GetSubmoduleFormat(); fmt != "" {
		args = append(args, "--submodule="+fmt)
	} else {
		args = addFlagIfTrue(args, "--submodule", d.GetSubmodule())
	}

	// Color / word-diff
	if f, ok := colorWhenFlag(d.GetColor()); ok {
		args = append(args, f)
	} else {
		args = addFlagIfTrue(args, "--color", d.GetColorFlag())
	}
	args = addFlagIfTrue(args, "--no-color", d.GetNoColor())
	if mode := d.GetColorMovedMode(); mode != "" {
		args = append(args, "--color-moved="+mode)
	} else {
		args = addFlagIfTrue(args, "--color-moved", d.GetColorMoved())
	}
	args = addFlagIfTrue(args, "--no-color-moved", d.GetNoColorMoved())
	args = addEqVal(args, "--color-moved-ws", d.GetColorMovedWs())
	args = addFlagIfTrue(args, "--no-color-moved-ws", d.GetNoColorMovedWs())
	if mode := d.GetWordDiffMode(); mode != "" {
		args = append(args, "--word-diff="+mode)
	} else {
		args = addFlagIfTrue(args, "--word-diff", d.GetWordDiff())
	}
	args = addEqVal(args, "--word-diff-regex", d.GetWordDiffRegex())
	if re := d.GetColorWordsRegex(); re != "" {
		args = append(args, "--color-words="+re)
	} else {
		args = addFlagIfTrue(args, "--color-words", d.GetColorWords())
	}

	// Rename / copy / rewrite / similarity
	args = addFlagIfTrue(args, "--no-renames", d.GetNoRenames())
	args = addTriBool(args, "--rename-empty", "--no-rename-empty", d.GetRenameEmpty())
	args = addFlagIfTrue(args, "--check", d.GetCheck())
	args = addEqVal(args, "--ws-error-highlight", d.GetWsErrorHighlight())
	args = addFlagIfTrue(args, "--full-index", d.GetFullIndex())
	args = addFlagIfTrue(args, "--binary", d.GetBinary())
	args = addEqInt(args, "--abbrev", d.GetAbbrev())
	args = addFlagIfTrue(args, "--no-abbrev", d.GetNoAbbrev())
	args = addEqVal(args, "--break-rewrites", d.GetBreakRewrites())
	args = addEqVal(args, "--find-renames", d.GetFindRenames())
	args = addEqVal(args, "--find-copies", d.GetFindCopies())
	args = addFlagIfTrue(args, "--find-copies-harder", d.GetFindCopiesHarder())
	args = addFlagIfTrue(args, "--irreversible-delete", d.GetIrreversibleDelete())
	args = addAttachedInt(args, "-l", d.GetRenameLimit())
	args = addEqVal(args, "--diff-filter", d.GetDiffFilter())
	args = addAttachedString(args, "-S", d.GetPickaxeS())
	args = addAttachedString(args, "-G", d.GetPickaxeG())
	args = addEqVal(args, "--find-object", d.GetFindObject())
	args = addFlagIfTrue(args, "--pickaxe-all", d.GetPickaxeAll())
	args = addFlagIfTrue(args, "--pickaxe-regex", d.GetPickaxeRegex())
	args = addAttachedString(args, "-O", d.GetOrderfile())
	args = addEqVal(args, "--skip-to", d.GetSkipTo())
	args = addEqVal(args, "--rotate-to", d.GetRotateTo())
	args = addFlagIfTrue(args, "-R", d.GetReverse())

	// Relative
	if p := d.GetRelativePath(); p != "" {
		args = append(args, "--relative="+p)
	} else {
		args = addFlagIfTrue(args, "--relative", d.GetRelative())
	}
	args = addFlagIfTrue(args, "--no-relative", d.GetNoRelative())

	// Whitespace
	args = addFlagIfTrue(args, "--text", d.GetText())
	args = addFlagIfTrue(args, "--ignore-cr-at-eol", d.GetIgnoreCrAtEol())
	args = addFlagIfTrue(args, "--ignore-space-at-eol", d.GetIgnoreSpaceAtEol())
	args = addFlagIfTrue(args, "--ignore-space-change", d.GetIgnoreSpaceChange())
	args = addFlagIfTrue(args, "--ignore-all-space", d.GetIgnoreAllSpace())
	args = addFlagIfTrue(args, "--ignore-blank-lines", d.GetIgnoreBlankLines())
	args = addRepeatedEq(args, "--ignore-matching-lines", d.GetIgnoreMatchingLines())
	args = addEqInt(args, "--inter-hunk-context", d.GetInterHunkContext())
	args = addFlagIfTrue(args, "--function-context", d.GetFunctionContext())

	// Exit / external / textconv
	args = addFlagIfTrue(args, "--exit-code", d.GetExitCode())
	args = addFlagIfTrue(args, "--quiet", d.GetQuiet())
	args = addFlagIfTrue(args, "--ext-diff", d.GetExtDiff())
	args = addFlagIfTrue(args, "--no-ext-diff", d.GetNoExtDiff())
	args = addTriBool(args, "--textconv", "--no-textconv", d.GetTextconv())

	// Submodules / prefixes
	if when := d.GetIgnoreSubmodulesWhen(); when != "" {
		args = append(args, "--ignore-submodules="+when)
	} else {
		args = addFlagIfTrue(args, "--ignore-submodules", d.GetIgnoreSubmodules())
	}
	args = addEqVal(args, "--src-prefix", d.GetSrcPrefix())
	args = addEqVal(args, "--dst-prefix", d.GetDstPrefix())
	args = addFlagIfTrue(args, "--no-prefix", d.GetNoPrefix())
	args = addEqVal(args, "--line-prefix", d.GetLinePrefix())
	args = addFlagIfTrue(args, "--ita-invisible-in-index", d.GetItaInvisibleInIndex())
	args = addFlagIfTrue(args, "--ita-visible-in-index", d.GetItaVisibleInIndex())

	return args
}

// ---- per-subcommand argv builders -----------------------------------------

func argvAdd(a *pb.GitAddArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--verbose", a.GetVerbose())
	args = addFlagIfTrue(args, "--dry-run", a.GetDryRun())
	args = addFlagIfTrue(args, "--force", a.GetForce())
	args = addFlagIfTrue(args, "--interactive", a.GetInteractive())
	args = addFlagIfTrue(args, "--patch", a.GetPatch())
	args = addFlagIfTrue(args, "--edit", a.GetEdit())
	args = addTriBool(args, "--all", "--no-all", a.GetAll())
	args = addTriBool(args, "--ignore-removal", "--no-ignore-removal", a.GetIgnoreRemoval())
	args = addFlagIfTrue(args, "--update", a.GetUpdate())
	args = addFlagIfTrue(args, "--sparse", a.GetSparse())
	args = addFlagIfTrue(args, "--intent-to-add", a.GetIntentToAdd())
	args = addFlagIfTrue(args, "--refresh", a.GetRefresh())
	args = addFlagIfTrue(args, "--ignore-errors", a.GetIgnoreErrors())
	args = addFlagIfTrue(args, "--ignore-missing", a.GetIgnoreMissing())
	args = addFlagIfTrue(args, "--renormalize", a.GetRenormalize())
	switch a.GetChmod() {
	case pb.ChmodExecutable_CHMOD_EXECUTABLE_ADD:
		args = append(args, "--chmod=+x")
	case pb.ChmodExecutable_CHMOD_EXECUTABLE_REMOVE:
		args = append(args, "--chmod=-x")
	}
	return addPathspec(args, a.GetPathspec())
}

func argvArchive(a *pb.GitArchiveArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addEqVal(args, "--format", a.GetFormat())
	args = addFlagIfTrue(args, "--list", a.GetList())
	args = addEqVal(args, "--prefix", a.GetPrefix())
	args = addEqVal(args, "--output", a.GetOutput())
	args = addFlagIfTrue(args, "--worktree-attributes", a.GetWorktreeAttributes())
	args = addFlagIfTrue(args, "--verbose", a.GetVerbose())
	args = addEqVal(args, "--remote", a.GetRemote())
	args = addEqVal(args, "--exec", a.GetExec())
	for _, f := range a.GetAddFile() {
		args = append(args, "--add-file="+f)
	}
	for _, f := range a.GetAddVirtualFile() {
		args = append(args, "--add-virtual-file="+f)
	}
	args = addEqVal(args, "--mtime", a.GetMtime())
	if lvl := a.GetCompressionLevel(); lvl > 0 {
		args = append(args, fmt.Sprintf("-%d", lvl))
	}
	args = append(args, a.GetExtra()...)
	if t := a.GetTreeIsh(); t != "" {
		args = append(args, t)
	}
	if paths := a.GetPath(); len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	return args
}

func argvBackfill(a *pb.GitBackfillArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	if bs := a.GetBatchSize(); bs > 0 {
		args = append(args, fmt.Sprintf("--batch-size=%d", bs))
	}
	args = addTriBool(args, "--sparse", "--no-sparse", a.GetSparse())
	return args
}

func argvBisect(a *pb.GitBisectArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch v := a.GetVerb().(type) {
	case *pb.GitBisectArguments_Start:
		args = append(args, "start")
		s := v.Start
		args = addEqVal(args, "--term-new", s.GetTermNew())
		args = addEqVal(args, "--term-old", s.GetTermOld())
		args = addFlagIfTrue(args, "--no-checkout", s.GetNoCheckout())
		args = addFlagIfTrue(args, "--first-parent", s.GetFirstParent())
		if s.GetBad() != "" {
			args = append(args, s.GetBad())
		}
		args = append(args, s.GetGood()...)
		args = addPathspec(args, s.GetPathspec())
	case *pb.GitBisectArguments_Bad:
		args = append(args, "bad")
		if v.Bad.GetRev() != "" {
			args = append(args, v.Bad.GetRev())
		}
	case *pb.GitBisectArguments_Good:
		args = append(args, "good")
		if v.Good.GetRev() != "" {
			args = append(args, v.Good.GetRev())
		}
		args = append(args, v.Good.GetExtraRevs()...)
	case *pb.GitBisectArguments_Skip:
		args = append(args, "skip")
		args = append(args, v.Skip.GetRevs()...)
	case *pb.GitBisectArguments_Terms:
		args = append(args, "terms")
		args = addFlagIfTrue(args, "--term-good", v.Terms.GetTermGood())
		args = addFlagIfTrue(args, "--term-bad", v.Terms.GetTermBad())
	case *pb.GitBisectArguments_Reset_:
		args = append(args, "reset")
		if c := v.Reset_.GetCommit(); c != "" {
			args = append(args, c)
		}
	case *pb.GitBisectArguments_Replay:
		args = append(args, "replay", v.Replay.GetLogfile())
	case *pb.GitBisectArguments_Run:
		args = append(args, "run")
		args = append(args, v.Run.GetCommand()...)
	case *pb.GitBisectArguments_Log:
		args = append(args, "log")
	case *pb.GitBisectArguments_Next:
		args = append(args, "next")
	case *pb.GitBisectArguments_Visualize:
		args = append(args, "visualize")
	case *pb.GitBisectArguments_Help:
		args = append(args, "help")
	}
	return args
}

func argvBranch(a *pb.GitBranchArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--verbose", a.GetVerbose())
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	if f, ok := trackFlag(a.GetTrack()); ok {
		args = append(args, f)
	}
	args = addSeparateArg(args, "--set-upstream-to", a.GetSetUpstreamTo())
	args = addFlagIfTrue(args, "--unset-upstream", a.GetUnsetUpstream())
	if f, ok := colorWhenFlag(a.GetColor()); ok {
		args = append(args, f)
	}
	args = addFlagIfTrue(args, "--no-color", a.GetNoColor())
	args = addFlagIfTrue(args, "--remotes", a.GetRemotes())
	args = addFlagIfTrue(args, "--all", a.GetAll())
	args = addSeparateArg(args, "--contains", a.GetContains())
	args = addSeparateArg(args, "--no-contains", a.GetNoContains())
	if a.GetAbbrev() > 0 {
		args = append(args, fmt.Sprintf("--abbrev=%d", a.GetAbbrev()))
	}
	args = addFlagIfTrue(args, "--no-abbrev", a.GetNoAbbrev())
	args = addSeparateArg(args, "--merged", a.GetMerged())
	args = addSeparateArg(args, "--no-merged", a.GetNoMerged())
	args = addEqVal(args, "--column", a.GetColumn())
	args = addEqVal(args, "--sort", a.GetSort())
	args = addSeparateArg(args, "--points-at", a.GetPointsAt())
	args = addFlagIfTrue(args, "--ignore-case", a.GetIgnoreCase())
	if f, ok := recurseSubFlag(a.GetRecurseSubmodules()); ok {
		args = append(args, f)
	}
	args = addEqVal(args, "--format", a.GetFormat())
	args = addFlagIfTrue(args, "--force", a.GetForce())
	args = addFlagIfTrue(args, "--create-reflog", a.GetCreateReflog())

	switch a.GetAction() {
	case pb.GitBranchArguments_ACTION_LIST:
		args = append(args, "--list")
	case pb.GitBranchArguments_ACTION_DELETE:
		if a.GetForce() == pb.OptBool_OPT_BOOL_TRUE {
			args = append(args, "-D")
		} else {
			args = append(args, "-d")
		}
	case pb.GitBranchArguments_ACTION_MOVE:
		if a.GetForce() == pb.OptBool_OPT_BOOL_TRUE {
			args = append(args, "-M")
		} else {
			args = append(args, "-m")
		}
	case pb.GitBranchArguments_ACTION_COPY:
		if a.GetForce() == pb.OptBool_OPT_BOOL_TRUE {
			args = append(args, "-C")
		} else {
			args = append(args, "-c")
		}
	case pb.GitBranchArguments_ACTION_EDIT_DESCRIPTION:
		args = append(args, "--edit-description")
	case pb.GitBranchArguments_ACTION_SHOW_CURRENT:
		args = append(args, "--show-current")
	}
	args = append(args, a.GetNames()...)
	if sp := a.GetStartPoint(); sp != "" {
		args = append(args, sp)
	}
	return args
}

func argvBundle(a *pb.GitBundleArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch v := a.GetVerb().(type) {
	case *pb.GitBundleArguments_Create:
		c := v.Create
		args = append(args, "create")
		args = addFlagIfTrue(args, "--quiet", c.GetQuiet())
		args = addTriBool(args, "--progress", "--no-progress", c.GetProgress())
		args = addFlagIfTrue(args, "--all-progress", c.GetAllProgress())
		args = addFlagIfTrue(args, "--all-progress-implied", c.GetAllProgressImplied())
		if c.GetVersion() > 0 {
			args = append(args, fmt.Sprintf("--version=%d", c.GetVersion()))
		}
		args = append(args, c.GetFile())
		args = append(args, c.GetRevListArgs()...)
	case *pb.GitBundleArguments_Verify:
		args = append(args, "verify")
		args = addFlagIfTrue(args, "--quiet", v.Verify.GetQuiet())
		args = append(args, v.Verify.GetFile())
	case *pb.GitBundleArguments_ListHeads:
		args = append(args, "list-heads", v.ListHeads.GetFile())
		args = append(args, v.ListHeads.GetRefname()...)
	case *pb.GitBundleArguments_Unbundle:
		args = append(args, "unbundle")
		args = addFlagIfTrue(args, "--progress", v.Unbundle.GetProgress())
		args = append(args, v.Unbundle.GetFile())
		args = append(args, v.Unbundle.GetRefname()...)
	}
	return args
}

func argvCheckout(a *pb.GitCheckoutArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	args = addTriBool(args, "--progress", "--no-progress", a.GetProgress())
	args = addFlagIfTrue(args, "--force", a.GetForce())
	args = addFlagIfTrue(args, "--ours", a.GetOurs())
	args = addFlagIfTrue(args, "--theirs", a.GetTheirs())
	args = addSeparateArg(args, "-b", a.GetNewBranch())
	args = addSeparateArg(args, "-B", a.GetNewBranchForce())
	if f, ok := trackFlag(a.GetTrack()); ok {
		args = append(args, f)
	}
	args = addTriBool(args, "--guess", "--no-guess", a.GetGuess())
	args = addFlagIfTrue(args, "--detach", a.GetDetach())
	args = addSeparateArg(args, "--orphan", a.GetOrphan())
	args = addFlagIfTrue(args, "--ignore-skip-worktree-bits", a.GetIgnoreSkipWorktreeBits())
	args = addFlagIfTrue(args, "-m", a.GetMerge())
	args = addEqVal(args, "--conflict", a.GetConflictStyle())
	args = addFlagIfTrue(args, "--patch", a.GetPatch())
	if f, ok := recurseSubFlag(a.GetRecurseSubmodules()); ok {
		args = append(args, f)
	}
	args = addTriBool(args, "--overlay", "--no-overlay", a.GetOverlay())
	args = addEqVal(args, "--pathspec-from-file", a.GetPathspecFromFile())
	args = addFlagIfTrue(args, "--pathspec-file-nul", a.GetPathspecFileNul())
	if sp := a.GetStartPoint(); sp != "" {
		args = append(args, sp)
	}
	return addPathspec(args, a.GetPathspec())
}

func argvCherryPick(a *pb.GitCherryPickArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch a.GetControl() {
	case pb.GitCherryPickArguments_CONTROL_CONTINUE:
		return []string{"--continue"}
	case pb.GitCherryPickArguments_CONTROL_SKIP:
		return []string{"--skip"}
	case pb.GitCherryPickArguments_CONTROL_ABORT:
		return []string{"--abort"}
	case pb.GitCherryPickArguments_CONTROL_QUIT:
		return []string{"--quit"}
	}
	args = addFlagIfTrue(args, "--edit", a.GetEdit())
	args = addEqVal(args, "--cleanup", a.GetCleanup())
	if m := a.GetMainline(); m > 0 {
		args = append(args, "-m", strconv.Itoa(int(m)))
	}
	args = addFlagIfTrue(args, "--no-commit", a.GetNoCommit())
	args = addFlagIfTrue(args, "--signoff", a.GetSignoff())
	args = addGpgSign(args, a.GetGpgSign())
	args = addFlagIfTrue(args, "--ff", a.GetFastForward())
	args = addFlagIfTrue(args, "--allow-empty", a.GetAllowEmpty())
	args = addFlagIfTrue(args, "--allow-empty-message", a.GetAllowEmptyMessage())
	args = addFlagIfTrue(args, "--keep-redundant-commits", a.GetKeepRedundantCommits())
	args = addEqVal(args, "--strategy", a.GetStrategy())
	for _, o := range a.GetStrategyOption() {
		args = append(args, "-X", o)
	}
	if r := a.GetRerereAutoupdate(); r != "" {
		args = append(args, r)
	}
	args = addFlagIfTrue(args, "--reference", a.GetReference())
	args = addFlagIfTrue(args, "-x", a.GetX())
	args = append(args, a.GetCommits()...)
	return args
}

func argvClean(a *pb.GitCleanArguments) []string {
	var args []string
	if a == nil {
		// Match the string-arg Clean RPC's safety default: an empty request
		// becomes --dry-run so callers can't unintentionally rm -rf untracked
		// files just by forgetting to set a flag.
		return []string{"--dry-run"}
	}
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	// Same safety default as above when the caller has expressed no
	// destructive intent (no --force, no --interactive, no explicit --dry-run).
	if a.GetDryRun() != pb.OptBool_OPT_BOOL_TRUE &&
		a.GetForce() != pb.OptBool_OPT_BOOL_TRUE &&
		a.GetInteractive() != pb.OptBool_OPT_BOOL_TRUE {
		args = append(args, "--dry-run")
	} else {
		args = addFlagIfTrue(args, "--dry-run", a.GetDryRun())
	}
	args = addFlagIfTrue(args, "--force", a.GetForce())
	args = addFlagIfTrue(args, "--interactive", a.GetInteractive())
	args = addFlagIfTrue(args, "-d", a.GetDirectories())
	for _, e := range a.GetExclude() {
		args = append(args, "-e", e)
	}
	args = addFlagIfTrue(args, "-x", a.GetIncludeIgnored())
	args = addFlagIfTrue(args, "-X", a.GetOnlyIgnored())
	return addPathspec(args, a.GetPathspec())
}

func argvCommit(a *pb.GitCommitArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--all", a.GetAll())
	args = addFlagIfTrue(args, "--patch", a.GetPatch())
	args = addFlagIfTrue(args, "--reset-author", a.GetResetAuthor())
	args = addIdentity(args, a.GetIdentity())
	args = addMessageSource(args, a.GetMessage())
	args = addFlagIfTrue(args, "--allow-empty", a.GetAllowEmpty())
	args = addFlagIfTrue(args, "--allow-empty-message", a.GetAllowEmptyMessage())
	args = addFlagIfTrue(args, "--no-verify", a.GetNoVerify())
	args = addFlagIfTrue(args, "--edit", a.GetEdit())
	args = addEqVal(args, "--cleanup", a.GetCleanup())
	args = addFlagIfTrue(args, "--amend", a.GetAmend())
	args = addFlagIfTrue(args, "--no-post-rewrite", a.GetNoPostRewrite())
	for _, t := range a.GetTrailers() {
		args = append(args, "--trailer", t)
	}
	args = addFlagIfTrue(args, "--signoff", a.GetSignoff())
	args = addGpgSign(args, a.GetGpgSign())
	if f, ok := untrackedFilesFlag(a.GetUntrackedFiles()); ok {
		args = append(args, f)
	}
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	args = addFlagIfTrue(args, "--verbose", a.GetVerbose())
	args = addFlagIfTrue(args, "--dry-run", a.GetDryRun())
	switch a.GetStatusFormat() {
	case pb.StatusFormat_STATUS_FORMAT_SHORT:
		args = append(args, "--short")
	case pb.StatusFormat_STATUS_FORMAT_LONG:
		args = append(args, "--long")
	case pb.StatusFormat_STATUS_FORMAT_PORCELAIN_V1:
		args = append(args, "--porcelain")
	case pb.StatusFormat_STATUS_FORMAT_PORCELAIN_V2:
		args = append(args, "--porcelain=v2")
	}
	args = addTriBool(args, "--status", "--no-status", a.GetIncludeStatus())
	args = addFlagIfTrue(args, "--branch", a.GetBranch())
	args = addTriBool(args, "--ahead-behind", "--no-ahead-behind", a.GetAheadBehind())
	args = addFlagIfTrue(args, "-z", a.GetZ())
	args = addFlagIfTrue(args, "-i", a.GetInclude())
	args = addFlagIfTrue(args, "-o", a.GetOnly())
	return addPathspec(args, a.GetPathspec())
}

func argvDescribe(a *pb.GitDescribeArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--all", a.GetAll())
	args = addFlagIfTrue(args, "--tags", a.GetTags())
	args = addFlagIfTrue(args, "--contains", a.GetContains())
	if a.GetAbbrevIsSet() {
		args = append(args, fmt.Sprintf("--abbrev=%d", a.GetAbbrev()))
	}
	if c := a.GetCandidates(); c > 0 {
		args = append(args, fmt.Sprintf("--candidates=%d", c))
	}
	if m := a.GetDirtyMark(); m != "" {
		args = append(args, "--dirty="+m)
	} else if a.GetDirty() == pb.OptBool_OPT_BOOL_TRUE {
		args = append(args, "--dirty")
	}
	if m := a.GetBrokenMark(); m != "" {
		args = append(args, "--broken="+m)
	} else if a.GetBroken() == pb.OptBool_OPT_BOOL_TRUE {
		args = append(args, "--broken")
	}
	args = addFlagIfTrue(args, "--exact-match", a.GetExactMatch())
	args = addFlagIfTrue(args, "--debug", a.GetDebug())
	args = addFlagIfTrue(args, "--long", a.GetLong())
	args = addEqVal(args, "--match", a.GetMatch())
	for _, m := range a.GetMatches() {
		args = append(args, "--match="+m)
	}
	for _, e := range a.GetExclude() {
		args = append(args, "--exclude="+e)
	}
	args = addFlagIfTrue(args, "--always", a.GetAlways())
	args = addFlagIfTrue(args, "--first-parent", a.GetFirstParent())
	return append(args, a.GetCommitIsh()...)
}

func argvDiff(a *pb.GitDiffArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addDiffFormatting(args, a.GetFormatting())
	args = addFlagIfTrue(args, "--cached", a.GetCached())
	args = addFlagIfTrue(args, "--staged", a.GetStaged())
	args = addFlagIfTrue(args, "--merge-base", a.GetMergeBase())
	args = addFlagIfTrue(args, "--no-index", a.GetNoIndex())
	args = addFlagIfTrue(args, "--index", a.GetIndex())
	args = addFlagIfTrue(args, "--base", a.GetBase())
	args = addFlagIfTrue(args, "--ours", a.GetOurs())
	args = addFlagIfTrue(args, "--theirs", a.GetTheirs())
	args = addFlagIfTrue(args, "-0", a.GetCombinedDiff())
	args = append(args, a.GetCommits()...)
	return addPathspec(args, a.GetPathspec())
}

func argvGc(a *pb.GitGcArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--aggressive", a.GetAggressive())
	args = addFlagIfTrue(args, "--auto", a.GetAuto())
	args = addEqVal(args, "--prune", a.GetPrune())
	args = addFlagIfTrue(args, "--no-prune", a.GetNoPrune())
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	args = addFlagIfTrue(args, "--force", a.GetForce())
	args = addFlagIfTrue(args, "--keep-largest-pack", a.GetKeepLargestPack())
	args = addTriBool(args, "--cruft", "--no-cruft", a.GetCruft())
	args = addEqVal(args, "--cruft-expiration", a.GetCruftExpiration())
	return args
}

func argvInit(a *pb.GitInitArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	args = addFlagIfTrue(args, "--bare", a.GetBare())
	args = addEqVal(args, "--template", a.GetTemplate())
	args = addEqVal(args, "--separate-git-dir", a.GetSeparateGitDir())
	args = addEqVal(args, "--object-format", a.GetObjectFormat())
	args = addEqVal(args, "--initial-branch", a.GetInitialBranch())
	if s := a.GetShared(); s != "" {
		args = append(args, "--shared="+s)
	} else {
		args = addFlagIfTrue(args, "--shared", a.GetSharedFlag())
	}
	if d := a.GetDirectory(); d != "" {
		args = append(args, d)
	}
	return args
}

func argvLog(a *pb.GitLogArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addDiffFormatting(args, a.GetFormatting())

	// Limiting
	args = addEqInt(args, "--max-count", a.GetMaxCount())
	args = addEqInt(args, "--skip", a.GetSkip())
	args = addEqVal(args, "--since", a.GetSince())
	args = addEqVal(args, "--since-as-filter", a.GetSinceAsFilter())
	args = addEqVal(args, "--until", a.GetUntil())
	args = addEqVal(args, "--author", a.GetAuthor())
	args = addEqVal(args, "--committer", a.GetCommitter())
	args = addEqVal(args, "--grep-reflog", a.GetGrepReflog())
	args = addRepeatedEq(args, "--grep", a.GetGrep())
	args = addFlagIfTrue(args, "--all-match", a.GetAllMatch())
	args = addFlagIfTrue(args, "--invert-grep", a.GetInvertGrep())
	args = addFlagIfTrue(args, "--regexp-ignore-case", a.GetRegexpIgnoreCase())
	args = addFlagIfTrue(args, "--basic-regexp", a.GetBasicRegexp())
	args = addFlagIfTrue(args, "--extended-regexp", a.GetExtendedRegexp())
	args = addFlagIfTrue(args, "--fixed-strings", a.GetFixedStrings())
	args = addFlagIfTrue(args, "--perl-regexp", a.GetPerlRegexp())
	args = addFlagIfTrue(args, "--remove-empty", a.GetRemoveEmpty())
	args = addFlagIfTrue(args, "--merges", a.GetMerges())
	args = addFlagIfTrue(args, "--no-merges", a.GetNoMerges())
	args = addEqInt(args, "--min-parents", a.GetMinParents())
	args = addEqInt(args, "--max-parents", a.GetMaxParents())
	args = addFlagIfTrue(args, "--no-min-parents", a.GetNoMinParents())
	args = addFlagIfTrue(args, "--no-max-parents", a.GetNoMaxParents())
	args = addFlagIfTrue(args, "--first-parent", a.GetFirstParent())
	args = addFlagIfTrue(args, "--exclude-first-parent-only", a.GetExcludeFirstParentOnly())
	args = addFlagIfTrue(args, "--not", a.GetNot())

	// Revision selection
	args = addFlagIfTrue(args, "--all", a.GetAll())
	if p := a.GetBranchesPattern(); p != "" {
		args = append(args, "--branches="+p)
	} else {
		args = addFlagIfTrue(args, "--branches", a.GetBranches())
	}
	if p := a.GetTagsPattern(); p != "" {
		args = append(args, "--tags="+p)
	} else {
		args = addFlagIfTrue(args, "--tags", a.GetTags())
	}
	if p := a.GetRemotesPattern(); p != "" {
		args = append(args, "--remotes="+p)
	} else {
		args = addFlagIfTrue(args, "--remotes", a.GetRemotes())
	}
	args = addRepeatedEq(args, "--glob", a.GetGlob())
	args = addRepeatedEq(args, "--exclude", a.GetExcludeRef())
	args = addEqVal(args, "--exclude-hidden", a.GetExcludeHidden())
	args = addFlagIfTrue(args, "--reflog", a.GetReflog())
	args = addFlagIfTrue(args, "--alternate-refs", a.GetAlternateRefs())
	args = addFlagIfTrue(args, "--single-worktree", a.GetSingleWorktree())
	args = addFlagIfTrue(args, "--ignore-missing", a.GetIgnoreMissing())
	args = addFlagIfTrue(args, "--bisect", a.GetBisect())
	args = addFlagIfTrue(args, "--stdin", a.GetStdin())
	args = addFlagIfTrue(args, "--cherry-mark", a.GetCherryMark())
	args = addFlagIfTrue(args, "--cherry-pick", a.GetCherryPick())
	args = addFlagIfTrue(args, "--left-only", a.GetLeftOnly())
	args = addFlagIfTrue(args, "--right-only", a.GetRightOnly())
	args = addFlagIfTrue(args, "--cherry", a.GetCherry())
	args = addFlagIfTrue(args, "--walk-reflogs", a.GetWalkReflogs())
	args = addFlagIfTrue(args, "--merge", a.GetMerge())
	args = addFlagIfTrue(args, "--boundary", a.GetBoundary())

	// History simplification
	args = addFlagIfTrue(args, "--simplify-by-decoration", a.GetSimplifyByDecoration())
	args = addFlagIfTrue(args, "--show-pulls", a.GetShowPulls())
	args = addFlagIfTrue(args, "--full-history", a.GetFullHistory())
	args = addFlagIfTrue(args, "--dense", a.GetDense())
	args = addFlagIfTrue(args, "--sparse", a.GetSparse())
	args = addFlagIfTrue(args, "--simplify-merges", a.GetSimplifyMerges())
	if c := a.GetAncestryPathCommit(); c != "" {
		args = append(args, "--ancestry-path="+c)
	} else {
		args = addFlagIfTrue(args, "--ancestry-path", a.GetAncestryPath())
	}

	// Ordering
	args = addFlagIfTrue(args, "--date-order", a.GetDateOrder())
	args = addFlagIfTrue(args, "--author-date-order", a.GetAuthorDateOrder())
	args = addFlagIfTrue(args, "--topo-order", a.GetTopoOrder())
	args = addFlagIfTrue(args, "--reverse", a.GetReverse())
	if m := a.GetNoWalkMode(); m != "" {
		args = append(args, "--no-walk="+m)
	} else {
		args = addFlagIfTrue(args, "--no-walk", a.GetNoWalk())
	}
	args = addFlagIfTrue(args, "--do-walk", a.GetDoWalk())

	// Commit formatting
	if p := a.GetPretty(); p != "" {
		args = append(args, "--pretty="+p)
	} else {
		args = addFlagIfTrue(args, "--pretty", a.GetPrettyFlag())
	}
	args = addFlagIfTrue(args, "--abbrev-commit", a.GetAbbrevCommit())
	args = addFlagIfTrue(args, "--no-abbrev-commit", a.GetNoAbbrevCommit())
	args = addFlagIfTrue(args, "--oneline", a.GetOneline())
	args = addEqVal(args, "--encoding", a.GetEncoding())
	if w := a.GetExpandTabsWidth(); w != nil && w.GetPresent() {
		args = append(args, fmt.Sprintf("--expand-tabs=%d", w.GetValue()))
	} else {
		args = addFlagIfTrue(args, "--expand-tabs", a.GetExpandTabs())
	}
	args = addFlagIfTrue(args, "--no-expand-tabs", a.GetNoExpandTabs())
	if r := a.GetNotesRef(); r != "" {
		args = append(args, "--notes="+r)
	} else {
		args = addFlagIfTrue(args, "--notes", a.GetNotes())
	}
	args = addFlagIfTrue(args, "--no-notes", a.GetNoNotes())
	if r := a.GetShowNotesRef(); r != "" {
		args = append(args, "--show-notes="+r)
	} else {
		args = addFlagIfTrue(args, "--show-notes", a.GetShowNotes())
	}
	args = addTriBool(args, "--standard-notes", "--no-standard-notes", a.GetStandardNotes())
	args = addFlagIfTrue(args, "--show-signature", a.GetShowSignature())
	args = addFlagIfTrue(args, "--relative-date", a.GetRelativeDate())
	args = addEqVal(args, "--date", a.GetDateFormat())
	args = addFlagIfTrue(args, "--parents", a.GetParents())
	args = addFlagIfTrue(args, "--children", a.GetChildren())
	args = addFlagIfTrue(args, "--left-right", a.GetLeftRight())
	args = addFlagIfTrue(args, "--graph", a.GetGraph())
	if b := a.GetShowLinearBreakBarrier(); b != "" {
		args = append(args, "--show-linear-break="+b)
	} else {
		args = addFlagIfTrue(args, "--show-linear-break", a.GetShowLinearBreak())
	}

	// Log-specific
	args = addFlagIfTrue(args, "--follow", a.GetFollow())
	args = addFlagIfTrue(args, "--no-decorate", a.GetNoDecorate())
	if f, ok := decorateFlag(a.GetDecorate()); ok {
		args = append(args, f)
	} else {
		args = addFlagIfTrue(args, "--decorate", a.GetDecorateFlag())
	}
	args = addRepeatedEq(args, "--decorate-refs", a.GetDecorateRefs())
	args = addRepeatedEq(args, "--decorate-refs-exclude", a.GetDecorateRefsExclude())
	args = addFlagIfTrue(args, "--clear-decorations", a.GetClearDecorations())
	args = addFlagIfTrue(args, "--source", a.GetSource())
	args = addTriBool(args, "--mailmap", "--no-mailmap", a.GetMailmap())
	args = addTriBool(args, "--use-mailmap", "--no-use-mailmap", a.GetUseMailmap())
	args = addFlagIfTrue(args, "--full-diff", a.GetFullDiff())
	args = addFlagIfTrue(args, "--log-size", a.GetLogSize())
	for _, l := range a.GetLineRange() {
		args = append(args, "-L", l)
	}

	// Diff-merges
	args = addEqVal(args, "--diff-merges", a.GetDiffMerges())
	args = addFlagIfTrue(args, "--no-diff-merges", a.GetNoDiffMerges())
	args = addFlagIfTrue(args, "--combined-all-paths", a.GetCombinedAllPaths())
	args = addFlagIfTrue(args, "-t", a.GetDashT())
	args = addFlagIfTrue(args, "-m", a.GetDashM())
	args = addFlagIfTrue(args, "-c", a.GetDashC())
	args = addFlagIfTrue(args, "--cc", a.GetDashCc())
	args = addFlagIfTrue(args, "--remerge-diff", a.GetRemergeDiff())

	// Positional
	args = append(args, a.GetRevisionRange()...)
	return addPathspec(args, a.GetPathspec())
}

func argvMaintenance(a *pb.GitMaintenanceArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch a.GetVerb() {
	case pb.GitMaintenanceArguments_VERB_RUN:
		args = append(args, "run")
	case pb.GitMaintenanceArguments_VERB_START:
		args = append(args, "start")
	case pb.GitMaintenanceArguments_VERB_STOP:
		args = append(args, "stop")
	case pb.GitMaintenanceArguments_VERB_REGISTER:
		args = append(args, "register")
	case pb.GitMaintenanceArguments_VERB_UNREGISTER:
		args = append(args, "unregister")
	}
	args = addEqVal(args, "--schedule", a.GetSchedule())
	for _, t := range a.GetTask() {
		args = append(args, "--task="+t)
	}
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	args = addFlagIfTrue(args, "--auto", a.GetAuto())
	args = addEqVal(args, "--config-file", a.GetConfigFile())
	args = addFlagIfTrue(args, "--force", a.GetForce())
	return args
}

func argvMerge(a *pb.GitMergeArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch a.GetControl() {
	case pb.GitMergeArguments_CONTROL_CONTINUE:
		return []string{"--continue"}
	case pb.GitMergeArguments_CONTROL_ABORT:
		return []string{"--abort"}
	case pb.GitMergeArguments_CONTROL_QUIT:
		return []string{"--quit"}
	}
	if f, ok := fastForwardFlag(a.GetFastForward()); ok {
		args = append(args, f)
	}
	args = addFlagIfTrue(args, "--squash", a.GetSquash())
	args = addTriBool(args, "--commit", "--no-commit", a.GetCommit())
	args = addFlagIfTrue(args, "--edit", a.GetEdit())
	args = addFlagIfTrue(args, "--verify-signatures", a.GetVerifySignatures())
	args = addFlagIfTrue(args, "--signoff", a.GetSignoff())
	args = addFlagIfTrue(args, "--stat", a.GetStat())
	args = addFlagIfTrue(args, "--progress", a.GetProgress())
	args = addGpgSign(args, a.GetGpgSign())
	args = addTriBool(args, "--autostash", "--no-autostash", a.GetAutostash())
	args = addFlagIfTrue(args, "--allow-unrelated-histories", a.GetAllowUnrelatedHistories())
	args = addTriBool(args, "--rerere-autoupdate", "--no-rerere-autoupdate", a.GetRerereAutoupdate())
	args = addTriBool(args, "--overwrite-ignore", "--no-overwrite-ignore", a.GetOverwriteIgnore())
	for _, st := range a.GetStrategy() {
		args = append(args, "-s", st)
	}
	for _, o := range a.GetStrategyOption() {
		args = append(args, "-X", o)
	}
	args = addMessageSource(args, a.GetMessage())
	args = addSeparateArg(args, "--into-name", a.GetIntoName())
	if l := a.GetLog(); l != "" {
		args = append(args, "--log="+l)
	} else {
		args = addFlagIfTrue(args, "--log", a.GetLogFlag())
	}
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	args = addFlagIfTrue(args, "--verbose", a.GetVerbose())
	return append(args, a.GetCommits()...)
}

func argvMv(a *pb.GitMvArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--force", a.GetForce())
	args = addFlagIfTrue(args, "--dry-run", a.GetDryRun())
	args = addFlagIfTrue(args, "-k", a.GetSkipErrors())
	args = addFlagIfTrue(args, "--verbose", a.GetVerbose())
	args = addFlagIfTrue(args, "--sparse", a.GetSparse())
	args = append(args, a.GetSources()...)
	if d := a.GetDestination(); d != "" {
		args = append(args, d)
	}
	return args
}

func argvNotes(a *pb.GitNotesArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addEqVal(args, "--ref", a.GetRef())
	switch a.GetVerb() {
	case pb.GitNotesArguments_VERB_LIST:
		args = append(args, "list")
	case pb.GitNotesArguments_VERB_ADD:
		args = append(args, "add")
	case pb.GitNotesArguments_VERB_COPY:
		args = append(args, "copy")
	case pb.GitNotesArguments_VERB_APPEND:
		args = append(args, "append")
	case pb.GitNotesArguments_VERB_EDIT:
		args = append(args, "edit")
	case pb.GitNotesArguments_VERB_SHOW:
		args = append(args, "show")
	case pb.GitNotesArguments_VERB_MERGE:
		args = append(args, "merge")
	case pb.GitNotesArguments_VERB_REMOVE:
		args = append(args, "remove")
	case pb.GitNotesArguments_VERB_PRUNE:
		args = append(args, "prune")
	case pb.GitNotesArguments_VERB_GET_REF:
		args = append(args, "get-ref")
	}
	args = addFlagIfTrue(args, "--force", a.GetForce())
	args = addMessageSource(args, a.GetMessage())
	args = addFlagIfTrue(args, "--allow-empty", a.GetAllowEmpty())
	args = addTriBool(args, "--stripspace", "--no-stripspace", a.GetStripspace())
	args = addSeparateArg(args, "-s", a.GetStrategy())
	args = addFlagIfTrue(args, "--ignore-missing", a.GetIgnoreMissing())
	if f := a.GetFromObject(); f != "" {
		args = append(args, "--from="+f)
	}
	args = append(args, a.GetObjects()...)
	args = append(args, a.GetExtra()...)
	return args
}

func argvPush(a *pb.GitPushArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--all", a.GetAll())
	args = addFlagIfTrue(args, "--prune", a.GetPrune())
	args = addFlagIfTrue(args, "--mirror", a.GetMirror())
	args = addFlagIfTrue(args, "--dry-run", a.GetDryRun())
	args = addFlagIfTrue(args, "--porcelain", a.GetPorcelain())
	args = addFlagIfTrue(args, "--delete", a.GetDelete())
	args = addFlagIfTrue(args, "--tags", a.GetTags())
	args = addFlagIfTrue(args, "--follow-tags", a.GetFollowTags())
	args = addEqVal(args, "--receive-pack", a.GetReceivePack())
	args = addEqVal(args, "--repo", a.GetRepo())
	args = addFlagIfTrue(args, "--force", a.GetForce())
	if v := a.GetForceWithLease(); v != "" {
		args = append(args, "--force-with-lease="+v)
	} else {
		args = addFlagIfTrue(args, "--force-with-lease", a.GetForceWithLeaseFlag())
	}
	args = addEqVal(args, "--force-if-includes", a.GetForceIfIncludes())
	if f, ok := recurseSubFlag(a.GetRecurseSubmodules()); ok {
		args = append(args, f)
	}
	args = addTriBool(args, "--thin", "--no-thin", a.GetThin())
	args = addFlagIfTrue(args, "--atomic", a.GetAtomic())
	args = addTriBool(args, "--signed", "--no-signed", a.GetSigned())
	args = addEqVal(args, "--signed", a.GetSignedMode())
	args = addTriBool(args, "--verify", "--no-verify", a.GetVerify())
	args = addFlagIfTrue(args, "--set-upstream", a.GetSetUpstream())
	args = addFlagIfTrue(args, "--progress", a.GetProgress())
	args = addFlagIfTrue(args, "--verbose", a.GetVerbose())
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	for _, p := range a.GetPushOption() {
		args = append(args, "--push-option="+p)
	}
	args = addFlagIfTrue(args, "-4", a.GetIpv4())
	args = addFlagIfTrue(args, "-6", a.GetIpv6())
	if r := a.GetRepository(); r != "" {
		args = append(args, r)
	}
	return append(args, a.GetRefspec()...)
}

func argvRangeDiff(a *pb.GitRangeDiffArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	if a.GetCreationFactor() == pb.OptBool_OPT_BOOL_TRUE {
		args = append(args, fmt.Sprintf("--creation-factor=%d", a.GetCreationFactorValue()))
	}
	args = addFlagIfTrue(args, "--no-dual-color", a.GetNoDualColor())
	if r := a.GetNotes(); r != "" {
		args = append(args, "--notes="+r)
	} else {
		args = addFlagIfTrue(args, "--notes", a.GetNotesFlag())
	}
	args = addFlagIfTrue(args, "--left-only", a.GetLeftOnly())
	args = addFlagIfTrue(args, "--right-only", a.GetRightOnly())
	args = append(args, a.GetExtra()...)
	args = append(args, a.GetRanges()...)
	return addPathspec(args, a.GetPathspec())
}

func argvRebase(a *pb.GitRebaseArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch a.GetControl() {
	case pb.GitRebaseArguments_CONTROL_CONTINUE:
		return []string{"--continue"}
	case pb.GitRebaseArguments_CONTROL_SKIP:
		return []string{"--skip"}
	case pb.GitRebaseArguments_CONTROL_ABORT:
		return []string{"--abort"}
	case pb.GitRebaseArguments_CONTROL_QUIT:
		return []string{"--quit"}
	case pb.GitRebaseArguments_CONTROL_EDIT_TODO:
		return []string{"--edit-todo"}
	case pb.GitRebaseArguments_CONTROL_SHOW_CURRENT_PATCH:
		return []string{"--show-current-patch"}
	}
	args = addFlagIfTrue(args, "--interactive", a.GetInteractive())
	args = addFlagIfTrue(args, "--root", a.GetRoot())
	args = addEqVal(args, "--onto", a.GetOnto())
	args = addFlagIfTrue(args, "--preserve-merges", a.GetPreserveMerges())
	if m := a.GetRebaseMergesMode(); m != "" {
		args = append(args, "--rebase-merges="+m)
	} else {
		args = addFlagIfTrue(args, "--rebase-merges", a.GetRebaseMerges())
	}
	args = addTriBool(args, "--autosquash", "--no-autosquash", a.GetAutosquash())
	args = addFlagIfTrue(args, "--autostash", a.GetAutostash())
	args = addTriBool(args, "--fork-point", "--no-fork-point", a.GetForkPoint())
	if f, ok := fastForwardFlag(a.GetFastForward()); ok {
		args = append(args, f)
	}
	args = addTriBool(args, "--keep-empty", "--no-keep-empty", a.GetKeepEmpty())
	if m := a.GetEmptyMode(); m != "" {
		args = append(args, "--empty="+m)
	} else {
		args = addFlagIfTrue(args, "--empty", a.GetEmpty())
	}
	args = addFlagIfTrue(args, "--no-verify", a.GetNoVerify())
	args = addFlagIfTrue(args, "--verify", a.GetVerify())
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	args = addFlagIfTrue(args, "--verbose", a.GetVerbose())
	args = addFlagIfTrue(args, "--stat", a.GetStat())
	args = addGpgSign(args, a.GetGpgSign())
	args = addFlagIfTrue(args, "--signoff", a.GetSignoff())
	args = addEqVal(args, "--strategy", a.GetStrategy())
	for _, o := range a.GetStrategyOption() {
		args = append(args, "-X", o)
	}
	for _, e := range a.GetExecs() {
		args = append(args, "-x", e)
	}
	args = addFlagIfTrue(args, "--reschedule-failed-exec", a.GetRescheduleFailedExec())
	args = addTriBool(args, "--update-refs", "--no-update-refs", a.GetUpdateRefs())
	if u := a.GetUpstream(); u != "" {
		args = append(args, u)
	}
	if b := a.GetBranch(); b != "" {
		args = append(args, b)
	}
	return args
}

func argvReset(a *pb.GitResetArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch a.GetMode() {
	case pb.GitResetArguments_MODE_SOFT:
		args = append(args, "--soft")
	case pb.GitResetArguments_MODE_MIXED:
		args = append(args, "--mixed")
	case pb.GitResetArguments_MODE_HARD:
		args = append(args, "--hard")
	case pb.GitResetArguments_MODE_MERGE:
		args = append(args, "--merge")
	case pb.GitResetArguments_MODE_KEEP:
		args = append(args, "--keep")
	}
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	args = addFlagIfTrue(args, "--no-refresh", a.GetNoRefresh())
	args = addFlagIfTrue(args, "--patch", a.GetPatch())
	if c := a.GetCommit(); c != "" {
		args = append(args, c)
	}
	return addPathspec(args, a.GetPathspec())
}

func argvRestore(a *pb.GitRestoreArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addEqVal(args, "--source", a.GetSource())
	args = addFlagIfTrue(args, "--patch", a.GetPatch())
	args = addFlagIfTrue(args, "--worktree", a.GetWorktree())
	args = addFlagIfTrue(args, "--staged", a.GetStaged())
	args = addFlagIfTrue(args, "--ours", a.GetOurs())
	args = addFlagIfTrue(args, "--theirs", a.GetTheirs())
	args = addFlagIfTrue(args, "-m", a.GetMerge())
	args = addEqVal(args, "--conflict", a.GetConflictStyle())
	args = addFlagIfTrue(args, "--ignore-unmerged", a.GetIgnoreUnmerged())
	args = addFlagIfTrue(args, "--ignore-skip-worktree-bits", a.GetIgnoreSkipWorktreeBits())
	if f, ok := recurseSubFlag(a.GetRecurseSubmodules()); ok {
		args = append(args, f)
	}
	args = addTriBool(args, "--overlay", "--no-overlay", a.GetOverlay())
	args = addFlagIfTrue(args, "--progress", a.GetProgress())
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	return addPathspec(args, a.GetPathspec())
}

func argvRevert(a *pb.GitRevertArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch a.GetControl() {
	case pb.GitRevertArguments_CONTROL_CONTINUE:
		return []string{"--continue"}
	case pb.GitRevertArguments_CONTROL_SKIP:
		return []string{"--skip"}
	case pb.GitRevertArguments_CONTROL_ABORT:
		return []string{"--abort"}
	case pb.GitRevertArguments_CONTROL_QUIT:
		return []string{"--quit"}
	}
	args = addFlagIfTrue(args, "--edit", a.GetEdit())
	args = addFlagIfTrue(args, "--no-edit", a.GetNoEdit())
	args = addEqVal(args, "--cleanup", a.GetCleanup())
	if m := a.GetMainline(); m > 0 {
		args = append(args, "-m", strconv.Itoa(int(m)))
	}
	args = addFlagIfTrue(args, "--no-commit", a.GetNoCommit())
	args = addGpgSign(args, a.GetGpgSign())
	args = addFlagIfTrue(args, "--signoff", a.GetSignoff())
	args = addEqVal(args, "--strategy", a.GetStrategy())
	for _, o := range a.GetStrategyOption() {
		args = append(args, "-X", o)
	}
	args = addTriBool(args, "--rerere-autoupdate", "--no-rerere-autoupdate", a.GetRerereAutoupdate())
	args = addFlagIfTrue(args, "--reference", a.GetReference())
	return append(args, a.GetCommits()...)
}

func argvRm(a *pb.GitRmArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--force", a.GetForce())
	args = addFlagIfTrue(args, "--dry-run", a.GetDryRun())
	args = addFlagIfTrue(args, "-r", a.GetRecursive())
	args = addFlagIfTrue(args, "--cached", a.GetCached())
	args = addFlagIfTrue(args, "--ignore-unmatch", a.GetIgnoreUnmatch())
	args = addFlagIfTrue(args, "--sparse", a.GetSparse())
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	return addPathspec(args, a.GetPathspec())
}

func argvShortlog(a *pb.GitShortlogArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--numbered", a.GetNumbered())
	args = addFlagIfTrue(args, "--summary", a.GetSummary())
	args = addFlagIfTrue(args, "--email", a.GetEmail())
	args = addEqVal(args, "--format", a.GetFormat())
	if c := a.GetAbbrev(); c > 0 {
		args = append(args, fmt.Sprintf("-c%d", c))
	}
	args = addEqVal(args, "--group", a.GetGroup())
	args = addFlagIfTrue(args, "--committer", a.GetCommitter())
	if r := a.GetRange(); r != "" {
		args = append(args, r)
	}
	args = append(args, a.GetRevisionRange()...)
	return addPathspec(args, a.GetPathspec())
}

func argvShow(a *pb.GitShowArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addDiffFormatting(args, a.GetFormatting())
	if p := a.GetPretty(); p != "" {
		args = append(args, "--pretty="+p)
	} else {
		args = addFlagIfTrue(args, "--pretty", a.GetPrettyFlag())
	}
	args = addFlagIfTrue(args, "--abbrev-commit", a.GetAbbrevCommit())
	args = addFlagIfTrue(args, "--no-abbrev-commit", a.GetNoAbbrevCommit())
	args = addFlagIfTrue(args, "--oneline", a.GetOneline())
	args = addEqVal(args, "--encoding", a.GetEncoding())
	if w := a.GetExpandTabsWidth(); w != nil && w.GetPresent() {
		args = append(args, fmt.Sprintf("--expand-tabs=%d", w.GetValue()))
	} else {
		args = addFlagIfTrue(args, "--expand-tabs", a.GetExpandTabs())
	}
	args = addFlagIfTrue(args, "--no-expand-tabs", a.GetNoExpandTabs())
	if r := a.GetNotesRef(); r != "" {
		args = append(args, "--notes="+r)
	} else {
		args = addFlagIfTrue(args, "--notes", a.GetNotes())
	}
	args = addFlagIfTrue(args, "--no-notes", a.GetNoNotes())
	if r := a.GetShowNotesRef(); r != "" {
		args = append(args, "--show-notes="+r)
	} else {
		args = addFlagIfTrue(args, "--show-notes", a.GetShowNotes())
	}
	args = addTriBool(args, "--standard-notes", "--no-standard-notes", a.GetStandardNotes())
	args = addFlagIfTrue(args, "--show-signature", a.GetShowSignature())
	args = addEqVal(args, "--diff-merges", a.GetDiffMerges())
	args = addFlagIfTrue(args, "--no-diff-merges", a.GetNoDiffMerges())
	args = addFlagIfTrue(args, "--combined-all-paths", a.GetCombinedAllPaths())
	args = addFlagIfTrue(args, "-t", a.GetDashT())
	return append(args, a.GetObjects()...)
}

func argvSparseCheckout(a *pb.GitSparseCheckoutArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch a.GetVerb() {
	case pb.GitSparseCheckoutArguments_VERB_LIST:
		args = append(args, "list")
	case pb.GitSparseCheckoutArguments_VERB_SET:
		args = append(args, "set")
	case pb.GitSparseCheckoutArguments_VERB_ADD:
		args = append(args, "add")
	case pb.GitSparseCheckoutArguments_VERB_REAPPLY:
		args = append(args, "reapply")
	case pb.GitSparseCheckoutArguments_VERB_DISABLE:
		args = append(args, "disable")
	case pb.GitSparseCheckoutArguments_VERB_INIT:
		args = append(args, "init")
	case pb.GitSparseCheckoutArguments_VERB_CHECK_RULES:
		args = append(args, "check-rules")
	}
	args = addTriBool(args, "--cone", "--no-cone", a.GetCone())
	args = addTriBool(args, "--sparse-index", "--no-sparse-index", a.GetSparseIndex())
	args = addFlagIfTrue(args, "--stdin", a.GetStdinInput())
	args = addFlagIfTrue(args, "--skip-checks", a.GetSkipChecks())
	return append(args, a.GetPatterns()...)
}

func argvStash(a *pb.GitStashArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch v := a.GetVerb().(type) {
	case *pb.GitStashArguments_Push:
		p := v.Push
		args = append(args, "push")
		args = addFlagIfTrue(args, "--patch", p.GetPatch())
		args = addFlagIfTrue(args, "--keep-index", p.GetKeepIndex())
		args = addFlagIfTrue(args, "--staged", p.GetStaged())
		args = addFlagIfTrue(args, "--include-untracked", p.GetIncludeUntracked())
		args = addFlagIfTrue(args, "--all", p.GetAll())
		args = addFlagIfTrue(args, "--quiet", p.GetQuiet())
		args = addSeparateArg(args, "-m", p.GetMessage())
		args = addPathspec(args, p.GetPathspec())
	case *pb.GitStashArguments_List:
		args = append(args, "list")
		args = append(args, v.List.GetLogOptions()...)
	case *pb.GitStashArguments_Show:
		args = append(args, "show")
		args = addFlagIfTrue(args, "--include-untracked", v.Show.GetIncludeUntracked())
		args = addFlagIfTrue(args, "--only-untracked", v.Show.GetOnlyUntracked())
		args = addFlagIfTrue(args, "--patch", v.Show.GetPatch())
		args = addFlagIfTrue(args, "--stat", v.Show.GetStat())
		args = append(args, v.Show.GetExtra()...)
		if s := v.Show.GetStash(); s != "" {
			args = append(args, s)
		}
	case *pb.GitStashArguments_Drop:
		args = append(args, "drop")
		args = addFlagIfTrue(args, "--quiet", v.Drop.GetQuiet())
		if s := v.Drop.GetStash(); s != "" {
			args = append(args, s)
		}
	case *pb.GitStashArguments_Pop:
		args = append(args, "pop")
		args = addFlagIfTrue(args, "--index", v.Pop.GetIndex())
		args = addFlagIfTrue(args, "--quiet", v.Pop.GetQuiet())
		if s := v.Pop.GetStash(); s != "" {
			args = append(args, s)
		}
	case *pb.GitStashArguments_Apply:
		args = append(args, "apply")
		args = addFlagIfTrue(args, "--index", v.Apply.GetIndex())
		args = addFlagIfTrue(args, "--quiet", v.Apply.GetQuiet())
		if s := v.Apply.GetStash(); s != "" {
			args = append(args, s)
		}
	case *pb.GitStashArguments_Branch:
		args = append(args, "branch", v.Branch.GetBranch())
		if s := v.Branch.GetStash(); s != "" {
			args = append(args, s)
		}
	case *pb.GitStashArguments_Clear:
		args = append(args, "clear")
	case *pb.GitStashArguments_Create:
		args = append(args, "create")
		args = append(args, v.Create.GetMessage()...)
	case *pb.GitStashArguments_Store:
		args = append(args, "store")
		args = addSeparateArg(args, "-m", v.Store.GetMessage())
		args = addFlagIfTrue(args, "--quiet", v.Store.GetQuiet())
		args = append(args, v.Store.GetCommit())
	}
	return args
}

func argvStatus(a *pb.GitStatusArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch a.GetFormat() {
	case pb.StatusFormat_STATUS_FORMAT_SHORT:
		args = append(args, "--short")
	case pb.StatusFormat_STATUS_FORMAT_LONG:
		args = append(args, "--long")
	case pb.StatusFormat_STATUS_FORMAT_PORCELAIN_V1:
		args = append(args, "--porcelain")
	case pb.StatusFormat_STATUS_FORMAT_PORCELAIN_V2:
		args = append(args, "--porcelain=v2")
	}
	args = addFlagIfTrue(args, "--branch", a.GetBranch())
	args = addFlagIfTrue(args, "--show-stash", a.GetShowStash())
	args = addTriBool(args, "--ahead-behind", "--no-ahead-behind", a.GetAheadBehind())
	if f, ok := untrackedFilesFlag(a.GetUntrackedFiles()); ok {
		args = append(args, f)
	}
	switch a.GetIgnored() {
	case pb.IgnoredMode_IGNORED_MODE_TRADITIONAL:
		args = append(args, "--ignored=traditional")
	case pb.IgnoredMode_IGNORED_MODE_MATCHING:
		args = append(args, "--ignored=matching")
	case pb.IgnoredMode_IGNORED_MODE_NO:
		args = append(args, "--ignored=no")
	}
	args = addFlagIfTrue(args, "-z", a.GetZ())
	if c := a.GetColumn(); c != "" {
		args = append(args, "--column="+c)
	}
	args = addFlagIfTrue(args, "--no-column", a.GetNoColumn())
	if f, ok := recurseSubFlag(a.GetRecurseSubmodules()); ok {
		args = append(args, f)
	}
	args = addFlagIfTrue(args, "--verbose", a.GetVerbose())
	return addPathspec(args, a.GetPathspec())
}

func argvSubmodule(a *pb.GitSubmoduleArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch a.GetVerb() {
	case pb.GitSubmoduleArguments_VERB_STATUS:
		args = append(args, "status")
	case pb.GitSubmoduleArguments_VERB_INIT:
		args = append(args, "init")
	case pb.GitSubmoduleArguments_VERB_DEINIT:
		args = append(args, "deinit")
	case pb.GitSubmoduleArguments_VERB_UPDATE:
		args = append(args, "update")
	case pb.GitSubmoduleArguments_VERB_SUMMARY:
		args = append(args, "summary")
	case pb.GitSubmoduleArguments_VERB_FOREACH:
		args = append(args, "foreach")
	case pb.GitSubmoduleArguments_VERB_SYNC:
		args = append(args, "sync")
	case pb.GitSubmoduleArguments_VERB_ADD:
		args = append(args, "add")
	case pb.GitSubmoduleArguments_VERB_ABSORBGITDIRS:
		args = append(args, "absorbgitdirs")
	case pb.GitSubmoduleArguments_VERB_SET_BRANCH:
		args = append(args, "set-branch")
	case pb.GitSubmoduleArguments_VERB_SET_URL:
		args = append(args, "set-url")
	}
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	args = addFlagIfTrue(args, "--cached", a.GetCached())
	args = addFlagIfTrue(args, "--recursive", a.GetRecursive())
	args = addFlagIfTrue(args, "--force", a.GetForce())
	args = addFlagIfTrue(args, "--init", a.GetInit())
	args = addFlagIfTrue(args, "--remote", a.GetRemote())
	args = addFlagIfTrue(args, "--merge", a.GetMerge())
	args = addFlagIfTrue(args, "--rebase", a.GetRebase())
	args = addFlagIfTrue(args, "--checkout", a.GetCheckout())
	args = addFlagIfTrue(args, "--no-fetch", a.GetNoFetch())
	args = addFlagIfTrue(args, "--progress", a.GetProgress())
	args = addSeparateArg(args, "--branch", a.GetBranch())
	args = addSeparateArg(args, "--reference", a.GetReference())
	if d := a.GetDepth(); d != "" {
		args = append(args, "--depth="+d)
	}
	args = addSeparateArg(args, "--name", a.GetName())
	if u := a.GetUrl(); u != "" {
		args = append(args, u)
	}
	args = append(args, a.GetPaths()...)
	return append(args, a.GetExtra()...)
}

func argvSwitch(a *pb.GitSwitchArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	args = addFlagIfTrue(args, "--quiet", a.GetQuiet())
	args = addFlagIfTrue(args, "--progress", a.GetProgress())
	args = addSeparateArg(args, "-c", a.GetCreate())
	args = addSeparateArg(args, "-C", a.GetForceCreate())
	args = addFlagIfTrue(args, "--detach", a.GetDetach())
	if f, ok := trackFlag(a.GetTrack()); ok {
		args = append(args, f)
	}
	args = addTriBool(args, "--guess", "--no-guess", a.GetGuess())
	args = addFlagIfTrue(args, "--discard-changes", a.GetDiscardChanges())
	args = addFlagIfTrue(args, "-m", a.GetMerge())
	args = addEqVal(args, "--conflict", a.GetConflictStyle())
	args = addSeparateArg(args, "--orphan", a.GetOrphan())
	args = addFlagIfTrue(args, "--ignore-other-worktrees", a.GetIgnoreOtherWorktrees())
	if f, ok := recurseSubFlag(a.GetRecurseSubmodules()); ok {
		args = append(args, f)
	}
	if b := a.GetBranch(); b != "" {
		args = append(args, b)
	}
	if sp := a.GetStartPoint(); sp != "" {
		args = append(args, sp)
	}
	return args
}

func argvTag(a *pb.GitTagArguments) []string {
	var args []string
	if a == nil {
		return args
	}
	switch a.GetAction() {
	case pb.GitTagArguments_ACTION_LIST:
		args = append(args, "--list")
	case pb.GitTagArguments_ACTION_DELETE:
		args = append(args, "--delete")
	case pb.GitTagArguments_ACTION_VERIFY:
		args = append(args, "--verify")
	}
	args = addFlagIfTrue(args, "-a", a.GetAnnotate())
	args = addFlagIfTrue(args, "-s", a.GetSign())
	args = addGpgSign(args, a.GetGpgSign())
	args = addFlagIfTrue(args, "--force", a.GetForce())
	args = addMessageSource(args, a.GetMessage())
	if m := a.GetCleanupMode(); m != "" {
		args = append(args, "--cleanup="+m)
	} else {
		args = addFlagIfTrue(args, "--cleanup", a.GetCleanup())
	}
	args = addFlagIfTrue(args, "--edit", a.GetEdit())
	args = addEqVal(args, "--sort", a.GetSort())
	args = addEqVal(args, "--format", a.GetFormat())
	args = addSeparateArg(args, "--points-at", a.GetPointsAt())
	args = addSeparateArg(args, "--contains", a.GetContains())
	args = addSeparateArg(args, "--no-contains", a.GetNoContains())
	args = addSeparateArg(args, "--merged", a.GetMerged())
	args = addSeparateArg(args, "--no-merged", a.GetNoMerged())
	if f, ok := colorWhenFlag(a.GetColor()); ok {
		args = append(args, f)
	}
	args = addEqVal(args, "--column", a.GetColumn())
	args = addFlagIfTrue(args, "--ignore-case", a.GetIgnoreCase())
	if r := a.GetCreateReflog(); r != "" {
		args = append(args, r)
	}
	args = append(args, a.GetPatterns()...)
	if t := a.GetTagName(); t != "" {
		args = append(args, t)
	}
	if o := a.GetObject(); o != "" {
		args = append(args, o)
	}
	return append(args, a.GetTagNames()...)
}

func argvWorktree(a *pb.GitWorktreeArguments) []string {
	var args []string
	if a == nil {
		return []string{"list"}
	}
	switch a.GetVerb() {
	case pb.GitWorktreeArguments_VERB_LIST, pb.GitWorktreeArguments_VERB_UNSPECIFIED:
		args = append(args, "list")
	case pb.GitWorktreeArguments_VERB_ADD:
		args = append(args, "add")
	case pb.GitWorktreeArguments_VERB_REMOVE:
		args = append(args, "remove")
	case pb.GitWorktreeArguments_VERB_MOVE:
		args = append(args, "move")
	case pb.GitWorktreeArguments_VERB_LOCK:
		args = append(args, "lock")
	case pb.GitWorktreeArguments_VERB_UNLOCK:
		args = append(args, "unlock")
	case pb.GitWorktreeArguments_VERB_PRUNE:
		args = append(args, "prune")
	case pb.GitWorktreeArguments_VERB_REPAIR:
		args = append(args, "repair")
	}
	args = addFlagIfTrue(args, "--force", a.GetForce())
	args = addFlagIfTrue(args, "--detach", a.GetDetach())
	args = addTriBool(args, "--checkout", "--no-checkout", a.GetCheckout())
	args = addFlagIfTrue(args, "--lock", a.GetLock())
	args = addEqVal(args, "--reason", a.GetLockReason())
	args = addSeparateArg(args, "-b", a.GetBranch())
	args = addSeparateArg(args, "-B", a.GetBranchForce())
	if g := a.GetGuessRemote(); g != "" {
		args = append(args, g)
	}
	args = addFlagIfTrue(args, "--track", a.GetTrack())
	args = addFlagIfTrue(args, "--porcelain", a.GetPorcelain())
	args = addFlagIfTrue(args, "-z", a.GetZ())
	args = addFlagIfTrue(args, "--verbose", a.GetVerbose())
	args = addEqVal(args, "--expire", a.GetExpire())
	args = addFlagIfTrue(args, "--dry-run", a.GetDryRun())
	args = addFlagIfTrue(args, "--relative-paths", a.GetRelativePaths())
	if p := a.GetPath(); p != "" {
		args = append(args, p)
	}
	if c := a.GetCommitIsh(); c != "" {
		args = append(args, c)
	}
	if np := a.GetNewPath(); np != "" {
		args = append(args, np)
	}
	return append(args, a.GetPaths()...)
}
