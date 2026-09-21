package cli

import (
	"runtime/debug"
	"testing"
)

func TestVersionFrom(t *testing.T) {
	git := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "e516be597cb1abcdef"}, {Key: "vcs.modified", Value: "false"}}}
	dirty := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "e516be597cb1abcdef"}, {Key: "vcs.modified", Value: "true"}}}
	installed := &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260921141441-e516be597cb1"}}

	cases := []struct {
		name    string
		stamped string
		bi      *debug.BuildInfo
		ok      bool
		want    string
	}{
		{"release build keeps its stamp", "5.0.0", git, true, "5.0.0"},
		{"stamp beats build info", "v9.9.9", installed, true, "v9.9.9"},
		{"go install reports the module version", "dev", installed, true, "v0.0.0-20260921141441-e516be597cb1"},
		{"git checkout", "dev", git, true, "dev+e516be5"},
		{"dirty checkout", "dev", dirty, true, "dev+e516be5-dirty"},
		{"no build info", "dev", nil, false, "dev"},
		{"empty stamp behaves like dev", "", git, true, "dev+e516be5"},
		{"no vcs info", "dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, "dev"},
	}
	for _, c := range cases {
		if got := versionFrom(c.stamped, c.bi, c.ok); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
