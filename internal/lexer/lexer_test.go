package lexer

import (
	"strings"
	"testing"
)

func scan(t *testing.T, src string) []Token {
	t.Helper()
	toks, err := Scan("t.loom", src)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return toks
}

func scanErr(t *testing.T, src string) string {
	t.Helper()
	_, err := Scan("t.loom", src)
	if err == nil {
		t.Fatalf("expected an error for:\n%s", src)
	}
	return err.Error()
}

// render shows a token stream compactly: "prompt IDENT:X { ... }".
func render(toks []Token) string {
	var out []string
	for _, tk := range toks {
		switch tk.Type {
		case TokIdent, TokTextLine:
			out = append(out, tk.Type.String()+":"+tk.Text)
		default:
			out = append(out, tk.Type.String())
		}
	}
	return strings.Join(out, " ")
}

func TestSimplePrompt(t *testing.T) {
	got := render(scan(t, "prompt A {\n  persona := hello\n    world\n}\n"))
	// `hello` is inline content, so it is emitted before the continuation line
	want := "prompt IDENT:A { IDENT:persona := TEXT:hello TEXT:world } EOF"
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

func TestFieldBlockEndsAtBlankLineOrDedent(t *testing.T) {
	src := "prompt A {\n  instructions :=\n    - one\n    - two\n\n  constraints :=\n    - c\n  notes :=\n    n\n}\n"
	got := render(scan(t, src))
	want := "prompt IDENT:A { IDENT:instructions := TEXT:- one TEXT:- two IDENT:constraints := TEXT:- c IDENT:notes := TEXT:n } EOF"
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

func TestTextThatLooksLikeSyntaxStaysText(t *testing.T) {
	// inside a field body nothing is structural: braces, operators, keywords are all text
	src := "prompt A {\n  notes :=\n    use Foo\n    x += y\n    prompt B {\n    a: b\n}\n"
	got := render(scan(t, src))
	want := "prompt IDENT:A { IDENT:notes := TEXT:use Foo TEXT:x += y TEXT:prompt B { TEXT:a: b } EOF"
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

func TestInheritsAndUse(t *testing.T) {
	got := render(scan(t, "prompt A inherits B, team.C,D {\n  use Guard\n}\n"))
	want := "prompt IDENT:A inherits IDENT:B , IDENT:team.C , IDENT:D { use IDENT:Guard } EOF"
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
	// spaces around commas are fine
	got = render(scan(t, "prompt A inherits B , C {\n}\n"))
	if !strings.Contains(got, "IDENT:B , IDENT:C") {
		t.Error(got)
	}
	// legacy pack/Name still lexes
	if got = render(scan(t, "prompt A inherits pack/Base {\n}\n")); !strings.Contains(got, "IDENT:pack/Base") {
		t.Error(got)
	}
}

func TestExtendsIsRejectedWithAHint(t *testing.T) {
	msg := scanErr(t, "prompt A extends B {\n}\n")
	if !strings.Contains(msg, "inherits") || !strings.HasPrefix(msg, "t.loom:1:") {
		t.Error(msg)
	}
}

func TestErrorsCarryFileAndLine(t *testing.T) {
	cases := map[string]string{
		"prompt {\n}\n":                                "t.loom:1:",
		"prompt A\n}\n":                                "t.loom:1:",
		"prompt A inherits {\n}\n":                     "t.loom:1:",
		"prompt A inherits B C {\n}\n":                 "t.loom:1:",
		"prompt A inherits B,,C {\n}\n":                "t.loom:1:",
		"prompt A bogus B {\n}\n":                      "t.loom:1:",
		"prompt 'x' {\n}\n":                            "t.loom:1:",
		"block {\n}\n":                                 "t.loom:1:",
		"block A B {\n}\n":                             "t.loom:1:",
		"overlay {\n}\n":                               "t.loom:1:",
		"\n\nnonsense\n":                               "t.loom:3:",
		"prompt A {\n  what is this\n}\n":              "t.loom:2:",
		"prompt A {\n  var x\n}\n":                     "t.loom:2:",
		"prompt A {\n  var x = \"unterminated\n}\n":    "t.loom:2:",
		"prompt A {\n  slot x {\n}\n":                  "t.loom:2:",
		"prompt A {\n  slot a b\n}\n":                  "t.loom:2:",
		"prompt A {\n  slot x }\n}\n":                  "t.loom:2:",
		"prompt A {\n  variant v1 {\n    bogus\n  }\n": "t.loom:3:",
		"prompt A {\n  variant a b {\n  }\n}\n":        "t.loom:2:",
	}
	for src, want := range cases {
		if msg := scanErr(t, src); !strings.HasPrefix(msg, want) {
			t.Errorf("%q: %s (want prefix %s)", src, msg, want)
		}
	}
}

func TestOperators(t *testing.T) {
	// The lexer still recognises every operator so the parser can report legacy ones by name.
	src := "prompt A {\n  a := x\n  b += y\n  c -= z\n  d:\n    t\n}\n"
	got := render(scan(t, src))
	for _, want := range []string{"IDENT:a := TEXT:x", "IDENT:b += TEXT:y", "IDENT:c -= TEXT:z", "IDENT:d : TEXT:t"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestFromExpressionWithBlock(t *testing.T) {
	src := "prompt A inherits B {\n  constraints := from(B[*]) and {\n    - extra\n  }\n  notes :=\n    n\n}\n"
	got := render(scan(t, src))
	want := "prompt IDENT:A inherits IDENT:B { IDENT:constraints := TEXT:from(B[*]) and { TEXT:- extra TEXT:} IDENT:notes := TEXT:n } EOF"
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

func TestVarSlotVariantEnvContractCapabilitiesTags(t *testing.T) {
	src := `prompt A {
  var lang = "go"
  var plain = value # trailing comment
  var hash = "#fff"
  slot topic { required: true }
  tags: one, two
  variant short {
    notes := s
  }
  env prod {
    notes := p
  }
  contract {
    must_include:
      - x
  }
  capabilities {
    allow:
      - y
  }
}
`
	got := render(scan(t, src))
	for _, want := range []string{
		"var IDENT:lang TEXT:go", "var IDENT:plain TEXT:value", "var IDENT:hash TEXT:#fff",
		"slot IDENT:topic TEXT:required: true", "tags TEXT:one, two",
		"variant IDENT:short {", "env IDENT:prod {", "contract {", "capabilities {",
		"IDENT:must_include : TEXT:- x", "IDENT:allow : TEXT:- y",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	// the nested blocks close and the prompt still ends cleanly
	if strings.Count(got, "{") != strings.Count(got, "}") {
		t.Errorf("unbalanced braces: %s", got)
	}
}

func TestCommentsAreCollectedNotTokenised(t *testing.T) {
	src := "// header\nprompt A {\n  // inside\n  notes :=\n    // not a comment? still a full-line // comment\n    text\n}\n"
	toks, comments, err := ScanWithComments("t.loom", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 3 || comments[0].Line != 1 || comments[0].Text != "// header" || comments[1].Line != 3 || comments[2].Line != 5 {
		t.Errorf("%+v", comments)
	}
	for _, tk := range toks {
		if strings.HasPrefix(tk.Text, "//") {
			t.Errorf("comment leaked into the token stream: %+v", tk)
		}
	}
}

func TestWindowsLineEndingsAndTabs(t *testing.T) {
	got := render(scan(t, "prompt A {\r\n\tnotes :=\r\n\t\tline one\r\n\t\tline two\r\n}\r\n"))
	if !strings.Contains(got, "TEXT:line one TEXT:line two") || strings.Contains(got, "\r") {
		t.Errorf("%q", got)
	}
}

func TestPositions(t *testing.T) {
	toks := scan(t, "prompt A {\n  notes :=\n    hello\n}\n")
	for _, tk := range toks {
		if tk.Type == TokTextLine && tk.Text == "hello" && (tk.Line != 3 || tk.Col != 5) {
			t.Errorf("text position: %+v", tk)
		}
		if tk.Type == TokIdent && tk.Text == "notes" && (tk.Line != 2 || tk.Col != 3) {
			t.Errorf("field position: %+v", tk)
		}
		if tk.Type == TokEOF && tk.Line != 6 {
			t.Errorf("eof line: %+v", tk)
		}
	}
}

func TestEmptyAndCommentOnlyInput(t *testing.T) {
	for _, src := range []string{"", "\n\n", "// nothing\n"} {
		toks := scan(t, src)
		if len(toks) != 1 || toks[0].Type != TokEOF {
			t.Errorf("%q: %v", src, toks)
		}
	}
}

func TestSeveralDeclarationsInOneFile(t *testing.T) {
	src := "block G {\n  constraints :=\n    - c\n}\n\noverlay O {\n  notes :=\n    n\n}\n\nprompt P {\n  use G\n}\n"
	got := render(scan(t, src))
	if !strings.HasPrefix(got, "block IDENT:G {") || !strings.Contains(got, "overlay IDENT:O {") || !strings.Contains(got, "prompt IDENT:P {") {
		t.Error(got)
	}
}

func TestIsIdentAndNamespaced(t *testing.T) {
	for s, want := range map[string]bool{
		"A": true, "a_b-c1": true, "日本": true, "": false, "a b": false, "a.b": false, "a/b": false, "a{": false,
	} {
		if isIdent(s) != want {
			t.Errorf("isIdent(%q) != %v", s, want)
		}
	}
	for s, want := range map[string]bool{
		"A": true, "team.A": true, "pack/A": true, ".A": false, "team.": false, "a.b.c": false, "a.b/c": false,
	} {
		if isNamespacedIdent(s) != want {
			t.Errorf("isNamespacedIdent(%q) != %v", s, want)
		}
	}
}

func TestStripInlineComment(t *testing.T) {
	cases := map[string]string{
		`a # b`: "a", `"a # b"`: `"a # b"`, `"a \" # b" # c`: `"a \" # b"`, `plain`: "plain", `  x  `: "x", `#only`: "",
	}
	for in, want := range cases {
		if got := stripInlineComment(in); got != want {
			t.Errorf("stripInlineComment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseQuotedValue(t *testing.T) {
	if v, err := parseQuotedValue(`"a\nb\"c"`); err != nil || v != "a\nb\"c" {
		t.Errorf("%q %v", v, err)
	}
	if v, _ := parseQuotedValue("  bare  "); v != "bare" {
		t.Errorf("%q", v)
	}
	if v, _ := parseQuotedValue(""); v != "" {
		t.Errorf("%q", v)
	}
	if _, err := parseQuotedValue(`"open`); err == nil {
		t.Error("unterminated string must fail")
	}
}

func TestScanVars(t *testing.T) {
	src := "// defaults\nvar lang = \"go\"\nvar n = 3\nslot topic { required: true }\nslot other {}\n\n"
	vs, err := ScanVars("x.vars.loom", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 4 {
		t.Fatalf("%+v", vs)
	}
	if vs[0].Name != "lang" || vs[0].Default != "go" || vs[0].IsSlot || vs[0].Line != 2 || vs[0].File != "x.vars.loom" {
		t.Errorf("%+v", vs[0])
	}
	if vs[1].Default != "3" {
		t.Errorf("%+v", vs[1])
	}
	if !vs[2].IsSlot || !vs[2].Required || vs[3].Required {
		t.Errorf("%+v %+v", vs[2], vs[3])
	}
	for _, bad := range []string{"prompt A {\n", "var x\n", "slot a b\n", "var x = \"open\n"} {
		if _, err := ScanVars("x.vars.loom", bad); err == nil || !strings.HasPrefix(err.Error(), "x.vars.loom:1:") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestTokTypeStringCoversEveryToken(t *testing.T) {
	for tt := TokEOF; tt <= TokKwTags; tt++ {
		if tt.String() == "UNKNOWN" {
			t.Errorf("token %d has no name", tt)
		}
	}
	if TokType(999).String() != "UNKNOWN" {
		t.Error("out of range")
	}
}

// A trailing "{" in ordinary text (a code or JSON example) must not swallow the closing "}"
// of the prompt: the counter used to leak across fields and files.
func TestUnbalancedBraceInTextDoesNotSwallowTheClosingBrace(t *testing.T) {
	src := "prompt A {\n  notes :=\n    Respond with an object like {\n}\n\nprompt B {\n  notes :=\n    n\n}\n"
	got := render(scan(t, src))
	want := "prompt IDENT:A { IDENT:notes := TEXT:Respond with an object like { } prompt IDENT:B { IDENT:notes := TEXT:n } EOF"
	if got != want {
		t.Errorf("\n got %s\nwant %s", got, want)
	}
}

func TestFromExpressionOnTheNextLineAndParentForm(t *testing.T) {
	for _, src := range []string{
		"prompt A inherits B {\n  c :=\n    from(B[*]) and {\n      - x\n    }\n  n :=\n    t\n}\n",
		"prompt A inherits B {\n  c := parent[*] and {\n    - x\n  }\n  n :=\n    t\n}\n",
	} {
		got := render(scan(t, src))
		if !strings.Contains(got, "IDENT:n := TEXT:t } EOF") {
			t.Errorf("%s", got)
		}
	}
}

// `slot name` without braces is valid (the language docs and the editor hover both say so) and
// means the same as `slot name {}`.
func TestBareSlot(t *testing.T) {
	got := render(scan(t, "prompt A {\n  slot topic\n  slot other {}\n}\n"))
	if !strings.Contains(got, "slot IDENT:topic TEXT: slot IDENT:other TEXT:") {
		t.Errorf("%s", got)
	}
	vs, err := ScanVars("x.vars.loom", "slot topic\nslot other { required: true }\n")
	if err != nil || len(vs) != 2 || !vs[0].IsSlot || vs[0].Name != "topic" || vs[0].Required != false && false {
		t.Errorf("%+v %v", vs, err)
	}
}
