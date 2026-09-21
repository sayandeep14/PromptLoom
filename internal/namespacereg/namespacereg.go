// Package namespacereg scans installed packs from the loompack/ directory and
// provides namespace-qualified lookups for prompts and blocks.
//
// Installed pack prompts/blocks are addressed as "slug.Name". Within-pack
// references remain bare (e.g. a pack file writing "inherits BaseEngineer"
// resolves to "slug.BaseEngineer" when the resolver is walking that pack).
package namespacereg

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/parser"
	"github.com/sayandeep14/PromptLoom/internal/registry"
)

// local copies of loader extension constants — avoids an import cycle with loader.
const (
	extPrompt  = ".prompt.loom"
	extBlock   = ".block.loom"
	extOverlay = ".overlay.loom"
	extMixed   = ".loom"
)

// NamespaceRegistry holds a per-pack registry for every installed pack,
// keyed by the pack's slug.
type NamespaceRegistry struct {
	// packs[slug] = registry of that pack's prompts/blocks/overlays
	packs map[string]*registry.Registry
}

// New returns an empty NamespaceRegistry.
func New() *NamespaceRegistry {
	return &NamespaceRegistry{packs: make(map[string]*registry.Registry)}
}

// Scan walks loompackDir, finds every installed pack, parses its source files,
// and returns a populated NamespaceRegistry.
// It is non-fatal: packs that fail to parse are skipped silently.
func Scan(loompackDir string) *NamespaceRegistry {
	nr := New()

	entries, err := os.ReadDir(loompackDir)
	if err != nil {
		return nr // loompack/ doesn't exist yet — no installed packs
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()
		packDir := filepath.Join(loompackDir, slug)
		reg := scanPack(packDir)
		if reg != nil && (reg.PromptCount() > 0 || reg.BlockCount() > 0) {
			nr.packs[slug] = reg
		}
	}
	return nr
}

// ScanProjectDir finds the loompack directory relative to projectDir
// (checking loom/loompack/ first, then loompack/) and scans it.
func ScanProjectDir(projectDir string) *NamespaceRegistry {
	candidates := []string{
		filepath.Join(projectDir, "loom", "loompack"),
		filepath.Join(projectDir, "loompack"),
	}
	for _, dir := range candidates {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return Scan(dir)
		}
	}
	return New()
}

// LookupPrompt returns the prompt node for "slug.Name" in the namespace registry.
func (nr *NamespaceRegistry) LookupPrompt(slug, name string) (*ast.Node, bool) {
	reg, ok := nr.packs[slug]
	if !ok {
		return nil, false
	}
	return reg.LookupPrompt(name)
}

// LookupBlock returns the block node for "slug.BlockName".
func (nr *NamespaceRegistry) LookupBlock(slug, name string) (*ast.Node, bool) {
	reg, ok := nr.packs[slug]
	if !ok {
		return nil, false
	}
	return reg.LookupBlock(name)
}

// LookupOverlay returns the overlay node for "slug.OverlayName".
func (nr *NamespaceRegistry) LookupOverlay(slug, name string) (*ast.Node, bool) {
	reg, ok := nr.packs[slug]
	if !ok {
		return nil, false
	}
	return reg.LookupOverlay(name)
}

// HasPack reports whether a pack with the given slug is installed.
func (nr *NamespaceRegistry) HasPack(slug string) bool {
	_, ok := nr.packs[slug]
	return ok
}

// Slugs returns the sorted list of installed pack slugs.
func (nr *NamespaceRegistry) Slugs() []string {
	out := make([]string, 0, len(nr.packs))
	for slug := range nr.packs {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

// scanPack parses all .loom source files inside an installed pack directory.
// It handles both the v2 layout (source/prompts/, source/blocks/, source/overlays/)
// and the legacy flat layout (source/*.loom).
func scanPack(packDir string) *registry.Registry {
	sourceDir := filepath.Join(packDir, "source")
	if fi, err := os.Stat(sourceDir); err != nil || !fi.IsDir() {
		return nil
	}

	reg := registry.New()

	// v2 layout: source/prompts/, source/blocks/, source/overlays/
	scanPackSubdir(filepath.Join(sourceDir, "prompts"), []string{extPrompt, extMixed}, reg)
	scanPackSubdir(filepath.Join(sourceDir, "blocks"), []string{extBlock, extMixed}, reg)
	scanPackSubdir(filepath.Join(sourceDir, "overlays"), []string{extOverlay, extMixed}, reg)

	// Legacy flat layout: source/*.loom (any kind)
	if reg.PromptCount() == 0 && reg.BlockCount() == 0 {
		scanPackSubdir(sourceDir, []string{extPrompt, extBlock, extOverlay, extMixed}, reg)
	}

	return reg
}

func scanPackSubdir(dir string, exts []string, reg *registry.Registry) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // directory absent — OK
	}
	for _, e := range entries {
		if e.IsDir() || !matchesExts(e.Name(), exts) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		nodes, err := parser.Parse(e.Name(), string(src))
		if err != nil {
			continue // skip files that fail to parse
		}
		_ = reg.Register(nodes) // ignore duplicate errors within a pack
	}
}

func matchesExts(name string, exts []string) bool {
	for _, ext := range exts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
