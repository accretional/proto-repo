package gitexec

import "testing"

func TestParseGitVersion(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    GitVersion
		wantErr bool
	}{
		{"apple git", "git version 2.39.5 (Apple Git-154)\n", GitVersion{2, 39, 5}, false},
		{"bare triple", "git version 2.45.0\n", GitVersion{2, 45, 0}, false},
		{"major.minor only", "git version 2.20", GitVersion{2, 20, 0}, false},
		{"rc suffix on patch", "git version 2.41.0-rc1", GitVersion{2, 41, 0}, false},
		{"linux-distro suffix", "git version 2.34.1.1-ubuntu", GitVersion{2, 34, 1}, false},
		{"wrong prefix", "hg version 5.0", GitVersion{}, true},
		{"not enough fields", "git version 2", GitVersion{}, true},
		{"non-numeric major", "git version x.y.z", GitVersion{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseGitVersion(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestGitVersionLess(t *testing.T) {
	cases := []struct {
		a, b GitVersion
		want bool
	}{
		{GitVersion{2, 20, 0}, GitVersion{2, 39, 0}, true},
		{GitVersion{2, 39, 0}, GitVersion{2, 20, 0}, false},
		{GitVersion{2, 39, 0}, GitVersion{2, 39, 0}, false},
		{GitVersion{1, 99, 99}, GitVersion{2, 0, 0}, true},
		{GitVersion{2, 39, 4}, GitVersion{2, 39, 5}, true},
	}
	for _, tc := range cases {
		if got := tc.a.Less(tc.b); got != tc.want {
			t.Errorf("%s.Less(%s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestProbeGitOnHost confirms the probe succeeds against whatever git the
// test host has. If this fails, no other test that shells out to git would
// pass anyway — this is an early, clear signal.
func TestProbeGitOnHost(t *testing.T) {
	v, err := ProbeGit()
	if err != nil {
		t.Fatalf("ProbeGit: %v", err)
	}
	if v.Less(MinGitVersion) {
		t.Fatalf("host git %s < MinGitVersion %s", v, MinGitVersion)
	}
}

func TestNameFromURI(t *testing.T) {
	cases := map[string]string{
		"https://github.com/foo/bar.git": "bar",
		"https://github.com/foo/bar/":    "bar",
		"file:///tmp/baz":                "baz",
		"file:///tmp/qux.git":            "qux",
		"git@github.com:foo/bar.git":     "bar",
	}
	for in, want := range cases {
		if got := NameFromURI(in); got != want {
			t.Errorf("NameFromURI(%q) = %q, want %q", in, got, want)
		}
	}
}
