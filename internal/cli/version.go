package cli

import (
	"runtime/debug"
)

// resolveVersion decides what `--version` reports:
//   - a release build stamps it with -ldflags (GoReleaser, `make build`)
//   - `go install …@latest` builds carry the module version (a pseudo-version with the commit)
//   - a plain `go build` inside a git checkout reports dev+<commit>
//   - anything else is just "dev"
//
// It never invents a release number.
func resolveVersion(stamped string) string {
	bi, ok := debug.ReadBuildInfo()
	return versionFrom(stamped, bi, ok)
}

func versionFrom(stamped string, bi *debug.BuildInfo, ok bool) string {
	if stamped != "" && stamped != "dev" {
		return stamped
	}
	if !ok || bi == nil {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	rev, dirty := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) >= 7 {
		v := "dev+" + rev[:7]
		if dirty {
			v += "-dirty"
		}
		return v
	}
	return "dev"
}
