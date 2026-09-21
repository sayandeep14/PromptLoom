package graph

import (
	"fmt"
	"sort"
	"strings"
)

// Kind says whether a name refers to a prompt or a block.
type Kind string

const (
	KindPrompt Kind = "prompt"
	KindBlock  Kind = "block"
)

// Related is one prompt in a neighbourhood, with how far it is from the focus.
type Related struct {
	Name  string
	Depth int // 1 = direct parent / child
}

// BlockUse is a block that reaches a prompt, either directly or through an ancestor.
type BlockUse struct {
	Block string
	Via   string // "" when the prompt uses it itself, else the ancestor that does
}

// Neighborhood is everything directly relevant to one prompt or block.
type Neighborhood struct {
	Name        string
	Kind        Kind
	Ancestors   []Related  // prompt: transitive parents, nearest first
	Descendants []Related  // prompt: transitive children, nearest first
	Blocks      []BlockUse // prompt: blocks it gets
	UsedBy      []string   // block: prompts that use it directly
}

// Impact is the blast radius of changing a prompt or a block.
type Impact struct {
	Name       string
	Kind       Kind
	Direct     []string // prompts that use the block, or inherit from the prompt
	Transitive []string // everything further down the inheritance chain
}

// Total is how many prompts are affected.
func (i Impact) Total() int { return len(i.Direct) + len(i.Transitive) }

// Lookup reports what a name refers to. A name that is both a prompt and a block is a prompt.
func (g *Graph) Lookup(name string) (Kind, bool) {
	if _, ok := g.prompts[name]; ok {
		return KindPrompt, true
	}
	if _, ok := g.allBlocks[name]; ok {
		return KindBlock, true
	}
	return "", false
}

