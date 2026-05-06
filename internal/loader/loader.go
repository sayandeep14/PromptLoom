// Package loader scans directories for .loom source files, parses them, and
// populates a registry. It is shared by all CLI commands that need to work
// with the full project.
package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/lexer"
	"github.com/sayandeepgiri/promptloom/internal/parser"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/secret"
)

// File extension constants for the new .loom file scheme.
const (
	ExtPrompt  = ".prompt.loom"
	ExtBlock   = ".block.loom"
	ExtOverlay = ".overlay.loom"
	ExtVars    = ".vars.loom"
	ExtMixed   = ".loom" // any combination of prompt/block/overlay
)

// promptExts are accepted in the prompts/ directory.
var promptExts = []string{ExtPrompt, ExtMixed}

// blockExts are accepted in the blocks/ directory.
var blockExts = []string{ExtBlock, ExtMixed}

// overlayExts are accepted in the overlays/ directory.
var overlayExts = []string{ExtOverlay, ExtMixed}

// matchesExts reports whether filename ends with any of the given suffixes.
// Longer suffixes are checked first so ".prompt.loom" wins over ".loom".
func matchesExts(name string, exts []string) bool {
	for _, ext := range exts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// IsLoomSource reports whether a file path is any kind of loom source file.
// Used by watchers and formatters.
func IsLoomSource(path string) bool {
	base := filepath.Base(path)
	return matchesExts(base, []string{ExtPrompt, ExtBlock, ExtOverlay, ExtVars, ExtMixed})
}

// Load reads loom.toml from dir, scans all .loom source files, parses them,
// and returns a populated registry and the config.
func Load(dir string) (*registry.Registry, *config.Config, error) {
	// Load .loomsecret before anything else so API keys are available to all commands.
	secret.Load(dir)

	cfg, err := config.Load(dir)
	if err != nil {
		return nil, nil, err
	}

	reg := registry.New()

	// Parse prompt files (.prompt.loom and .loom).
	if err := scanDir(filepath.Join(dir, cfg.Paths.Prompts), promptExts, reg); err != nil {
		return nil, nil, err
	}

	// Parse block files (.block.loom and .loom).
	if err := scanDir(filepath.Join(dir, cfg.Paths.Blocks), blockExts, reg); err != nil {
		return nil, nil, err
	}

	// Parse overlay files (.overlay.loom and .loom).
	if err := scanDir(filepath.Join(dir, cfg.Paths.Overlays), overlayExts, reg); err != nil {
		return nil, nil, err
	}

	// Load global variable declarations from .vars.loom files (project root only).
	if err := loadVarsFiles(dir, reg); err != nil {
		return nil, nil, err
	}

	return reg, cfg, nil
}

func scanDir(dir string, exts []string, reg *registry.Registry) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil // directory hasn't been created yet — that's fine
	}
	if err != nil {
		return fmt.Errorf("could not read directory %s: %w", dir, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			// Scan subdirectory as a pack namespace: nodes registered as "<subdir>/Name".
			if err := scanDirNamespaced(filepath.Join(dir, e.Name()), exts, e.Name(), reg); err != nil {
				return err
			}
			continue
		}
		if !matchesExts(e.Name(), exts) {
			continue
		}
		if err := parseAndRegister(filepath.Join(dir, e.Name()), "", reg); err != nil {
			return err
		}
	}
	return nil
}

// scanDirNamespaced scans a subdirectory and registers each node as "<namespace>/OriginalName".
func scanDirNamespaced(dir string, exts []string, namespace string, reg *registry.Registry) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("could not read directory %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !matchesExts(e.Name(), exts) {
			continue
		}
		if err := parseAndRegister(filepath.Join(dir, e.Name()), namespace, reg); err != nil {
			return err
		}
	}
	return nil
}

func parseAndRegister(path, namespace string, reg *registry.Registry) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", path, err)
	}
	nodes, err := parser.Parse(path, string(src))
	if err != nil {
		return err
	}
	if namespace != "" {
		for _, n := range nodes {
			n.Name = namespace + "/" + n.Name
		}
	}
	return reg.Register(nodes)
}

// loadVarsFiles scans dir for .vars.loom files and registers their declarations.
func loadVarsFiles(dir string, reg *registry.Registry) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("could not read directory %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ExtVars) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("could not read %s: %w", path, err)
		}
		vars, err := lexer.ScanVars(path, string(src))
		if err != nil {
			return err
		}
		reg.RegisterGlobalVars(vars)
	}
	return nil
}

// NodesByKind filters nodes from a slice by kind.
func NodesByKind(nodes []*ast.Node, kind ast.NodeKind) []*ast.Node {
	var out []*ast.Node
	for _, n := range nodes {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}
