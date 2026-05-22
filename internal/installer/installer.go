// Package installer downloads a vault bundle from the PromptLoom registry,
// writes the raw .loom source files, and compiles them to rendered .md files.
package installer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/parser"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/render"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
)

// defaultRegistryURL is used when LOOM_REGISTRY_URL is not set.
const defaultRegistryURL = "https://registry.promptloom.dev"

// RelatedLibrary mirrors the server's RelatedLibrary type.
type RelatedLibrary struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	ID      string `json:"id"`
}

// Bundle mirrors the server's Bundle wire format.
type Bundle struct {
	PackID           string           `json:"pack_id,omitempty"`
	Name             string           `json:"name"`
	Slug             string           `json:"slug"`
	Version          string           `json:"version"`
	Description      string           `json:"description"`
	Author           string           `json:"author"`
	Tags             []string         `json:"tags"`
	RelatedLibraries []RelatedLibrary `json:"relatedLibraries,omitempty"`
	Files            []BundleFile     `json:"files"`
}

// BundleFile mirrors the server's BundleFile wire format.
type BundleFile struct {
	Path     string `json:"path"`
	FileType string `json:"file_type"`
	Content  string `json:"content"`
}

// PackMeta is written to loompack/<name>/pack.json after a successful install.
type PackMeta struct {
	PackID        string    `json:"pack_id,omitempty"`
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	Version       string    `json:"version"`
	Description   string    `json:"description"`
	Author        string    `json:"author"`
	Tags          []string  `json:"tags"`
	InstalledAt   time.Time `json:"installed_at"`
	SourceCount   int       `json:"source_count"`
	CompiledCount int       `json:"compiled_count"`
	RegistryURL   string    `json:"registry_url"`
}

// Result holds what Install produced for display by the CLI.
type Result struct {
	PackDir     string
	SourceDir   string
	CompiledDir string
	Written     []string // relative paths of .loom files written
	Compiled    []string // relative paths of .md files written
	Meta        PackMeta
}

// Install fetches the named vault from the registry and installs it.
// In the new workspace structure (loom/ directory present), installs under
// loom/loompack/<vault>. Otherwise falls back to loompack/<vault>.
// PackDir returns the directory where a named pack is (or would be) installed.
func PackDir(slug, cwd string) string {
	packRoot := "loompack"
	if _, err := os.Stat(filepath.Join(cwd, "loom")); err == nil {
		packRoot = filepath.Join("loom", "loompack")
	}
	return filepath.Join(cwd, packRoot, slug)
}

