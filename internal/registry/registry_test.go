package registry

import (
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
)

func node(kind ast.NodeKind, name string) *ast.Node {
	return &ast.Node{Kind: kind, Name: name, Pos: ast.Position{File: name + ".loom", Line: 1}}
}

func TestRegisterAndLookup(t *testing.T) {
	r := New()
	if err := r.Register([]*ast.Node{node(ast.KindPrompt, "P"), node(ast.KindBlock, "B"), node(ast.KindOverlay, "O")}); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.LookupPrompt("P"); !ok {
		t.Error("prompt")
	}
	if _, ok := r.LookupBlock("B"); !ok {
		t.Error("block")
	}
	if _, ok := r.LookupOverlay("O"); !ok {
		t.Error("overlay")
	}
	// kinds do not leak into each other
	if _, ok := r.LookupPrompt("B"); ok {
		t.Error("a block is not a prompt")
	}
	if r.PromptCount() != 1 || r.BlockCount() != 1 || r.OverlayCount() != 1 {
		t.Errorf("%d/%d/%d", r.PromptCount(), r.BlockCount(), r.OverlayCount())
	}
}

func TestDuplicatesAreReportedWithBothPositions(t *testing.T) {
	for kind, label := range map[ast.NodeKind]string{ast.KindPrompt: "prompt", ast.KindBlock: "block", ast.KindOverlay: "overlay"} {
		r := New()
		first, second := node(kind, "X"), node(kind, "X")
		first.Pos = ast.Position{File: "a.loom", Line: 3}
		second.Pos = ast.Position{File: "b.loom", Line: 9}
		if err := r.Register([]*ast.Node{first}); err != nil {
			t.Fatal(err)
		}
		err := r.Register([]*ast.Node{second})
		if err == nil || !strings.Contains(err.Error(), "duplicate "+label+` name "X"`) ||
			!strings.Contains(err.Error(), "b.loom:9") || !strings.Contains(err.Error(), "a.loom:3") {
			t.Errorf("%s: %v", label, err)
		}
	}
}

func TestSameNameInDifferentKindsIsAllowed(t *testing.T) {
	r := New()
	if err := r.Register([]*ast.Node{node(ast.KindPrompt, "Same"), node(ast.KindBlock, "Same")}); err != nil {
		t.Errorf("a prompt and a block may share a name: %v", err)
	}
}

func TestListingsAreSortedByName(t *testing.T) {
	r := New()
	var nodes []*ast.Node
	for _, n := range []string{"delta", "Alpha", "charlie", "Bravo", "echo"} {
		nodes = append(nodes, node(ast.KindPrompt, n), node(ast.KindBlock, n), node(ast.KindOverlay, n))
	}
	if err := r.Register(nodes); err != nil {
		t.Fatal(err)
	}
	want := "Alpha,Bravo,charlie,delta,echo"
	for i := 0; i < 20; i++ { // map iteration order is random: repeat to make a flaky order visible
		for label, list := range map[string][]*ast.Node{"prompts": r.Prompts(), "blocks": r.Blocks(), "overlays": r.Overlays()} {
			var names []string
			for _, n := range list {
				names = append(names, n.Name)
			}
			if strings.Join(names, ",") != want {
				t.Fatalf("%s not sorted: %v", label, names)
			}
		}
	}
}

type fakeNS map[string]*ast.Node // "slug.Name" -> node

func (f fakeNS) LookupPrompt(slug, name string) (*ast.Node, bool) {
	n, ok := f[slug+"."+name]
	return n, ok
}
func (f fakeNS) LookupBlock(slug, name string) (*ast.Node, bool) {
	n, ok := f["block:"+slug+"."+name]
	return n, ok
}
func (f fakeNS) LookupOverlay(string, string) (*ast.Node, bool) { return nil, false }

func TestNamespacedLookupOrder(t *testing.T) {
	r := New()
	local := node(ast.KindPrompt, "Base")
	packBase := node(ast.KindPrompt, "Base")
	packOnly := node(ast.KindPrompt, "OnlyInPack")
	r.Register([]*ast.Node{local})
	r.SetNamespaceRegistry(fakeNS{"kit.Base": packBase, "kit.OnlyInPack": packOnly})

	// explicit slug.Name goes straight to the pack
	if n, ns, ok := r.LookupPromptFull("kit.Base", ""); !ok || n != packBase || ns != "kit" {
		t.Errorf("explicit: %v %q %v", n, ns, ok)
	}
	// an unknown slug never falls back to a local prompt of the same local name
	if _, _, ok := r.LookupPromptFull("nope.Base", ""); ok {
		t.Error("an unknown pack must not resolve to a local prompt")
	}
	// bare name inside a pack: the pack wins over the project
	if n, ns, ok := r.LookupPromptFull("Base", "kit"); !ok || n != packBase || ns != "kit" {
		t.Errorf("context pack first: %v %q", n, ns)
	}
	// bare name with no context: local only
	if n, ns, ok := r.LookupPromptFull("Base", ""); !ok || n != local || ns != "" {
		t.Errorf("local: %v %q", n, ns)
	}
	if _, _, ok := r.LookupPromptFull("OnlyInPack", ""); ok {
		t.Error("a pack's prompt is not visible by bare name from the project")
	}
	// inside a pack, a name the pack lacks falls back to the project
	if n, _, ok := r.LookupPromptFull("Base", "other"); !ok || n != local {
		t.Error("fallback to the project")
	}
}

func TestNoNamespaceRegistry(t *testing.T) {
	r := New()
	r.Register([]*ast.Node{node(ast.KindPrompt, "P")})
	if _, _, ok := r.LookupPromptFull("slug.P", ""); ok {
		t.Error("a qualified name cannot resolve without installed packs")
	}
	if _, ok := r.LookupBlockFull("slug.B", ""); ok {
		t.Error("nor a qualified block")
	}
	if _, _, ok := r.LookupPromptFull("P", ""); !ok {
		t.Error("bare names still work")
	}
}
