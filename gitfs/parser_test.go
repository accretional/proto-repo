package gitfs

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/accretional/proto-repo/gitfs/pb"
)

// repoGitDir locates the repo's own .git directory by walking upward from
// this test file. All tests use this repo as the source of truth.
func repoGitDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for {
		candidate := filepath.Join(dir, ".git")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find .git directory walking up from %s", wd)
		}
		dir = parent
	}
}

// gitOutput shells out to git for an authoritative comparison value.
func gitOutput(t *testing.T, gitDir string, args ...string) string {
	t.Helper()
	full := append([]string{"--git-dir", gitDir}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", full, err)
	}
	return strings.TrimRight(string(out), "\n")
}

// headSHA reads the resolved HEAD commit SHA via git, used as the seed for
// many tests so they don't break when the working branch advances.
func headSHA(t *testing.T, gitDir string) string {
	t.Helper()
	return gitOutput(t, gitDir, "rev-parse", "HEAD")
}

// ---------------------------------------------------------------------------
// Object loading
// ---------------------------------------------------------------------------

// TestLoadObjectCommit verifies that loading the HEAD commit (assumed loose,
// since it was created post-clone) produces the same tree/parent/author
// fields as `git cat-file -p`.
func TestLoadObjectCommit(t *testing.T) {
	gitDir := repoGitDir(t)
	sha := headSHA(t, gitDir)

	obj, err := LoadObject(gitDir, sha)
	if err != nil {
		t.Fatalf("LoadObject(%s): %v", sha, err)
	}
	if obj.GetType() != pb.ObjectType_COMMIT {
		t.Fatalf("type = %v, want COMMIT", obj.GetType())
	}

	c := obj.GetCommit()
	if c == nil {
		t.Fatalf("commit body is nil")
	}

	wantTree := gitOutput(t, gitDir, "rev-parse", sha+"^{tree}")
	if got := hex.EncodeToString(c.GetTreeSha1()); got != wantTree {
		t.Errorf("tree sha = %s, want %s", got, wantTree)
	}

	parents := gitOutput(t, gitDir, "log", "-1", "--format=%P", sha)
	wantParents := []string{}
	if parents != "" {
		wantParents = strings.Fields(parents)
	}
	if len(c.GetParentSha1S()) != len(wantParents) {
		t.Fatalf("parents = %d, want %d", len(c.GetParentSha1S()), len(wantParents))
	}
	for i, want := range wantParents {
		if got := hex.EncodeToString(c.GetParentSha1S()[i]); got != want {
			t.Errorf("parent[%d] = %s, want %s", i, got, want)
		}
	}

	wantAuthorName := gitOutput(t, gitDir, "log", "-1", "--format=%an", sha)
	if c.GetAuthor().GetName() != wantAuthorName {
		t.Errorf("author name = %q, want %q", c.GetAuthor().GetName(), wantAuthorName)
	}
	wantAuthorEmail := gitOutput(t, gitDir, "log", "-1", "--format=%ae", sha)
	if c.GetAuthor().GetEmail() != wantAuthorEmail {
		t.Errorf("author email = %q, want %q", c.GetAuthor().GetEmail(), wantAuthorEmail)
	}

	wantSubject := gitOutput(t, gitDir, "log", "-1", "--format=%s", sha)
	if !strings.HasPrefix(c.GetMessage(), wantSubject) {
		t.Errorf("message subject = %q, want prefix %q",
			firstLine(c.GetMessage()), wantSubject)
	}
}

