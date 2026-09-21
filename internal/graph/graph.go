// Package graph builds and renders a dependency graph of a PromptLoom library.
package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/registry"
)

// Graph holds the dependency structure of a prompt library.
type Graph struct {
	prompts    map[string]*ast.Node
	children   map[string][]string // prompt name → names of prompts that directly inherit from it
	roots      []string            // prompts with no parents (or all parents unresolvable)
	usedBlocks map[string]bool     // block name → true if referenced by any prompt
	allBlocks  map[string]*ast.Node
	cycles     [][]string // each entry is a cycle path, e.g. ["A","B","A"]
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

	// Build parent→children edges using the full Parents slice.
	// A node with multiple parents becomes a child of every parent.
	for _, n := range g.prompts {
		for _, parentName := range n.Parents {
			g.children[parentName] = append(g.children[parentName], n.Name)
		}
		for _, blockName := range n.Uses {
			g.usedBlocks[blockName] = true
		}
	}
	for k := range g.children {
		sort.Strings(g.children[k])
	}

	// Roots: prompts that have no parents, or whose every declared parent is
	// absent from the registry (orphaned references).
	for name, n := range g.prompts {
		isRoot := true
		for _, parentName := range n.Parents {
			if _, ok := g.prompts[parentName]; ok {
				isRoot = false
				break
			}
		}
		if isRoot {
			g.roots = append(g.roots, name)
		}
	}
	sort.Strings(g.roots)

	// Detect cycles via DFS over the children edges.
	g.cycles = detectCycles(g.prompts, g.children)

	return g
}

// detectCycles runs DFS over the child edges and returns every cycle found.
// Each returned slice is a path that starts and ends at the same node,
// e.g. ["A", "B", "C", "A"].
func detectCycles(prompts map[string]*ast.Node, children map[string][]string) [][]string {
	var cycles [][]string
	visited := make(map[string]bool)
	onPath := make(map[string]bool)
	path := []string{}

	var dfs func(name string)
	dfs = func(name string) {
		if visited[name] {
			return
		}
		if onPath[name] {
			// Locate where in path this node first appears.
			for i, n := range path {
				if n == name {
					cycle := make([]string, len(path)-i+1)
					copy(cycle, path[i:])
					cycle[len(cycle)-1] = name
					cycles = append(cycles, cycle)
					return
				}
			}
			return
		}

		onPath[name] = true
		path = append(path, name)

		for _, child := range children[name] {
			dfs(child)
		}

		path = path[:len(path)-1]
		delete(onPath, name)
		visited[name] = true
	}

	// Sort for deterministic output.
	names := make([]string, 0, len(prompts))
	for name := range prompts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		dfs(name)
	}
	return cycles
}

// cycleSet returns a set of node names that participate in any cycle.
func (g *Graph) cycleSet() map[string]bool {
	s := make(map[string]bool)
	for _, cycle := range g.cycles {
		for _, name := range cycle {
			s[name] = true
		}
	}
	return s
}

// ASCII renders the full library as an indented tree.
// Nodes that appear under multiple parents are shown under each parent.
// Cycle members are rendered with a [cycle] marker and their subtree is not
// expanded further (to prevent infinite output).
// Pure-cycle nodes (those with no non-cycle ancestor) are shown at the bottom
// as floating entries so they always appear in the output.
func (g *Graph) ASCII() string {
	var b strings.Builder
	cycleNodes := g.cycleSet()
	visited := make(map[string]bool)

	for i, root := range g.roots {
		last := i == len(g.roots)-1
		g.renderNode(&b, root, "", last, true, make(map[string]bool), cycleNodes)
		// Track which nodes were reachable from this root.
		g.markReachable(root, visited, make(map[string]bool))
	}

	// Any node in a cycle that was never visited (no non-cycle ancestor) is
	// shown as a floating entry so cycles are always visible.
	var floating []string
	for name := range cycleNodes {
		if !visited[name] {
			floating = append(floating, name)
		}
	}
	sort.Strings(floating)
	if len(floating) > 0 {
		b.WriteString("\n  [cycle members with no base ancestor]\n")
		for _, name := range floating {
			b.WriteString("  " + name + " [cycle]\n")
		}
	}

	return b.String()
}

