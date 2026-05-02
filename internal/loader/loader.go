// Package loader scans directories for .prompt files, parses them, and
// populates a registry. It is shared by all CLI commands that need to work
// with the full project.
package loader

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/parser"
	"github.com/sayandeepgiri/promptloom/internal/registry"
)

// Load reads loom.toml from dir, scans all .prompt files, parses them, and
// returns a populated registry and the config. Parse errors are returned
// immediately as a non-nil error.
func Load(dir string) (*registry.Registry, *config.Config, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, nil, err
	}

	reg := registry.New()

	// Parse prompt files.
	if err := scanDir(filepath.Join(dir, cfg.Paths.Prompts), ".prompt", reg); err != nil {
		return nil, nil, err
	}

	// Parse block files.
	if err := scanDir(filepath.Join(dir, cfg.Paths.Blocks), ".prompt", reg); err != nil {
		return nil, nil, err
	}

	// Parse overlay files.
	if err := scanDir(filepath.Join(dir, cfg.Paths.Overlays), ".overlay", reg); err != nil {
		return nil, nil, err
	}

	return reg, cfg, nil
}

func scanDir(dir, ext string, reg *registry.Registry) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil // directory hasn't been created yet — that's fine
	}
	if err != nil {
		return fmt.Errorf("could not read directory %s: %w", dir, err)
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ext {
			continue
		}
		path := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("could not read %s: %w", path, err)
		}
		nodes, err := parser.Parse(path, string(src))
		if err != nil {
			return err // parse error already contains file:line
		}
		if err := reg.Register(nodes); err != nil {
			return err
		}
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