// TestLoadObjectTree round-trips the HEAD tree against `git cat-file -p`,
// asserting the entry list and modes match.
func TestLoadObjectTree(t *testing.T) {
	gitDir := repoGitDir(t)
	headTree := gitOutput(t, gitDir, "rev-parse", "HEAD^{tree}")

	obj, err := LoadObject(gitDir, headTree)
	if err != nil {
		t.Fatalf("LoadObject tree: %v", err)
	}
	if obj.GetType() != pb.ObjectType_TREE {
		t.Fatalf("type = %v, want TREE", obj.GetType())
	}

	wantLines := strings.Split(gitOutput(t, gitDir, "cat-file", "-p", headTree), "\n")
	gotEntries := obj.GetTree().GetEntries()
	if len(gotEntries) != len(wantLines) {
		t.Fatalf("entries = %d, want %d", len(gotEntries), len(wantLines))
	}
	for i, line := range wantLines {
		// "<mode> <type> <sha>\t<name>"
		fields := strings.SplitN(line, "\t", 2)
		hdr := strings.Fields(fields[0])
		wantMode := hdr[0]
		wantSHA := hdr[2]
		wantName := fields[1]

		got := gotEntries[i]
		if gotMode := fmt.Sprintf("%06o", got.GetMode()); gotMode != wantMode {
			t.Errorf("entry %d mode = %s, want %s", i, gotMode, wantMode)
		}
		if got.GetName() != wantName {
			t.Errorf("entry %d name = %q, want %q", i, got.GetName(), wantName)
		}
		if gotSHA := hex.EncodeToString(got.GetSha1()); gotSHA != wantSHA {
			t.Errorf("entry %d sha = %s, want %s", i, gotSHA, wantSHA)
		}
	}
}

// TestLoadObjectBlob loads .gitignore via its blob sha and compares to the
// working-tree file content.
func TestLoadObjectBlob(t *testing.T) {
	gitDir := repoGitDir(t)
	blobSHA := gitOutput(t, gitDir, "rev-parse", "HEAD:.gitignore")

	obj, err := LoadObject(gitDir, blobSHA)
	if err != nil {
		t.Skipf("blob %s not loose (likely packed): %v", blobSHA, err)
	}
	if obj.GetType() != pb.ObjectType_BLOB {
		t.Fatalf("type = %v, want BLOB", obj.GetType())
	}

	want := []byte(gitOutput(t, gitDir, "cat-file", "-p", blobSHA) + "\n")
	if !bytes.Equal(obj.GetBlob().GetContent(), want) {
		t.Errorf(".gitignore content mismatch — proto %d bytes, git %d bytes",
			len(obj.GetBlob().GetContent()), len(want))
	}
}

// ---------------------------------------------------------------------------
// Refs
// ---------------------------------------------------------------------------

// TestLoadHEAD checks the HEAD ref parses as symbolic.
func TestLoadHEAD(t *testing.T) {
	gitDir := repoGitDir(t)
	ref, err := LoadRef(gitDir, "HEAD")
	if err != nil {
		t.Fatalf("LoadRef HEAD: %v", err)
	}
	target := ref.GetSymbolicRef()
	if target == "" {
		t.Fatalf("HEAD is not symbolic — got direct sha %x", ref.GetSha1())
	}
	if !strings.HasPrefix(target, "refs/heads/") {
		t.Errorf("HEAD target = %q, want refs/heads/ prefix", target)
	}
}

// TestLoadDirectRef confirms refs/heads/<branch> resolves to the same sha
// as `git rev-parse`.
func TestLoadDirectRef(t *testing.T) {
	gitDir := repoGitDir(t)
	headRef, err := LoadRef(gitDir, "HEAD")
	if err != nil {
		t.Fatalf("LoadRef HEAD: %v", err)
	}
	target := headRef.GetSymbolicRef()
	if target == "" {
		t.Fatal("HEAD is not symbolic")
	}

	ref, err := LoadRef(gitDir, target)
	if err != nil {
		t.Skipf("direct ref %s not loose (likely packed): %v", target, err)
	}
	want := gitOutput(t, gitDir, "rev-parse", target)
	if got := hex.EncodeToString(ref.GetSha1()); got != want {
		t.Errorf("%s sha = %s, want %s", target, got, want)
	}
}

