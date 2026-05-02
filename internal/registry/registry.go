// Package registry collects all parsed prompt and block nodes and provides
// duplicate-detection and name-lookup services.
package registry

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/ast"
)

// Registry holds all known prompts and blocks indexed by name.
type Registry struct {
	prompts map[string]*ast.Node
	blocks  map[string]*ast.Node
	overlays map[string]*ast.Node
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		prompts: make(map[string]*ast.Node),
		blocks:  make(map[string]*ast.Node),
		overlays: make(map[string]*ast.Node),
	}
}

// Register adds all nodes to the registry. Returns an error on the first
// duplicate name (prompts and blocks share the same namespace).
func (r *Registry) Register(nodes []*ast.Node) error {
	for _, n := range nodes {
		switch n.Kind {
		case ast.KindPrompt:
			if existing, ok := r.prompts[n.Name]; ok {
				return fmt.Errorf("%s: duplicate prompt name %q (first defined at %s)",
					n.Pos, n.Name, existing.Pos)
			}
			r.prompts[n.Name] = n
		case ast.KindBlock:
			if existing, ok := r.blocks[n.Name]; ok {
				return fmt.Errorf("%s: duplicate block name %q (first defined at %s)",
					n.Pos, n.Name, existing.Pos)
			}
			r.blocks[n.Name] = n
		case ast.KindOverlay:
			if existing, ok := r.overlays[n.Name]; ok {
				return fmt.Errorf("%s: duplicate overlay name %q (first defined at %s)",
					n.Pos, n.Name, existing.Pos)
			}
			r.overlays[n.Name] = n
		}
	}
	return nil
}

// LookupPrompt returns the prompt node with the given name, if it exists.
func (r *Registry) LookupPrompt(name string) (*ast.Node, bool) {
	n, ok := r.prompts[name]
	return n, ok
}

// LookupBlock returns the block node with the given name, if it exists.
func (r *Registry) LookupBlock(name string) (*ast.Node, bool) {
	n, ok := r.blocks[name]
	return n, ok
}

// LookupOverlay returns the overlay node with the given name, if it exists.
func (r *Registry) LookupOverlay(name string) (*ast.Node, bool) {
	n, ok := r.overlays[name]
	return n, ok
}

// Prompts returns all registered prompts as an unordered slice.
func (r *Registry) Prompts() []*ast.Node {
	out := make([]*ast.Node, 0, len(r.prompts))
	for _, n := range r.prompts {
		out = append(out, n)
	}
	return out
}

// Blocks returns all registered blocks as an unordered slice.
func (r *Registry) Blocks() []*ast.Node {
	out := make([]*ast.Node, 0, len(r.blocks))
	for _, n := range r.blocks {
		out = append(out, n)
	}
	return out
}

// Overlays returns all registered overlays as an unordered slice.
func (r *Registry) Overlays() []*ast.Node {
	out := make([]*ast.Node, 0, len(r.overlays))
	for _, n := range r.overlays {
		out = append(out, n)
	}
	return out
}

// PromptCount returns the number of registered prompts.
func (r *Registry) PromptCount() int { return len(r.prompts) }

// BlockCount returns the number of registered blocks.
func (r *Registry) BlockCount() int { return len(r.blocks) }

// OverlayCount returns the number of registered overlays.
func (r *Registry) OverlayCount() int { return len(r.overlays) }