// Known returns every prompt and block name, sorted, for "did you mean" hints.
func (g *Graph) Known() []string {
	var out []string
	for n := range g.prompts {
		out = append(out, n)
	}
	for n := range g.allBlocks {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// walk returns the transitive closure over next, nearest first, each name once. Cycles are safe.
func walk(start string, next func(string) []string) []Related {
	var out []Related
	seen := map[string]bool{start: true}
	frontier := []string{start}
	for depth := 1; len(frontier) > 0; depth++ {
		var following []string
		for _, cur := range frontier {
			for _, n := range next(cur) {
				if seen[n] {
					continue
				}
				seen[n] = true
				following = append(following, n)
			}
		}
		sort.Strings(following)
		for _, n := range following {
			out = append(out, Related{Name: n, Depth: depth})
		}
		frontier = following
	}
	return out
}

func (g *Graph) parentsOf(name string) []string {
	n, ok := g.prompts[name]
	if !ok {
		return nil
	}
	var out []string
	for _, p := range n.Parents {
		if _, known := g.prompts[p]; known { // unresolvable parents are validation errors, not graph nodes
			out = append(out, p)
		}
	}
	return out
}

func (g *Graph) usersOf(block string) []string {
	var out []string
	for name, n := range g.prompts {
		for _, u := range n.Uses {
			if u == block {
				out = append(out, name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// Focus describes the neighbourhood of a prompt or block. The error lists close names when
// name is unknown.
func (g *Graph) Focus(name string) (*Neighborhood, error) {
	kind, ok := g.Lookup(name)
	if !ok {
		return nil, g.unknown(name)
	}
	nb := &Neighborhood{Name: name, Kind: kind}

	if kind == KindBlock {
		nb.UsedBy = g.usersOf(name)
		nb.Descendants = walkUsers(nb.UsedBy, g)
		return nb, nil
	}

	nb.Ancestors = walk(name, g.parentsOf)
	nb.Descendants = walk(name, func(n string) []string { return g.children[n] })

	seen := map[string]bool{}
	addBlocks := func(prompt, via string) {
		for _, b := range g.prompts[prompt].Uses {
			key := b + "\x00" + via
			if !seen[key] {
				seen[key] = true
				nb.Blocks = append(nb.Blocks, BlockUse{Block: b, Via: via})
			}
		}
	}
	addBlocks(name, "")
	for _, a := range nb.Ancestors {
		addBlocks(a.Name, a.Name)
	}
	return nb, nil
}

// walkUsers is the descendants of every prompt in users (excluding the users themselves).
func walkUsers(users []string, g *Graph) []Related {
	direct := map[string]bool{}
	for _, u := range users {
		direct[u] = true
	}
	best := map[string]int{}
	for _, u := range users {
		for _, d := range walk(u, func(n string) []string { return g.children[n] }) {
			if direct[d.Name] {
				continue
			}
			if cur, ok := best[d.Name]; !ok || d.Depth < cur {
				best[d.Name] = d.Depth
			}
		}
	}
	var out []Related
	for n, d := range best {
		out = append(out, Related{Name: n, Depth: d})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Impact computes who is affected when name changes: for a prompt, the prompts that inherit from
// it; for a block, the prompts that use it and everything that inherits from those.
func (g *Graph) Impact(name string) (*Impact, error) {
	nb, err := g.Focus(name)
	if err != nil {
		return nil, err
	}
	im := &Impact{Name: name, Kind: nb.Kind}
	if nb.Kind == KindBlock {
		im.Direct = nb.UsedBy
		for _, d := range nb.Descendants {
			im.Transitive = append(im.Transitive, d.Name)
		}
		return im, nil
	}
	for _, d := range nb.Descendants {
		if d.Depth == 1 {
			im.Direct = append(im.Direct, d.Name)
		} else {
			im.Transitive = append(im.Transitive, d.Name)
		}
	}
	return im, nil
}

func (g *Graph) unknown(name string) error {
	best, bestDist := "", 4
	for _, c := range g.Known() {
		if d := editDistance(strings.ToLower(name), strings.ToLower(c)); d < bestDist {
			best, bestDist = c, d
		}
	}
	if best != "" {
		return fmt.Errorf("no prompt or block named %q — did you mean %q?", name, best)
	}
	return fmt.Errorf("no prompt or block named %q", name)
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// ── rendering ────────────────────────────────────────────────────────────────

func indent(depth int) string { return strings.Repeat("  ", depth) }

// Text is a plain-text description of the neighbourhood.
func (n *Neighborhood) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  (%s)\n", n.Name, n.Kind)

	section := func(title string, rows []Related, empty string) {
		fmt.Fprintf(&b, "\n%s\n", title)
		if len(rows) == 0 {
			fmt.Fprintf(&b, "  %s\n", empty)
			return
		}
		for _, r := range rows {
			fmt.Fprintf(&b, "  %s%s\n", indent(r.Depth-1), r.Name)
		}
	}

	if n.Kind == KindBlock {
		fmt.Fprintf(&b, "\nUsed by\n")
		if len(n.UsedBy) == 0 {
			b.WriteString("  (no prompt uses this block)\n")
		}
		for _, u := range n.UsedBy {
			fmt.Fprintf(&b, "  %s\n", u)
		}
		section("Also reaches (through inheritance)", n.Descendants, "(nothing inherits from those prompts)")
		return b.String()
	}

	section("Inherits from", n.Ancestors, "(a base prompt: no parents)")
	section("Inherited by", n.Descendants, "(no prompt inherits from this one)")

	b.WriteString("\nBlocks\n")
	if len(n.Blocks) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, u := range n.Blocks {
		if u.Via == "" {
			fmt.Fprintf(&b, "  %s\n", u.Block)
		} else {
			fmt.Fprintf(&b, "  %s  (via %s)\n", u.Block, u.Via)
		}
	}
	return b.String()
}

// edges lists the drawing edges of the neighbourhood: inheritance (from → to) and block use.
func (g *Graph) focusEdges(n *Neighborhood) (inherit [][2]string, blocks [][2]string, nodes map[string]Kind) {
	nodes = map[string]Kind{n.Name: n.Kind}
	inSet := map[string]bool{n.Name: true}
	for _, a := range n.Ancestors {
		inSet[a.Name] = true
		nodes[a.Name] = KindPrompt
	}
	for _, d := range n.Descendants {
		inSet[d.Name] = true
		nodes[d.Name] = KindPrompt
	}
	if n.Kind == KindBlock {
		for _, u := range n.UsedBy {
			inSet[u] = true
			nodes[u] = KindPrompt
			blocks = append(blocks, [2]string{n.Name, u})
		}
	}
	names := make([]string, 0, len(inSet))
	for name := range inSet {
		if _, isPrompt := g.prompts[name]; isPrompt {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		for _, p := range g.parentsOf(name) {
			if inSet[p] {
				inherit = append(inherit, [2]string{p, name})
			}
		}
		if n.Kind == KindPrompt {
			for _, blk := range g.prompts[name].Uses {
				// show a block where it reaches the focus: on the focus itself or on an ancestor
				if name == n.Name || isAncestor(n, name) {
					blocks = append(blocks, [2]string{blk, name})
					nodes[blk] = KindBlock
				}
			}
		}
	}
	return
}

func isAncestor(n *Neighborhood, name string) bool {
	for _, a := range n.Ancestors {
		if a.Name == name {
			return true
		}
	}
	return false
}

// FocusMermaid draws only the neighbourhood, not the whole library.
func (g *Graph) FocusMermaid(n *Neighborhood) string {
	inherit, blocks, nodes := g.focusEdges(n)
	var b strings.Builder
	b.WriteString("graph TD\n")
	for _, e := range inherit {
		fmt.Fprintf(&b, "    %s --> %s\n", mermaidID(e[0]), mermaidID(e[1]))
	}
	for _, e := range blocks {
		fmt.Fprintf(&b, "    %s -.->|block| %s\n", mermaidID(e[0]), mermaidID(e[1]))
	}
	if len(inherit) == 0 && len(blocks) == 0 {
		fmt.Fprintf(&b, "    %s\n", mermaidID(n.Name))
	}
	names := make([]string, 0, len(nodes))
	for name := range nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if nodes[name] == KindBlock {
			fmt.Fprintf(&b, "    %s[[\"%s\"]]\n", mermaidID(name), name)
		}
	}
	fmt.Fprintf(&b, "    style %s stroke-width:3px\n", mermaidID(n.Name))
	return b.String()
}

// FocusDOT is FocusMermaid for Graphviz.
func (g *Graph) FocusDOT(n *Neighborhood) string {
	inherit, blocks, nodes := g.focusEdges(n)
	var b strings.Builder
	b.WriteString("digraph focus {\n    rankdir=TB;\n")
	names := make([]string, 0, len(nodes))
	for name := range nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		shape := "box"
		if nodes[name] == KindBlock {
			shape = "component"
		}
		extra := ""
		if name == n.Name {
			extra = ", penwidth=3"
		}
		fmt.Fprintf(&b, "    %q [shape=%s%s];\n", name, shape, extra)
	}
	for _, e := range inherit {
		fmt.Fprintf(&b, "    %q -> %q;\n", e[0], e[1])
	}
	for _, e := range blocks {
		fmt.Fprintf(&b, "    %q -> %q [style=dashed, label=\"block\"];\n", e[0], e[1])
	}
	b.WriteString("}\n")
	return b.String()
}

// Text describes an Impact for a terminal.
func (i Impact) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Changing %s %q affects %d prompt(s)\n", i.Kind, i.Name, i.Total())
	list := func(title string, names []string) {
		if len(names) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s (%d)\n", title, len(names))
		for _, n := range names {
			fmt.Fprintf(&b, "  %s\n", n)
		}
	}
	if i.Kind == KindBlock {
		list("Use the block directly", i.Direct)
		list("Inherit it through those prompts", i.Transitive)
	} else {
		list("Inherit from it directly", i.Direct)
		list("Inherit further down the chain", i.Transitive)
	}
	if i.Total() == 0 {
		b.WriteString("\nNothing depends on it: it is safe to change or remove.\n")
	}
	return b.String()
}