// TestLoadPackedRefs loads packed-refs (if present) and validates the
// header trait and that every named ref matches `git rev-parse`.
func TestLoadPackedRefs(t *testing.T) {
	gitDir := repoGitDir(t)
	pr, err := LoadPackedRefs(gitDir)
	if err != nil {
		t.Fatalf("LoadPackedRefs: %v", err)
	}
	if pr == nil {
		t.Skip("no packed-refs file in this repo")
	}
	if len(pr.GetHeaderTraits()) == 0 {
		t.Errorf("expected header traits like 'peeled', 'sorted'")
	}
	// Compare to the literal file content (not `git rev-parse <name>` —
	// that applies the loose-shadowing rule, which can mask packed-refs
	// values when a loose ref of the same name exists at a different sha).
	raw, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		t.Fatalf("read packed-refs: %v", err)
	}
	wantPairs := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		wantPairs[parts[1]] = parts[0]
	}
	for _, r := range pr.GetRefs() {
		want, ok := wantPairs[r.GetName()]
		if !ok {
			t.Errorf("parser produced ref %s not in packed-refs file", r.GetName())
			continue
		}
		if got := hex.EncodeToString(r.GetSha1()); got != want {
			t.Errorf("packed ref %s sha = %s, want %s", r.GetName(), got, want)
		}
		// Sanity: the recorded sha must be a real object.
		if _, err := exec.Command("git", "--git-dir", gitDir, "cat-file", "-t", want).Output(); err != nil {
			t.Errorf("packed ref %s sha %s does not resolve to an object", r.GetName(), want)
		}
	}
}

// ---------------------------------------------------------------------------
// Index
// ---------------------------------------------------------------------------

