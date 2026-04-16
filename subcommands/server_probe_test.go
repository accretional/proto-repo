package subcommands

import "testing"

func TestParseGitVersion(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    gitVersion
		wantErr bool
	}{
		{"apple git", "git version 2.39.5 (Apple Git-154)\n", gitVersion{2, 39, 5}, false},
		{"bare triple", "git version 2.45.0\n", gitVersion{2, 45, 0}, false},
		{"major.minor only", "git version 2.20", gitVersion{2, 20, 0}, false},
		{"rc suffix on patch", "git version 2.41.0-rc1", gitVersion{2, 41, 0}, false},
		{"linux-distro suffix", "git version 2.34.1.1-ubuntu", gitVersion{2, 34, 1}, false},
		{"wrong prefix", "hg version 5.0", gitVersion{}, true},
		{"not enough fields", "git version 2", gitVersion{}, true},
		{"non-numeric major", "git version x.y.z", gitVersion{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseGitVersion(tc.in)
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
		a, b gitVersion
		want bool
	}{
		{gitVersion{2, 20, 0}, gitVersion{2, 39, 0}, true},
		{gitVersion{2, 39, 0}, gitVersion{2, 20, 0}, false},
		{gitVersion{2, 39, 0}, gitVersion{2, 39, 0}, false},
		{gitVersion{1, 99, 99}, gitVersion{2, 0, 0}, true},
		{gitVersion{2, 39, 4}, gitVersion{2, 39, 5}, true},
	}
	for _, tc := range cases {
		if got := tc.a.less(tc.b); got != tc.want {
			t.Errorf("%s.less(%s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestProbeGitOnHost confirms the probe succeeds against whatever git the
// test host has. If this fails, no other test in the package would pass
// anyway — Execute RPCs shell out to the same binary.
func TestProbeGitOnHost(t *testing.T) {
	v, err := probeGit()
	if err != nil {
		t.Fatalf("probeGit: %v", err)
	}
	if v.less(MinGitVersion) {
		t.Fatalf("host git %s < MinGitVersion %s", v, MinGitVersion)
	}
}
