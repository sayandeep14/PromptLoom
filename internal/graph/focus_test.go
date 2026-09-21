package graph_test

import (
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/graph"
)

// Base ← Mid ← Leaf, Base ← Other, plus Solo, with blocks on different levels:
//
//	Guard  is used by Base and Solo
//	Style  is used by Leaf
//	Lonely is used by nobody
const lib = `
block Guard {
  constraints :=
    - safe
}
block Style {
  constraints :=
    - brief
}
block Lonely {
  constraints :=
    - x
}

prompt Base {
  use Guard
  persona :=
    b
}
prompt Mid inherits Base {
  persona :=
    m
}
prompt Leaf inherits Mid {
  use Style
  persona :=
    l
}
prompt Other inherits Base {
  persona :=
    o
}
prompt Solo {
  use Guard
  persona :=
    s
}
prompt Both inherits Leaf, Other {
  persona :=
    x
}
`

func build(t *testing.T) *graph.Graph {
	t.Helper()
	return graph.Build(buildReg(t, map[string]string{"lib.loom": lib}))
}

func names(rs []graph.Related) string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return strings.Join(out, ",")
}

func TestFocusOnAPrompt(t *testing.T) {
	g := build(t)
	nb, err := g.Focus("Mid")
	if err != nil {
		t.Fatal(err)
	}
	if nb.Kind != graph.KindPrompt || names(nb.Ancestors) != "Base" {
		t.Errorf("ancestors: %s", names(nb.Ancestors))
	}
	// descendants, nearest first; Both is reachable through Leaf only (Other is not below Mid)
	if names(nb.Descendants) != "Leaf,Both" || nb.Descendants[0].Depth != 1 || nb.Descendants[1].Depth != 2 {
		t.Errorf("descendants: %+v", nb.Descendants)
	}
	// Mid uses no block itself but gets Guard through Base
	if len(nb.Blocks) != 1 || nb.Blocks[0].Block != "Guard" || nb.Blocks[0].Via != "Base" {
		t.Errorf("blocks: %+v", nb.Blocks)
	}
}

func TestFocusOnAMultiParentPromptListsEveryAncestorOnce(t *testing.T) {
	g := build(t)
	nb, _ := g.Focus("Both")
	// Mid and Leaf and Other are ancestors; Base is reachable two ways but appears once
	if names(nb.Ancestors) != "Leaf,Other,Base,Mid" {
		t.Errorf("%s", names(nb.Ancestors))
	}
	blocks := map[string]bool{}
	for _, b := range nb.Blocks {
		blocks[b.Block+"@"+b.Via] = true
	}
	if !blocks["Style@Leaf"] || !blocks["Guard@Base"] || len(nb.Blocks) != 2 {
		t.Errorf("%+v", nb.Blocks)
	}
}

func TestFocusOnABlock(t *testing.T) {
	g := build(t)
	nb, _ := g.Focus("Guard")
	if nb.Kind != graph.KindBlock || strings.Join(nb.UsedBy, ",") != "Base,Solo" {
		t.Errorf("%+v", nb)
	}
	// everything below Base also gets Guard's content
	if names(nb.Descendants) != "Mid,Other,Both,Leaf" {
		t.Errorf("%s", names(nb.Descendants))
	}
	if nb, _ := g.Focus("Lonely"); len(nb.UsedBy) != 0 || len(nb.Descendants) != 0 {
		t.Errorf("an unused block: %+v", nb)
	}
}

func TestImpact(t *testing.T) {
	g := build(t)
	cases := []struct {
		name         string
		kind         graph.Kind
		direct, rest string
	}{
		{"Base", graph.KindPrompt, "Mid,Other", "Both,Leaf"},
		{"Mid", graph.KindPrompt, "Leaf", "Both"},
		{"Leaf", graph.KindPrompt, "Both", ""},
		{"Both", graph.KindPrompt, "", ""},
		{"Solo", graph.KindPrompt, "", ""},
		{"Guard", graph.KindBlock, "Base,Solo", "Mid,Other,Both,Leaf"},
		{"Style", graph.KindBlock, "Leaf", "Both"},
		{"Lonely", graph.KindBlock, "", ""},
	}
	for _, c := range cases {
		im, err := g.Impact(c.name)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if im.Kind != c.kind || strings.Join(im.Direct, ",") != c.direct || strings.Join(im.Transitive, ",") != c.rest {
			t.Errorf("%s: %s direct=%v transitive=%v", c.name, im.Kind, im.Direct, im.Transitive)
		}
		if im.Total() != len(im.Direct)+len(im.Transitive) {
			t.Errorf("%s total", c.name)
		}
	}
}