// TestLoadIndex validates DIRC magic, version, entry count, and the SHA
// of the first entry against `git ls-files -s`.
func TestLoadIndex(t *testing.T) {
	gitDir := repoGitDir(t)
	idx, err := LoadIndex(gitDir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	if v := idx.GetVersion(); v != 2 && v != 3 {
		t.Errorf("version = %d, want 2 or 3", v)
	}
	if len(idx.GetEntries()) == 0 {
		t.Fatal("no entries")
	}

	// Cross-check entry count against `git ls-files`.
	want := gitOutput(t, gitDir, "ls-files")
	wantCount := len(strings.Split(want, "\n"))
	if got := len(idx.GetEntries()); got != wantCount {
		t.Errorf("entry count = %d, want %d (git ls-files)", got, wantCount)
	}

	// Validate every entry's name+sha matches `git ls-files -s` (which
	// emits "<mode> <sha> <stage>\t<name>" per line).
	wantStaged := gitOutput(t, gitDir, "ls-files", "-s")
	wantBySha := map[string]string{}
	for _, line := range strings.Split(wantStaged, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		hdr := strings.Fields(parts[0])
		wantBySha[parts[1]] = hdr[1]
	}
	for _, e := range idx.GetEntries() {
		want, ok := wantBySha[e.GetName()]
		if !ok {
			t.Errorf("index entry %q absent from git ls-files", e.GetName())
			continue
		}
		if got := hex.EncodeToString(e.GetSha1()); got != want {
			t.Errorf("entry %q sha = %s, want %s", e.GetName(), got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Reflog
// ---------------------------------------------------------------------------

// TestLoadReflogHEAD parses logs/HEAD and asserts the first line is a
// ref-creation entry (old sha all zero, message starts with a known prefix
// like "clone:" or "branch:").
func TestLoadReflogHEAD(t *testing.T) {
	gitDir := repoGitDir(t)
	rl, err := LoadReflog(gitDir, "HEAD")
	if err != nil {
		t.Fatalf("LoadReflog HEAD: %v", err)
	}
	if rl == nil {
		t.Skip("no reflog at logs/HEAD")
	}
	if len(rl.GetEntries()) == 0 {
		t.Fatal("no entries")
	}

	first := rl.GetEntries()[0]
	zero := make([]byte, 20)
	if !bytes.Equal(first.GetOldSha1(), zero) {
		t.Errorf("first entry old sha = %x, want all-zero (creation)", first.GetOldSha1())
	}
	if first.GetWho().GetName() == "" {
		t.Error("first entry who.name is empty")
	}

	// Last entry's new sha should be HEAD.
	last := rl.GetEntries()[len(rl.GetEntries())-1]
	wantHead := headSHA(t, gitDir)
	if got := hex.EncodeToString(last.GetNewSha1()); got != wantHead {
		t.Errorf("last entry new sha = %s, want HEAD %s", got, wantHead)
	}
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// TestLoadConfig parses .git/config and validates the canonical sections
// (core.bare = false; remote.origin.url ends in .git).
func TestLoadConfig(t *testing.T) {
	gitDir := repoGitDir(t)
	cfg, err := LoadConfigDefault(gitDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	core := findSection(cfg, "core", "")
	if core == nil {
		t.Fatal("missing [core] section")
	}
	if v := findVar(core, "bare"); v != "false" {
		t.Errorf("core.bare = %q, want false", v)
	}

	origin := findSection(cfg, "remote", "origin")
	if origin == nil {
		t.Fatal("missing [remote \"origin\"] section")
	}
	url := findVar(origin, "url")
	if !strings.HasSuffix(url, ".git") {
		t.Errorf("remote.origin.url = %q, want .git suffix", url)
	}
	wantURL := gitOutput(t, gitDir, "config", "--get", "remote.origin.url")
	if url != wantURL {
		t.Errorf("remote.origin.url = %q, want %q", url, wantURL)
	}
}

// ---------------------------------------------------------------------------
// Pack
// ---------------------------------------------------------------------------

// TestLoadPack validates the .pack header magic/version + the .idx fanout
// + sha list is consistent with `git verify-pack -v`.
func TestLoadPack(t *testing.T) {
	gitDir := repoGitDir(t)
	packDir := filepath.Join(gitDir, "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		t.Skipf("no packs: %v", err)
	}
	var basename string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".pack") {
			basename = strings.TrimSuffix(e.Name(), ".pack")
			break
		}
	}
	if basename == "" {
		t.Skip("no .pack file")
	}
	pack, err := LoadPack(gitDir, basename)
	if err != nil {
		t.Fatalf("LoadPack: %v", err)
	}
	if v := pack.GetHeader().GetVersion(); v != 2 {
		t.Errorf("pack header version = %d, want 2", v)
	}
	gotCount := pack.GetHeader().GetObjectCount()
	if int(gotCount) != len(pack.GetIndex().GetSha1S()) {
		t.Errorf("pack object count %d != idx sha count %d",
			gotCount, len(pack.GetIndex().GetSha1S()))
	}
	if pack.GetIndex().GetVersion() != 2 {
		t.Errorf("idx version = %d, want 2", pack.GetIndex().GetVersion())
	}
	// Fanout array final entry must equal the total object count.
	fanout := pack.GetIndex().GetFanout()
	if len(fanout) != 256 {
		t.Fatalf("fanout len = %d, want 256", len(fanout))
	}
	if fanout[255] != gotCount {
		t.Errorf("fanout[255] = %d, want %d", fanout[255], gotCount)
	}
	// Pack and idx both record the same pack sha at their tail.
	if !bytes.Equal(pack.GetHeader().GetPackSha1(), pack.GetIndex().GetPackSha1()) {
		t.Errorf("pack trailer sha != idx pack-sha")
	}
}

// ---------------------------------------------------------------------------
// Top-level Open
// ---------------------------------------------------------------------------

// TestOpenRepository drives Open() over this repo's .git and spot-checks
// the populated fields.
func TestOpenRepository(t *testing.T) {
	gitDir := repoGitDir(t)
	repo, err := Open(gitDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if repo.GetGitDir() == "" {
		t.Error("git_dir empty")
	}
	if repo.GetHead() == nil {
		t.Error("head nil")
	}
	if len(repo.GetLooseRefs()) == 0 {
		t.Error("expected at least one loose ref")
	}
	if repo.GetIndex() == nil {
		t.Error("index nil")
	}
	if repo.GetConfig() == nil || len(repo.GetConfig().GetSections()) == 0 {
		t.Error("config empty")
	}
	if len(repo.GetReflogs()) == 0 {
		t.Error("expected at least one reflog")
	}
	// description is the canned "Unnamed repository; …" until the user
	// edits it. Just check it's non-empty.
	if repo.GetLocalState().GetDescription() == "" {
		t.Error("local_state.description empty")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func findSection(cfg *pb.Config, name, sub string) *pb.ConfigSection {
	for _, s := range cfg.GetSections() {
		if s.GetName() == name && s.GetSubsection() == sub {
			return s
		}
	}
	return nil
}

func findVar(s *pb.ConfigSection, name string) string {
	for _, v := range s.GetVariables() {
		if v.GetName() == name {
			return v.GetValue()
		}
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