// markReachable does a DFS to populate visited without rendering.
// cycleGuard prevents infinite loops when following cycle edges.
func (g *Graph) markReachable(name string, visited, cycleGuard map[string]bool) {
	if visited[name] || cycleGuard[name] {
		return
	}
	visited[name] = true
	cycleGuard[name] = true
	for _, child := range g.children[name] {
		g.markReachable(child, visited, cycleGuard)
	}
	delete(cycleGuard, name)
}

// ASCIISubgraph renders only the subtree rooted at name.
func (g *Graph) ASCIISubgraph(name string) string {
	n, ok := g.prompts[name]
	if !ok {
		return fmt.Sprintf("  no prompt named %q\n", name)
	}
	var b strings.Builder
	b.WriteString("  " + nodeLine(n) + "\n")
	cycleNodes := g.cycleSet()
	children := g.children[name]
	for i, child := range children {
		g.renderNode(&b, child, "  ", i == len(children)-1, false, map[string]bool{name: true}, cycleNodes)
	}
	return b.String()
}

func (g *Graph) renderNode(b *strings.Builder, name, prefix string, last, isRoot bool, visiting map[string]bool, cycleNodes map[string]bool) {
	n, ok := g.prompts[name]
	if !ok {
		return
	}

	label := nodeLine(n)
	isCycleNode := cycleNodes[name]

	if isRoot {
		if isCycleNode {
			b.WriteString("  " + label + " [cycle]\n")
		} else {
			b.WriteString("  " + label + "\n")
		}
	} else {
		conn := "├── "
		if last {
			conn = "└── "
		}
		if isCycleNode && visiting[name] {
			// Already expanding this node above us — show the back-edge marker.
			b.WriteString("  " + prefix + conn + label + " ↩ [cycle]\n")
			return
		}
		if isCycleNode {
			b.WriteString("  " + prefix + conn + label + " [cycle]\n")
		} else {
			b.WriteString("  " + prefix + conn + label + "\n")
		}
	}

	// Stop expanding if we're already visiting this node (back-edge).
	if visiting[name] {
		return
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

	// Mark this node as currently being expanded.
	visiting[name] = true
	for i, child := range children {
		g.renderNode(b, child, childPrefix, i == len(children)-1, false, visiting, cycleNodes)
	}
	delete(visiting, name)
}

func nodeLine(n *ast.Node) string {
	if len(n.Uses) == 0 {
		return n.Name
	}
	return n.Name + " [" + strings.Join(n.Uses, ", ") + "]"
}

// AncestorTree renders the ancestry chain for a single prompt as a tree
// rooted at the target prompt, with parents/grandparents expanding below it.
//
// Example output for C inherits A, B:
//
//	C
//	├── parent[0]: A
//	│   └── parent[0]: Root
//	└── parent[1]: B
//
// Shared ancestors (reached through multiple paths in a diamond) are shown
// once in full and then referenced as "(shared)" on subsequent appearances.
// Cycle back-edges are shown as "↩ (cycle)".
func (g *Graph) AncestorTree(name string) string {
	n, ok := g.prompts[name]
	if !ok {
		return fmt.Sprintf("  no prompt named %q\n", name)
	}
	var b strings.Builder
	seen := make(map[string]bool)
	g.renderAncestors(&b, n, "", true, seen, make(map[string]bool))
	return b.String()
}

// renderAncestors recursively renders a node and its parents.
//
//   - prefix is the current line-drawing prefix (e.g. "│   ").
//   - isRoot marks the top-level node so it gets no branch connector.
//   - seen tracks nodes that have already been fully rendered (shared ancestors).
//   - onPath tracks nodes in the current DFS path (cycle detection).
func (g *Graph) renderAncestors(b *strings.Builder, n *ast.Node, prefix string, isRoot bool, seen, onPath map[string]bool) {
	if isRoot {
		b.WriteString("  " + n.Name + "\n")
		prefix = ""
	}

	// Collect parents that are resolved in this registry.
	type parentEntry struct {
		idx  int
		node *ast.Node
		name string // original reference (may be namespaced)
	}
	var parents []parentEntry
	var externalParents []string
	for i, parentRef := range n.Parents {
		if pn, ok := g.prompts[parentRef]; ok {
			parents = append(parents, parentEntry{i, pn, parentRef})
		} else {
			externalParents = append(externalParents, fmt.Sprintf("parent[%d]: %s (external)", i, parentRef))
		}
	}

	total := len(parents) + len(externalParents)
	idx := 0

	for _, pe := range parents {
		idx++
		isLast := idx == total
		conn, childIndent := branchChars(isLast)

		label := fmt.Sprintf("parent[%d]: %s", pe.idx, pe.node.Name)

		switch {
		case onPath[pe.node.Name]:
			// Back-edge — cycle.
			b.WriteString("  " + prefix + conn + label + " ↩ (cycle)\n")
		case seen[pe.node.Name]:
			// Already fully expanded through another path.
			b.WriteString("  " + prefix + conn + label + " (shared)\n")
		default:
			b.WriteString("  " + prefix + conn + label + "\n")
			seen[pe.node.Name] = true
			onPath[pe.node.Name] = true
			g.renderAncestors(b, pe.node, prefix+childIndent, false, seen, onPath)
			delete(onPath, pe.node.Name)
		}
	}

	for _, ext := range externalParents {
		idx++
		isLast := idx == total
		conn, _ := branchChars(isLast)
		b.WriteString("  " + prefix + conn + MutedRef(ext) + "\n")
	}
}

// branchChars returns the box-drawing connector and the child indent string
// for a tree node, depending on whether it is the last sibling.
func branchChars(isLast bool) (conn, childIndent string) {
	if isLast {
		return "└── ", "    "
	}
	return "├── ", "│   "
}

// MutedRef is a hook for callers that want to style external/unresolved
// references differently. The graph package itself just returns s unchanged;
// the TUI layer overrides it via the render functions.
func MutedRef(s string) string { return s }

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
		for _, parentName := range n.Parents {
			b.WriteString(fmt.Sprintf("    %s --> %s\n",
				mermaidID(parentName), mermaidID(n.Name)))
		}
		for _, block := range n.Uses {
			b.WriteString(fmt.Sprintf("    %s -.->|block| %s\n",
				mermaidID(block), mermaidID(n.Name)))
		}
	}
	// Mark cycle members.
	cycleNodes := g.cycleSet()
	for name := range cycleNodes {
		b.WriteString(fmt.Sprintf("    style %s fill:#f88,stroke:#f00\n", mermaidID(name)))
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
	// Mark cycle nodes with a red border.
	cycleNodes := g.cycleSet()
	for name := range cycleNodes {
		b.WriteString(fmt.Sprintf("    %q [color=red, penwidth=2];\n", name))
	}
	for _, n := range g.sortedPrompts() {
		for _, parentName := range n.Parents {
			if _, ok := g.prompts[parentName]; ok {
				b.WriteString(fmt.Sprintf("    %q -> %q;\n", parentName, n.Name))
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

// Roots returns the root prompt names (those with no resolvable parent in the registry).
func (g *Graph) Roots() []string { return g.roots }

// Children returns the names of prompts that directly inherit from name.
func (g *Graph) Children(name string) []string { return g.children[name] }

// Prompt returns the AST node for the named prompt, or nil.
func (g *Graph) Prompt(name string) *ast.Node { return g.prompts[name] }

// Cycles returns all detected inheritance cycles. Each entry is a path that
// starts and ends at the same node, e.g. ["A", "B", "C", "A"].
func (g *Graph) Cycles() [][]string { return g.cycles }

// HasCycles reports whether any inheritance cycle exists in the graph.
func (g *Graph) HasCycles() bool { return len(g.cycles) > 0 }
