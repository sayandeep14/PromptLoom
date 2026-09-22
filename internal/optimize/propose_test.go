package optimize

import (
	"context"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/format"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/parser"
)

// scriptedRefiner returns replies[i] on the i-th call and records what it was asked.
type scriptedRefiner struct {
	replies []string
	i       int
	seen    []llm.Request
	err     error
}

func (r *scriptedRefiner) Complete(_ context.Context, req llm.Request) (string, error) {
	r.seen = append(r.seen, req)
	if r.err != nil {
		return "", r.err
	}
	reply := r.replies[r.i]
	if r.i < len(r.replies)-1 {
		r.i++
	}
	return reply, nil
}

const proposeLibSrc = `block Guard {
  constraints :=
    - be safe
}

prompt Base {
  persona :=
    You are careful.
}

prompt Child inherits Base {
  use Guard
  slot repo { required: true }
  tags: a, b

  persona :=
    You review {{repo}} code.

  instructions :=
    - Be terse.

  variant strict {
    persona :=
      Be very strict.
  }

  env prod {
    persona :=
      Prod persona.
  }

  contract {
    must_include:
      - verdict
  }

  capabilities {
    allowed:
      - read_code
  }
}
`

func proposeLib(t *testing.T) string {
	t.Helper()
	out, err := format.Source("f.loom", proposeLibSrc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func someFeedback() []Feedback {
	return []Feedback{{
		Case: "terse review", Input: "review this",
		Weak: []WeakCriterion{{Criterion: "explains why", Score: 20, Note: "gives no reasoning"}},
	}}
}

func TestProposeAcceptsAValidRewrite(t *testing.T) {
	lib := proposeLib(t)
	good := "prompt Child inherits Base {\n  use Guard\n  slot repo { required: true }\n  tags: a, b\n\n  persona :=\n    You review {{repo}} code, explaining your reasoning.\n\n  instructions :=\n    - Be terse.\n    - Always explain your reasoning.\n\n  variant strict {\n    persona :=\n      Be very strict.\n  }\n\n  env prod {\n    persona :=\n      Prod persona.\n  }\n\n  contract {\n    must_include:\n      - verdict\n  }\n\n  capabilities {\n    allowed:\n      - read_code\n  }\n}\n"
	r := &scriptedRefiner{replies: []string{good}}
	cand, err := Propose(context.Background(), r, "f.loom", lib, "Child", someFeedback())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cand.Text, "explaining your reasoning") || len(cand.Fields) == 0 {
		t.Errorf("%+v", cand)
	}
	// the request shows the CURRENT declaration and the feedback, fenced/labelled clearly
	if len(r.seen) != 1 {
		t.Fatal("one call")
	}
	req := r.seen[0]
	if !strings.Contains(req.User, "You review {{repo}} code.") || !strings.Contains(req.User, "terse review") ||
		!strings.Contains(req.User, `"explains why" scored 20/100`) || !strings.Contains(req.User, "gives no reasoning") {
		t.Errorf("%s", req.User)
	}
	if !strings.Contains(req.System, "keep the exact same name") {
		t.Errorf("%s", req.System)
	}

	// the candidate applies cleanly through ReplaceFields, and everything else is unchanged
	out, err := format.ReplaceFields("f.loom", lib, "Child", cand.Fields)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "explaining your reasoning") || !strings.Contains(out, "contract {") || !strings.Contains(out, "variant strict {") {
		t.Errorf("%s", out)
	}
}

func TestProposeStripsACodeFenceTheModelAddedAnyway(t *testing.T) {
	lib := proposeLib(t)
	good := "```loom\nprompt Child inherits Base {\n  use Guard\n  slot repo { required: true }\n  tags: a, b\n\n  persona :=\n    Better.\n\n  instructions :=\n    - Be terse.\n\n  variant strict {\n    persona :=\n      Be very strict.\n  }\n\n  env prod {\n    persona :=\n      Prod persona.\n  }\n\n  contract {\n    must_include:\n      - verdict\n  }\n\n  capabilities {\n    allowed:\n      - read_code\n  }\n}\n```"
	r := &scriptedRefiner{replies: []string{good}}
	cand, err := Propose(context.Background(), r, "f.loom", lib, "Child", someFeedback())
	if err != nil || !strings.Contains(cand.Text, "Better.") {
		t.Fatalf("%+v %v", cand, err)
	}
}