func TestUnknownNamesSuggest(t *testing.T) {
	g := build(t)
	_, err := g.Impact("Midd")
	if err == nil || !strings.Contains(err.Error(), `did you mean "Mid"?`) {
		t.Errorf("%v", err)
	}
	if _, err := g.Focus("zzzzzzzzzz"); err == nil || strings.Contains(err.Error(), "did you mean") {
		t.Errorf("a far-off name gets no suggestion: %v", err)
	}
}

func TestCyclesDoNotHang(t *testing.T) {
	g := graph.Build(buildReg(t, map[string]string{"c.loom": "prompt A inherits B {\n  persona :=\n    a\n}\nprompt B inherits A {\n  persona :=\n    b\n}\nprompt C inherits A {\n  persona :=\n    c\n}\n"}))
	im, err := g.Impact("A")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(append(im.Direct, im.Transitive...), ",") != "B,C" {
		t.Errorf("%+v", im)
	}
	if nb, _ := g.Focus("C"); names(nb.Ancestors) != "A,B" {
		t.Errorf("%s", names(nb.Ancestors))
	}
}

func TestFocusText(t *testing.T) {
	g := build(t)
	nb, _ := g.Focus("Mid")
	out := nb.Text()
	for _, want := range []string{"Mid  (prompt)", "Inherits from\n  Base", "Inherited by\n  Leaf\n    Both", "Guard  (via Base)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	root, _ := g.Focus("Solo")
	if out := root.Text(); !strings.Contains(out, "a base prompt") || !strings.Contains(out, "no prompt inherits from this one") {
		t.Errorf("%s", out)
	}
	blk, _ := g.Focus("Guard")
	if out := blk.Text(); !strings.Contains(out, "Used by\n  Base\n  Solo") || !strings.Contains(out, "Also reaches") {
		t.Errorf("%s", out)
	}
	if out, _ := g.Focus("Lonely"); !strings.Contains(out.Text(), "no prompt uses this block") {
		t.Errorf("%s", out.Text())
	}
}

// The old per-prompt Mermaid drew the whole library; the focused one must not.
func TestFocusDiagramsOnlyShowTheNeighbourhood(t *testing.T) {
	g := build(t)
	nb, _ := g.Focus("Mid")

	m := g.FocusMermaid(nb)
	for _, want := range []string{"Base --> Mid", "Mid --> Leaf", "Leaf --> Both", "Guard -.->|block| Base", "style Mid"} {
		if !strings.Contains(m, want) {
			t.Errorf("mermaid lacks %q:\n%s", want, m)
		}
	}
	for _, unwanted := range []string{"Solo", "Other", "Lonely", "Style"} {
		if strings.Contains(m, unwanted) {
			t.Errorf("mermaid shows %q, which is not related to Mid:\n%s", unwanted, m)
		}
	}

	d := g.FocusDOT(nb)
	for _, want := range []string{`"Base" -> "Mid";`, `"Guard" -> "Base" [style=dashed`, `"Mid" [shape=box, penwidth=3]`, `"Guard" [shape=component]`} {
		if !strings.Contains(d, want) {
			t.Errorf("dot lacks %q:\n%s", want, d)
		}
	}
	if strings.Contains(d, "Solo") {
		t.Errorf("dot shows an unrelated prompt:\n%s", d)
	}

	// a block: the prompts that use it and what they pass it on to
	blk, _ := g.Focus("Style")
	if bm := g.FocusMermaid(blk); !strings.Contains(bm, "Style -.->|block| Leaf") || strings.Contains(bm, "Solo") {
		t.Errorf("%s", bm)
	}
	// an isolated prompt still draws itself
	solo, _ := g.Focus("Solo")
	if sm := g.FocusMermaid(solo); !strings.Contains(sm, "Guard -.->|block| Solo") {
		t.Errorf("%s", sm)
	}
}

func TestImpactText(t *testing.T) {
	g := build(t)
	base, _ := g.Impact("Base")
	out := base.Text()
	for _, want := range []string{`Changing prompt "Base" affects 4 prompt(s)`, "Inherit from it directly (2)\n  Mid\n  Other", "Inherit further down the chain (2)\n  Both\n  Leaf"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	guard, _ := g.Impact("Guard")
	if out := guard.Text(); !strings.Contains(out, "Use the block directly (2)") || !strings.Contains(out, "Inherit it through those prompts (4)") {
		t.Errorf("%s", out)
	}
	lonely, _ := g.Impact("Lonely")
	if out := lonely.Text(); !strings.Contains(out, "safe to change or remove") {
		t.Errorf("%s", out)
	}
}
