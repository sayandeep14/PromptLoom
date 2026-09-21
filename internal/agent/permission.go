package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Permission holds the `permission` section of .loom.config: which paths a command may read (to
// show to a model) and write (transcripts). A missing section, or "*", allows everything, which
// is the behaviour before the setting was enforced.
type Permission struct {
	Read  []string `json:"read"`
	Write []string `json:"write"`

	root string // the project root patterns are relative to (where .loom.config lives)
}

// LoadPermission reads .loom.config from dir upward. Without one, everything is allowed.
func LoadPermission(dir string) (*Permission, error) {
	for d := dir; ; {
		p := filepath.Join(d, ".loom.config")
		if data, err := os.ReadFile(p); err == nil {
			var cfg struct {
				Permission *Permission `json:"permission"`
			}
			if err := json.Unmarshal(data, &cfg); err != nil {
				return nil, fmt.Errorf("parse %s: %w", p, err)
			}
			perm := cfg.Permission
			if perm == nil {
				perm = &Permission{Read: []string{"*"}, Write: []string{"*"}}
			}
			perm.root = d
			return perm, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return &Permission{Read: []string{"*"}, Write: []string{"*"}, root: dir}, nil
}

// AllowsAllReads reports whether read access is unrestricted.
func (p *Permission) AllowsAllReads() bool { return allowsAll(p.Read) }

func allowsAll(patterns []string) bool {
	if len(patterns) == 0 {
		return false // an explicit empty list means nothing is allowed
	}
	for _, pat := range patterns {
		if strings.TrimSpace(pat) == "*" {
			return true
		}
	}
	return false
}

// CheckRead reports whether the path may be read; the error says which setting to change.
func (p *Permission) CheckRead(target string) error {
	return p.check("read", p.Read, target)
}

// CheckWrite is CheckRead for files a command would write.
func (p *Permission) CheckWrite(target string) error {
	return p.check("write", p.Write, target)
}

func (p *Permission) check(kind string, patterns []string, target string) error {
	if allowsAll(patterns) {
		return nil
	}
	rel, ok := p.relative(target)
	if ok {
		for _, pat := range patterns {
			if matches(strings.TrimSpace(pat), rel) {
				return nil
			}
		}
	}
	shown := target
	if ok {
		shown = rel
	}
	return fmt.Errorf("%s is not allowed by permission.%s in .loom.config (allowed: %s)", shown, kind, listOrNone(patterns))
}

func listOrNone(p []string) string {
	if len(p) == 0 {
		return "nothing"
	}
	return strings.Join(p, ", ")
}

// relative returns the slash-separated path of target below the project root, resolving
// symlinks so a link inside the project cannot reach a file outside it. ok is false for a path
// outside the root.
func (p *Permission) relative(target string) (string, bool) {
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(p.root, abs)
	}
	abs = resolveExisting(abs)
	root := resolveExisting(p.root)
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// resolveExisting resolves symlinks in the longest existing prefix (the file itself may not exist
// yet, as for a transcript).
func resolveExisting(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// matches implements the small pattern language of permission lists:
//
//	dir/   dir/*   dir/**   everything below dir
//	*.md   README.md        a name (matched against the file name) or, with a slash, a path
//	docs/*.md               path.Match against the whole relative path
func matches(pattern, rel string) bool {
	if pattern == "" {
		return false
	}
	for _, suffix := range []string{"/**", "/*", "/"} {
		if strings.HasSuffix(pattern, suffix) {
			dir := strings.TrimSuffix(pattern, suffix)
			return rel == dir || strings.HasPrefix(rel, strings.TrimSuffix(dir, "/")+"/")
		}
	}
	if !strings.Contains(pattern, "/") {
		ok, _ := path.Match(pattern, path.Base(rel))
		return ok
	}
	ok, _ := path.Match(pattern, rel)
	return ok
}

// CheckSources applies permission.read to the context a command would attach to a prompt
// (`--with SPEC` and `--context BUNDLE`), BEFORE anything is read:
//
//	file:PATH, dir:PATH   the path must be readable
//	stdin                 the user's own input: always allowed
//	git:...               reads repository content: needs unrestricted read access
//	--context BUNDLE      expands to many files: needs unrestricted read access
//
// cwd resolves relative paths, exactly as the source resolver does.
func (p *Permission) CheckSources(cwd string, specs []string, bundle string) error {
	for _, spec := range specs {
		kind, arg, _ := strings.Cut(spec, ":")
		switch strings.ToLower(kind) {
		case "file", "dir":
			target := arg
			if !filepath.IsAbs(target) {
				target = filepath.Join(cwd, target)
			}
			if err := p.CheckRead(target); err != nil {
				return fmt.Errorf("--with %s: %w", spec, err)
			}
		case "git":
			if !p.AllowsAllReads() {
				return fmt.Errorf("--with %s: git sources read repository content, which needs permission.read to include \"*\" in .loom.config", spec)
			}
		}
	}
	if bundle != "" && !p.AllowsAllReads() {
		return fmt.Errorf("--context %s: a context bundle expands to many files, which needs permission.read to include \"*\" in .loom.config", bundle)
	}
	return nil
}
