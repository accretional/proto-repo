// Command stubgen generates per-section gRPC service .proto files from
// the docs/git-links/*.csv inventory, one file per package in the
// "TODO: per-category gRPC services" table in README.md.
//
// It's the shape-preview gluon's codegen pipeline would have produced
// once cross-package proto imports were supported: every RPC takes
// subcommands.SubCommandReq and returns repo.RepoMsg, matching the
// thin-runner pattern already in the subcommands/ package. gluon v1's
// codegen emits self-contained protos (types inlined, no imports), so
// driving this off gluon directly would duplicate those types per
// package — instead we mimic gluon's naming conventions (snake-case
// filenames, CamelCase RPC names) and emit the right import directives
// by hand.
//
// Usage:
//
//	stubgen --docs-dir docs/git-links --out gluon
//
// Run without args from the repo root to regenerate everything.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// section maps a CSV basename to the package + service name it becomes.
// The table lines up with README.md's per-category gRPC services list.
type section struct {
	csv     string // basename under docs-dir
	pkg     string // go package + proto package
	service string // gRPC service name
}

var sections = []section{
	{"_ancillary_commands.csv", "ancillary", "Ancillary"},
	{"_interacting_with_others.csv", "interaction", "Interaction"},
	{"_internal_helper_commands.csv", "helper", "Helper"},
	{"_interrogation_commands.csv", "interrogation", "Interrogation"},
	{"_manipulation_commands.csv", "manipulation", "Manipulation"},
	{"_other.csv", "misc", "Misc"},
	{"_reset_restore_and_revert.csv", "revert", "Revert"},
	{"_syncing_repositories.csv", "syncing", "Syncing"},
	{"plumbing_commands.csv", "plumbing", "Plumbing"},
}

// cmdEntry is one deduped git-<cmd>[1] row from a CSV.
type cmdEntry struct {
	URL     string // canonical docs URL
	Command string // the git-* slug with "git-" stripped (e.g., "fast-export")
}

func main() {
	docsDir := flag.String("docs-dir", "docs/git-links", "directory of CSVs produced by docs/pull")
	outDir := flag.String("out", "gluon", "destination directory for generated packages")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", *outDir, err)
	}

	for _, sec := range sections {
		cmds, err := parseCSV(filepath.Join(*docsDir, sec.csv))
		if err != nil {
			log.Fatalf("%s: %v", sec.csv, err)
		}
		if len(cmds) == 0 {
			fmt.Printf("skip %s — no [1] entries\n", sec.csv)
			continue
		}
		pkgDir := filepath.Join(*outDir, sec.pkg)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			log.Fatalf("mkdir %s: %v", pkgDir, err)
		}
		protoPath := filepath.Join(pkgDir, sec.pkg+".proto")
		if err := writeProto(protoPath, sec, cmds); err != nil {
			log.Fatalf("write %s: %v", protoPath, err)
		}
		fmt.Printf("wrote %s (%d rpcs)\n", protoPath, len(cmds))
	}
}

// parseCSV reads a docs/git-links CSV, filters to `git-*[1]` rows, strips
// the `git-` prefix, and returns the deduped command list sorted by name.
// Rows with [5]/[7] suffixes (file-format / conceptual pages) are dropped —
// they're not commands and have no CLI surface to wrap.
func parseCSV(path string) ([]cmdEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // CSVs have a header we skip by hand
	seen := make(map[string]cmdEntry)
	header := true
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header {
			header = false
			continue
		}
		if len(rec) < 2 {
			continue
		}
		url, text := strings.TrimSpace(rec[0]), strings.TrimSpace(rec[1])
		cmd, ok := parseGitLink(text)
		if !ok {
			continue
		}
		if _, dup := seen[cmd]; !dup {
			seen[cmd] = cmdEntry{URL: url, Command: cmd}
		}
	}

	out := make([]cmdEntry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Command < out[j].Command })
	return out, nil
}

// parseGitLink accepts a docs anchor label like "git-fast-export[1]" and
// returns the bare "fast-export" slug. Returns false for non-[1] labels
// (e.g., "gitattributes[5]") or ones that don't start with "git-".
func parseGitLink(text string) (string, bool) {
	if !strings.HasSuffix(text, "[1]") {
		return "", false
	}
	text = strings.TrimSuffix(text, "[1]")
	if !strings.HasPrefix(text, "git-") {
		return "", false
	}
	text = strings.TrimPrefix(text, "git-")
	if text == "" {
		return "", false
	}
	return text, true
}

// camelCase converts a hyphenated slug like "fast-export" to "FastExport".
// Underscores are treated the same as hyphens, so "sparse-checkout" and
// "sparse_checkout" both produce "SparseCheckout".
func camelCase(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		if r == '-' || r == '_' {
			upper = true
			continue
		}
		if upper {
			b.WriteRune(toUpper(r))
			upper = false
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

// writeProto renders the .proto file for one section. Every RPC follows the
// SubCommandReq → RepoMsg shape so servers can delegate to the shared runner
// in subcommands/ without per-command argv logic.
func writeProto(path string, sec section, cmds []cmdEntry) error {
	var b strings.Builder
	fmt.Fprintln(&b, `syntax = "proto3";`)
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "package %s;\n\n", sec.pkg)
	fmt.Fprintln(&b, `import "repo.proto";`)
	fmt.Fprintln(&b, `import "subcommands.proto";`)
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "option go_package = \"github.com/accretional/proto-repo/gluon/%s/pb\";\n\n", sec.pkg)
	fmt.Fprintf(&b, "// %s wraps the %q section of https://git-scm.com/docs/git.\n", sec.service, humanSection(sec.pkg))
	fmt.Fprintf(&b, "// Generated from docs/git-links/%s; do not edit by hand. Re-run\n", sec.csv)
	fmt.Fprintln(&b, "// `go run ./cmd/stubgen` to refresh.")
	fmt.Fprintf(&b, "service %s {\n", sec.service)
	for _, c := range cmds {
		fmt.Fprintf(&b, "  // git-%s — %s\n", c.Command, c.URL)
		fmt.Fprintf(&b, "  rpc %s(subcommands.SubCommandReq) returns (repo.RepoMsg);\n", camelCase(c.Command))
	}
	fmt.Fprintln(&b, "}")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// humanSection returns a short description for a section package. Used only
// in the generated proto's service comment; nothing reads it programmatically.
func humanSection(pkg string) string {
	switch pkg {
	case "ancillary":
		return "Ancillary Commands"
	case "interaction":
		return "Interacting with Others"
	case "helper":
		return "Internal Helper Commands"
	case "interrogation":
		return "Interrogation Commands"
	case "manipulation":
		return "Manipulation Commands"
	case "misc":
		return "Other"
	case "revert":
		return "Reset, Restore and Revert"
	case "syncing":
		return "Syncing Repositories"
	case "plumbing":
		return "Plumbing Commands"
	}
	return pkg
}
