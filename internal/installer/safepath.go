package installer

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var (
	slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	pathRe = regexp.MustCompile(`^[A-Za-z0-9._@+/-]+$`)
)

// maxBundleBytes caps how much of a registry response is read into memory.
const maxBundleBytes = 64 << 20

// safeRelPath rejects any bundle file path that could escape the pack directory
// (absolute paths, ".." segments, backslashes, drive letters, odd characters).
// The registry validates the same rules on upload; the client must not rely on that,
// since it may be talking to a third-party or compromised registry.
func safeRelPath(p string) error {
	if p == "" || len(p) > 255 || !pathRe.MatchString(p) ||
		strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return fmt.Errorf("unsafe file path %q in bundle", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("unsafe file path %q in bundle", p)
		}
	}
	return nil
}

// validateBundle checks a downloaded bundle before anything is written to disk.
func validateBundle(b *Bundle) error {
	if !slugRe.MatchString(b.Slug) {
		return fmt.Errorf("registry returned an invalid pack slug %q — refusing to install", b.Slug)
	}
	seen := make(map[string]bool, len(b.Files))
	for _, f := range b.Files {
		if err := safeRelPath(f.Path); err != nil {
			return fmt.Errorf("%w — refusing to install pack %q", err, b.Slug)
		}
		if seen[f.Path] {
			return fmt.Errorf("duplicate file %q in pack %q — refusing to install", f.Path, b.Slug)
		}
		seen[f.Path] = true
	}
	return nil
}
