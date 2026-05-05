// Package graph builds and renders a dependency graph of a PromptLoom library.
package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/registry"
)

// Graph holds the dependency structure of a prompt library.
type Graph struct {
	prompts    map[string]*ast.Node
	children   map[string][]string // prompt name → names of prompts that inherit from it
	roots      []string            // prompts with no parent (or orphaned parent)
	usedBlocks map[string]bool     // block name → true if referenced by any prompt
	allBlocks  map[string]*ast.Node
}

// Build constructs a Graph from a registry.
func Build(reg *registry.Registry) *Graph {
	g := &Graph{
		prompts:    make(map[string]*ast.Node),
		children:   make(map[string][]string),
		usedBlocks: make(map[string]bool),
		allBlocks:  make(map[string]*ast.Node),
	}

	for _, n := range reg.Prompts() {
		g.prompts[n.Name] = n
	}
	for _, n := range reg.Blocks() {
		g.allBlocks[n.Name] = n
	}

	// Build parent→children relationships and track block usage.
	for _, n := range g.prompts {
		if n.Parent != "" {
			g.children[n.Parent] = append(g.children[n.Parent], n.Name)
		}
		for _, blockName := range n.Uses {
			g.usedBlocks[blockName] = true
		}
	}
	for k := range g.children {
		sort.Strings(g.children[k])
	}

	// Roots: prompts with no parent, or whose parent is not in the registry.
	for name, n := range g.prompts {
		if n.Parent == "" {
			g.roots = append(g.roots, name)
		} else if _, ok := g.prompts[n.Parent]; !ok {
			g.roots = append(g.roots, name)
		}
	}
	sort.Strings(g.roots)

	return g
}

// ASCII renders the full library as an indented tree.
func (g *Graph) ASCII() string {
	var b strings.Builder
	for i, root := range g.roots {
		last := i == len(g.roots)-1
		g.renderNode(&b, root, "", last, true)
	}
	return b.String()
}

// ASCIISubgraph renders only the subtree rooted at name.
func (g *Graph) ASCIISubgraph(name string) string {
	n, ok := g.prompts[name]
	if !ok {
		return fmt.Sprintf("  no prompt named %q\n", name)
	}
	var b strings.Builder
	b.WriteString("  " + nodeLine(n) + "\n")
	children := g.children[name]
	for i, child := range children {
		g.renderNode(&b, child, "  ", i == len(children)-1, false)
	}
	return b.String()
}

func (g *Graph) renderNode(b *strings.Builder, name, prefix string, last, isRoot bool) {
	n, ok := g.prompts[name]
	if !ok {
		return
	}

	if isRoot {
		b.WriteString("  " + nodeLine(n) + "\n")
	} else {
		conn := "├── "
		if last {
			conn = "└── "
		}
		b.WriteString("  " + prefix + conn + nodeLine(n) + "\n")
	}

	children := g.children[name]
	childPrefix := prefix
	if isRoot {
		childPrefix = ""
	} else if last {
		childPrefix = prefix + "    "
	} else {
		childPrefix = prefix + "│   "
	}

	for i, child := range children {
		g.renderNode(b, child, childPrefix, i == len(children)-1, false)
	}
}

func nodeLine(n *ast.Node) string {
	if len(n.Uses) == 0 {
		return n.Name
	}
	return n.Name + " [" + strings.Join(n.Uses, ", ") + "]"
}

// Unused returns block names defined but not used by any prompt.
func (g *Graph) Unused() []string {
	var out []string
	for name := range g.allBlocks {
		if !g.usedBlocks[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Mermaid renders the graph as a Mermaid flowchart.
func (g *Graph) Mermaid() string {
	var b strings.Builder
	b.WriteString("graph TD\n")
	for _, n := range g.sortedPrompts() {
		if n.Parent != "" {
			b.WriteString(fmt.Sprintf("    %s --> %s\n",
				mermaidID(n.Parent), mermaidID(n.Name)))
		}
		for _, block := range n.Uses {
			b.WriteString(fmt.Sprintf("    %s -.->|block| %s\n",
				mermaidID(block), mermaidID(n.Name)))
		}
	}
	return b.String()
}

// DOT renders the graph in Graphviz DOT format.
func (g *Graph) DOT() string {
	var b strings.Builder
	b.WriteString("digraph loom {\n")
	b.WriteString("    rankdir=TD;\n")
	b.WriteString("    node [shape=box, fontname=monospace];\n")
	for name := range g.allBlocks {
		b.WriteString(fmt.Sprintf("    %q [shape=ellipse, style=dashed];\n", name))
	}
	for _, n := range g.sortedPrompts() {
		if n.Parent != "" {
			if _, ok := g.prompts[n.Parent]; ok {
				b.WriteString(fmt.Sprintf("    %q -> %q;\n", n.Parent, n.Name))
			}
		}
		for _, block := range n.Uses {
			b.WriteString(fmt.Sprintf("    %q -> %q [style=dashed, label=\"block\"];\n", block, n.Name))
		}
	}
	b.WriteString("}\n")
	return b.String()
}

func (g *Graph) sortedPrompts() []*ast.Node {
	out := make([]*ast.Node, 0, len(g.prompts))
	for _, n := range g.prompts {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func mermaidID(name string) string {
	r := strings.NewReplacer(" ", "_", "-", "_", "/", "_", ".", "_")
	return r.Replace(name)
}

// Roots returns the root prompt names (those with no parent in the registry).
func (g *Graph) Roots() []string { return g.roots }

// Children returns the names of prompts that directly inherit from name.
func (g *Graph) Children(name string) []string { return g.children[name] }

// Prompt returns the AST node for the named prompt, or nil.
func (g *Graph) Prompt(name string) *ast.Node { return g.prompts[name] }
