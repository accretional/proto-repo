package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGitLink(t *testing.T) {
	cases := []struct {
		in  string
		out string
		ok  bool
	}{
		{"git-fast-export[1]", "fast-export", true},
		{"git-add[1]", "add", true},
		{"git-version[1]", "version", true},
		{"gitnamespaces[7]", "", false},
		{"gitattributes[5]", "", false},
		{"git-[1]", "", false},
		{"something", "", false},
		{"git-log", "", false},
	}
	for _, c := range cases {
		got, ok := parseGitLink(c.in)
		if ok != c.ok || got != c.out {
			t.Errorf("%q: got (%q,%t), want (%q,%t)", c.in, got, ok, c.out, c.ok)
		}
	}
}

func TestCamelCase(t *testing.T) {
	cases := map[string]string{
		"fast-export":     "FastExport",
		"add":             "Add",
		"sparse-checkout": "SparseCheckout",
		"sparse_checkout": "SparseCheckout",
		"cherry-pick":     "CherryPick",
		"range-diff":      "RangeDiff",
	}
	for in, want := range cases {
		if got := camelCase(in); got != want {
			t.Errorf("camelCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseCSVFiltersAndDedupes(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "sample.csv")
	body := strings.Join([]string{
		"url,text",
		"https://git-scm.com/docs/git-version,git-version[1]",
		"https://git-scm.com/docs/git-help,git-help[1]",
		"https://git-scm.com/docs/git-config,git-config[1]",
		"https://git-scm.com/docs/git-config,git-config[1]", // duplicate
		"https://git-scm.com/docs/gitnamespaces,gitnamespaces[7]",
		"https://git-scm.com/docs/gitattributes,gitattributes[5]",
	}, "\n") + "\n"
	if err := os.WriteFile(csvPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds, err := parseCSV(csvPath)
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	// Expected: 3 unique [1] entries (version, help, config), alphabetically sorted.
	want := []string{"config", "help", "version"}
	if len(cmds) != len(want) {
		t.Fatalf("got %d commands, want %d: %+v", len(cmds), len(want), cmds)
	}
	for i, w := range want {
		if cmds[i].Command != w {
			t.Errorf("cmds[%d] = %q, want %q", i, cmds[i].Command, w)
		}
	}
}

func TestWriteProtoShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plumbing.proto")
	sec := section{csv: "plumbing_commands.csv", pkg: "plumbing", service: "Plumbing"}
	cmds := []cmdEntry{
		{URL: "https://git-scm.com/docs/git-version", Command: "version"},
		{URL: "https://git-scm.com/docs/git-help", Command: "help"},
	}
	if err := writeProto(path, sec, cmds); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	mustContain := []string{
		`syntax = "proto3";`,
		"package plumbing;",
		`import "repo.proto";`,
		`import "subcommands.proto";`,
		`option go_package = "github.com/accretional/proto-repo/gluon/plumbing/pb";`,
		"service Plumbing {",
		"rpc Version(subcommands.SubCommandReq) returns (repo.RepoMsg);",
		"rpc Help(subcommands.SubCommandReq) returns (repo.RepoMsg);",
		"git-version — https://git-scm.com/docs/git-version",
	}
	for _, m := range mustContain {
		if !strings.Contains(got, m) {
			t.Errorf("proto missing %q\n--- got ---\n%s", m, got)
		}
	}
}