func TestProposeRejectsStructuralChanges(t *testing.T) {
	lib := proposeLib(t)
	// A full, valid Child declaration with every structural part present; each case below drops
	// (or changes) exactly one part, so only that guard can be the reason it is rejected.
	full := func(inherits, use, slot, tags, variant, env, contract, caps bool) string {
		var b strings.Builder
		b.WriteString("prompt Child")
		if inherits {
			b.WriteString(" inherits Base")
		}
		b.WriteString(" {\n")
		if use {
			b.WriteString("  use Guard\n")
		}
		if slot {
			b.WriteString("  slot repo { required: true }\n")
		}
		if tags {
			b.WriteString("  tags: a, b\n")
		}
		b.WriteString("\n  persona :=\n    p\n")
		if variant {
			b.WriteString("\n  variant strict {\n    persona :=\n      s\n  }\n")
		}
		if env {
			b.WriteString("\n  env prod {\n    persona :=\n      s\n  }\n")
		}
		if contract {
			b.WriteString("\n  contract {\n    must_include:\n      - verdict\n  }\n")
		}
		if caps {
			b.WriteString("\n  capabilities {\n    allowed:\n      - read_code\n  }\n")
		}
		b.WriteString("}\n")
		return b.String()
	}
	cases := map[string]string{
		"renamed":              strings.Replace(full(true, true, true, true, true, true, true, true), "Child", "NotChild", 1),
		"dropped the parent":   full(false, true, true, true, true, true, true, true),
		"added a parent":       strings.Replace(full(true, true, true, true, true, true, true, true), "inherits Base {", "inherits Base, Guard {", 1),
		"dropped the use":      full(true, false, true, true, true, true, true, true),
		"dropped the slot":     full(true, true, false, true, true, true, true, true),
		"dropped the tags":     full(true, true, true, false, true, true, true, true),
		"dropped the variant":  full(true, true, true, true, false, true, true, true),
		"dropped the env":      full(true, true, true, true, true, false, true, true),
		"dropped the contract": full(true, true, true, true, true, true, false, true),
		"dropped capabilities": full(true, true, true, true, true, true, true, false),
	}
	for name, reply := range cases {
		r := &scriptedRefiner{replies: []string{reply}}
		_, err := Propose(context.Background(), r, "f.loom", lib, "Child", someFeedback())
		if err == nil {
			t.Errorf("%s: must be rejected", name)
			continue
		}
		if want := errKind(name); !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v (want it to mention %q)", name, err, want)
		}
	}
}

func errKind(name string) string {
	switch name {
	case "renamed":
		return "renamed"
	case "dropped the parent", "added a parent":
		return "inherits"
	case "dropped the use":
		return "use lines"
	case "dropped the slot":
		return "var/slot"
	case "dropped the tags":
		return "tags"
	case "dropped the contract":
		return "contract"
	case "dropped the variant":
		return "variant"
	case "dropped the env":
		return "env"
	case "dropped capabilities":
		return "capabilities"
	}
	return ""
}

func TestProposeRejectsBadReplies(t *testing.T) {
	lib := proposeLib(t)
	cases := map[string]string{
		"not DSL at all":   "Sure! Here's my improved version of the prompt.",
		"two declarations": "prompt Child inherits Base {\n  persona :=\n    a\n}\nprompt Extra {\n  persona :=\n    b\n}\n",
		"unparsable":       "prompt Child inherits Base {\n  persona :=\n",
	}
	for name, reply := range cases {
		r := &scriptedRefiner{replies: []string{reply}}
		if _, err := Propose(context.Background(), r, "f.loom", lib, "Child", someFeedback()); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
	// identical to the original: nothing to apply
	same := format.Node(mustNode(t, lib, "Child"))
	r := &scriptedRefiner{replies: []string{same}}
	if _, err := Propose(context.Background(), r, "f.loom", lib, "Child", someFeedback()); err == nil || !strings.Contains(err.Error(), "no change") {
		t.Errorf("%v", err)
	}
	// the refiner call itself fails
	r = &scriptedRefiner{err: context.Canceled}
	if _, err := Propose(context.Background(), r, "f.loom", lib, "Child", someFeedback()); err == nil {
		t.Error("expected an error")
	}
}

func TestProposeRefusesWithoutFeedbackOrOnANonPrompt(t *testing.T) {
	lib := proposeLib(t)
	r := &scriptedRefiner{replies: []string{"anything"}}
	if _, err := Propose(context.Background(), r, "f.loom", lib, "Child", nil); err == nil || !strings.Contains(err.Error(), "no feedback") {
		t.Errorf("%v", err)
	}
	if _, err := Propose(context.Background(), r, "f.loom", lib, "Guard", someFeedback()); err == nil || !strings.Contains(err.Error(), "block") {
		t.Errorf("%v", err)
	}
	if _, err := Propose(context.Background(), r, "f.loom", lib, "Nope", someFeedback()); err == nil || !strings.Contains(err.Error(), "no prompt named") {
		t.Errorf("%v", err)
	}
}

func mustNode(t *testing.T, src, name string) *ast.Node {
	t.Helper()
	nodes, err := parser.Parse("f.loom", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Name == name {
			return n
		}
	}
	t.Fatalf("no node %q", name)
	return nil
}
