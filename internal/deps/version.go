package deps

import (
	"fmt"
	"strconv"
	"strings"
)

// semver holds a parsed major.minor.patch version.
type semver struct {
	Major, Minor, Patch int
	Pre                 string // pre-release tag ("beta.1"); empty for a normal release
}

// parseSemver accepts "1", "1.2", "1.2.3", "1.2.3-beta.1" and "1.2.3+build5".
// Build metadata is ignored; a pre-release sorts below the same release (semver rule).
func parseSemver(v string) (semver, error) {
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], v[i+1:]
	}
	sv, err := parseCore(v)
	sv.Pre = pre
	return sv, err
}

func parseCore(v string) (semver, error) {
	parts := strings.SplitN(v, ".", 3)
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, fmt.Errorf("invalid semver %q: %w", v, err)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, fmt.Errorf("invalid semver %q: %w", v, err)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semver{}, fmt.Errorf("invalid semver %q: %w", v, err)
	}
	return semver{Major: major, Minor: minor, Patch: patch}, nil
}

// compare returns -1, 0, or 1.
func (a semver) compare(b semver) int {
	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1
		}
		return 1
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "": // a release is newer than any pre-release of the same version
		return 1
	case b.Pre == "":
		return -1
	case a.Pre < b.Pre:
		return -1
	}
	return 1
}

// Satisfies reports whether installedVersion satisfies the constraint defined
// by (op, requiredVersion). An empty op means "any version" → always true.
// Returns false (not an error) when either version string is unparseable,
// so callers can treat unknown versions conservatively.
func Satisfies(installedVersion, op, requiredVersion string) bool {
	if op == "" || requiredVersion == "" {
		return true
	}
	installed, err := parseSemver(installedVersion)
	if err != nil {
		return false
	}
	required, err := parseSemver(requiredVersion)
	if err != nil {
		return false
	}
	cmp := installed.compare(required)
	switch op {
	case "==":
		return cmp == 0
	case ">=":
		return cmp >= 0
	case ">":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case "<":
		return cmp < 0
	case "~=":
		// compatible release: installed >= required AND installed.Major == required.Major
		// AND installed.Minor == required.Minor (patch-level flexibility)
		return cmp >= 0 &&
			installed.Major == required.Major &&
			installed.Minor == required.Minor
	}
	return false
}

// DependencySatisfied reports whether dep is satisfied by installedVersion.
func DependencySatisfied(dep Dependency, installedVersion string) bool {
	return Satisfies(installedVersion, dep.Op, dep.Version)
}
