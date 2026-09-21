// Package validate checks untrusted registry input before it reaches the store.
package validate

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sayandeep14/PromptLoom/server/internal/models"
)

// Limits on what a single pack may contain.
const (
	MaxFiles       = 500
	MaxFileBytes   = 1 << 20 // 1 MiB per file
	MaxPathLen     = 255
	MaxNameLen     = 100
	MaxDescLen     = 1000
	MaxAuthorLen   = 100
	MaxTags        = 20
	MaxTagLen      = 32
	MaxRelatedLibs = 50
)

var (
	slugRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	uuidRe    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	pathRe    = regexp.MustCompile(`^[A-Za-z0-9._@+/-]+$`)
	fileTypes = map[string]bool{"prompt": true, "block": true, "overlay": true, "meta": true}
)

// Slug reports whether s is a valid pack slug: lowercase letters, digits, '-' and '_',
// starting with a letter or digit, at most 64 characters.
func Slug(s string) error {
	if !slugRe.MatchString(s) {
		return fmt.Errorf("slug %q is invalid: use 1-64 lowercase letters, digits, '-' or '_', starting with a letter or digit", s)
	}
	return nil
}

// FilePath reports whether p is a safe relative path inside a pack. It rejects
// absolute paths, "." / ".." segments, backslashes, drive letters and anything
// outside a small portable character set, so a pack can never write outside its directory.
func FilePath(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("file path is empty")
	case len(p) > MaxPathLen:
		return fmt.Errorf("file path %q is longer than %d characters", p, MaxPathLen)
	case !pathRe.MatchString(p):
		return fmt.Errorf("file path %q contains characters outside A-Z a-z 0-9 . _ @ + - /", p)
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("file path %q must be relative", p)
	case path.Clean(p) != p:
		return fmt.Errorf("file path %q is not in canonical form (no '//', '.' or '..' segments)", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." || seg == "" {
			return fmt.Errorf("file path %q contains an illegal segment %q", p, seg)
		}
	}
	return nil
}

// Bundle validates an uploaded pack and returns every problem found.
func Bundle(b *models.Bundle) []string {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	if err := Slug(b.Slug); err != nil {
		add("%v", err)
	}
	if !versionRe.MatchString(b.Version) {
		add("version %q is invalid: use semantic versioning, e.g. 1.2.3", b.Version)
	}
	if b.PackID != "" && !uuidRe.MatchString(b.PackID) {
		add("pack_id %q is not a UUID", b.PackID)
	}
	if strings.TrimSpace(b.Name) == "" {
		add("name is required")
	}
	text := func(field, v string, max int) {
		if utf8.RuneCountInString(v) > max {
			add("%s is longer than %d characters", field, max)
		}
		for _, r := range v {
			if unicode.IsControl(r) && r != '\n' && r != '\t' {
				add("%s contains control characters", field)
				return
			}
		}
	}
	text("name", b.Name, MaxNameLen)
	text("description", b.Description, MaxDescLen)
	text("author", b.Author, MaxAuthorLen)

	if len(b.Tags) > MaxTags {
		add("too many tags (%d, max %d)", len(b.Tags), MaxTags)
	}
	for _, t := range b.Tags {
		if t == "" || utf8.RuneCountInString(t) > MaxTagLen {
			add("tag %q must be 1-%d characters", t, MaxTagLen)
		}
	}
	if len(b.RelatedLibraries) > MaxRelatedLibs {
		add("too many related libraries (%d, max %d)", len(b.RelatedLibraries), MaxRelatedLibs)
	}

	switch {
	case len(b.Files) == 0:
		add("files list is empty")
	case len(b.Files) > MaxFiles:
		add("too many files (%d, max %d)", len(b.Files), MaxFiles)
	}
	seen := make(map[string]bool, len(b.Files))
	for _, f := range b.Files {
		if err := FilePath(f.Path); err != nil {
			add("%v", err)
			continue
		}
		if seen[f.Path] {
			add("duplicate file path %q", f.Path)
		}
		seen[f.Path] = true
		if !fileTypes[f.FileType] {
			add("file %q has invalid file_type %q (want prompt, block, overlay or meta)", f.Path, f.FileType)
		}
		if len(f.Content) > MaxFileBytes {
			add("file %q is larger than %d bytes", f.Path, MaxFileBytes)
		}
		if strings.ContainsRune(f.Content, 0) || !utf8.ValidString(f.Content) {
			add("file %q is not valid UTF-8 text", f.Path)
		}
	}

	const maxReported = 20
	if len(errs) > maxReported {
		n := len(errs) - maxReported
		errs = append(errs[:maxReported], fmt.Sprintf("...and %d more problems", n))
	}
	return errs
}