func Install(vaultName, cwd string) (*Result, error) {
	registryURL := os.Getenv("LOOM_REGISTRY_URL")
	if registryURL == "" {
		registryURL = defaultRegistryURL
	}
	registryURL = strings.TrimRight(registryURL, "/")

	bundle, err := fetchBundle(registryURL, vaultName)
	if err != nil {
		return nil, err
	}

	packDir := PackDir(bundle.Slug, cwd)
	sourceDir := filepath.Join(packDir, "source")
	compiledDir := filepath.Join(packDir, "compiled")

	for _, d := range []string{sourceDir, compiledDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	result := &Result{
		PackDir:     packDir,
		SourceDir:   sourceDir,
		CompiledDir: compiledDir,
	}

	// Write source files preserving the bundle's directory structure.
	// Meta files (.metadata.loom, .dependency.loom, etc.) go directly in sourceDir.
	// Prompt/block/overlay files preserve their relative path.
	for _, f := range bundle.Files {
		dest := filepath.Join(sourceDir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, []byte(f.Content), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", f.Path, err)
		}
		if f.FileType != "meta" {
			result.Written = append(result.Written, f.Path)
		}
	}

	// Also write a .metadata.loom at the pack root for local tooling.
	writeBundleMetadata(packDir, bundle)

	// Compile: parse all loom source files, resolve and render prompts.
	compiled, err := compile(bundle, compiledDir)
	if err != nil {
		result.Compiled = nil
		_ = compiled
	} else {
		result.Compiled = compiled
	}

	// Write pack.json for the CLI display and local bookkeeping.
	meta := PackMeta{
		PackID:        bundle.PackID,
		Name:          bundle.Name,
		Slug:          bundle.Slug,
		Version:       bundle.Version,
		Description:   bundle.Description,
		Author:        bundle.Author,
		Tags:          bundle.Tags,
		InstalledAt:   time.Now().UTC(),
		SourceCount:   len(result.Written),
		CompiledCount: len(result.Compiled),
		RegistryURL:   registryURL,
	}
	result.Meta = meta
	if err := writePackMeta(packDir, meta); err != nil {
		return result, fmt.Errorf("write pack.json: %w", err)
	}

	return result, nil
}

// fetchBundle calls GET {registryURL}/api/v1/vaults/{name}/bundle.
func fetchBundle(registryURL, vaultName string) (*Bundle, error) {
	url := fmt.Sprintf("%s/api/v1/vaults/%s/bundle", registryURL, vaultName)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch bundle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("vault %q not found in registry", vaultName)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned %d for vault %q", resp.StatusCode, vaultName)
	}

	var bundle Bundle
	if err := json.NewDecoder(resp.Body).Decode(&bundle); err != nil {
		return nil, fmt.Errorf("decode bundle: %w", err)
	}
	if len(bundle.Files) == 0 {
		return nil, fmt.Errorf("vault %q has no files", vaultName)
	}
	return &bundle, nil
}

// compile parses all files in the bundle, registers them, resolves every prompt,
// and renders each one to compiledDir. Returns the list of written .md paths.
func compile(bundle *Bundle, compiledDir string) ([]string, error) {
	reg := registry.New()
	cfg := config.Defaults()

	// Parse and register every file in the bundle.
	for _, f := range bundle.Files {
		nodes, err := parser.Parse(f.Path, f.Content)
		if err != nil {
			// Skip files that fail to parse rather than aborting the whole compile.
			continue
		}
		_ = reg.Register(nodes)
	}

	var written []string
	for _, p := range reg.Prompts() {
		rp, err := resolve.Resolve(p.Name, reg)
		if err != nil {
			continue
		}
		md := render.Render(rp, cfg)
		outPath := filepath.Join(compiledDir, p.Name+".md")
		if err := os.WriteFile(outPath, []byte(md), 0o644); err != nil {
			continue
		}
		written = append(written, p.Name+".md")
	}
	return written, nil
}

func writePackMeta(packDir string, meta PackMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(packDir, "pack.json"), data, 0o644)
}

// writeBundleMetadata writes a .metadata.loom at packDir for local tooling.
// It synthesises the file from bundle fields if the bundle doesn't already
// include one as a file entry.
func writeBundleMetadata(packDir string, bundle *Bundle) {
	type relLib struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		ID      string `json:"id"`
	}
	type meta struct {
		ID               string   `json:"id,omitempty"`
		Slug             string   `json:"slug"`
		Version          string   `json:"version"`
		Name             string   `json:"name"`
		Author           string   `json:"author,omitempty"`
		Description      string   `json:"description,omitempty"`
		Tags             []string `json:"tags,omitempty"`
		RelatedLibraries []relLib `json:"relatedLibraries,omitempty"`
	}
	var libs []relLib
	for _, rl := range bundle.RelatedLibraries {
		libs = append(libs, relLib{Name: rl.Name, Version: rl.Version, ID: rl.ID})
	}
	m := meta{
		ID:               bundle.PackID,
		Slug:             bundle.Slug,
		Version:          bundle.Version,
		Name:             bundle.Name,
		Author:           bundle.Author,
		Description:      bundle.Description,
		Tags:             bundle.Tags,
		RelatedLibraries: libs,
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	data = append(data, '\n')
	_ = os.WriteFile(filepath.Join(packDir, ".metadata.loom"), data, 0o644)
}
