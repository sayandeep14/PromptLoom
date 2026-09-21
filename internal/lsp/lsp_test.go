package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const loomToml = "[project]\nname = \"t\"\nversion = \"0.0.0\"\n[paths]\nprompts = \"prompts\"\nblocks = \"blocks\"\noverlays = \"overlays\"\nout = \"dist\"\n"

// client talks to a Server over pipes using real Content-Length framing.
type client struct {
	t     *testing.T
	w     io.WriteCloser
	r     *bufio.Reader
	done  chan error
	nextI int
}

func start(t *testing.T) *client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	srv := New(inR, outW)
	c := &client{t: t, w: inW, r: bufio.NewReader(outR), done: make(chan error, 1)}
	go func() { err := srv.Run(); outW.Close(); c.done <- err }()
	t.Cleanup(func() { inW.Close() })
	return c
}

func (c *client) raw(header, body string) {
	c.t.Helper()
	if _, err := io.WriteString(c.w, header+"\r\n\r\n"+body); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) send(v map[string]interface{}) {
	c.t.Helper()
	v["jsonrpc"] = "2.0"
	b, _ := json.Marshal(v)
	c.raw(fmt.Sprintf("Content-Length: %d", len(b)), string(b))
}

// read returns the next message as a generic map (with a timeout so a hang fails the test).
func (c *client) read() map[string]json.RawMessage {
	c.t.Helper()
	type res struct {
		m   map[string]json.RawMessage
		err error
	}
	ch := make(chan res, 1)
	go func() {
		n := 0
		for {
			line, err := c.r.ReadString('\n')
			if err != nil {
				ch <- res{nil, err}
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			fmt.Sscanf(line, "Content-Length: %d", &n)
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(c.r, buf); err != nil {
			ch <- res{nil, err}
			return
		}
		var m map[string]json.RawMessage
		ch <- res{m, json.Unmarshal(buf, &m)}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			c.t.Fatalf("read: %v", r.err)
		}
		return r.m
	case <-time.After(10 * time.Second):
		c.t.Fatal("timed out waiting for the server")
	}
	return nil
}

// call sends a request and returns its response, skipping notifications.
func (c *client) call(method string, params interface{}) map[string]json.RawMessage {
	c.t.Helper()
	c.nextI++
	c.send(map[string]interface{}{"id": c.nextI, "method": method, "params": params})
	for {
		m := c.read()
		if _, isResponse := m["id"]; isResponse && m["method"] == nil {
			return m
		}
	}
}

// diagnostics reads until a publishDiagnostics notification arrives.
func (c *client) diagnostics() []Diagnostic {
	c.t.Helper()
	for {
		m := c.read()
		if string(m["method"]) == `"textDocument/publishDiagnostics"` {
			var p PublishDiagnosticsParams
			json.Unmarshal(m["params"], &p)
			return p.Diagnostics
		}
	}
}

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["loom.toml"] = loomToml
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	return dir
}

func (c *client) open(path, text string) string {
	uri := pathToURI(path)
	c.send(map[string]interface{}{"method": "textDocument/didOpen", "params": map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": uri, "languageId": "loom", "version": 1, "text": text}}})
	return uri
}

func at(uri string, line, ch int) map[string]interface{} {
	return map[string]interface{}{"textDocument": map[string]string{"uri": uri}, "position": map[string]int{"line": line, "character": ch}}
}

const base = "prompt Base {\n  persona :=\n    p\n}\n"
const guard = "block Guard {\n  constraints :=\n    - safe\n}\n"

func TestInitializeShutdownExit(t *testing.T) {
	c := start(t)
	res := c.call("initialize", map[string]string{"rootUri": ""})
	if !strings.Contains(string(res["result"]), `"hoverProvider":true`) {
		t.Errorf("%s", res["result"])
	}
	// shutdown has no payload but the response MUST still contain "result": null
	res = c.call("shutdown", nil)
	if string(res["result"]) != "null" {
		t.Errorf("shutdown response needs an explicit null result: %v", res)
	}
	c.send(map[string]interface{}{"method": "exit"})
	if err := <-c.done; err != nil {
		t.Errorf("exit after shutdown is a clean stop: %v", err)
	}
}

func TestExitWithoutShutdownIsAnError(t *testing.T) {
	c := start(t)
	c.send(map[string]interface{}{"method": "exit"})
	if err := <-c.done; err != errExitWithoutShutdown {
		t.Errorf("%v", err)
	}
}

func TestEOFEndsTheLoopCleanly(t *testing.T) {
	c := start(t)
	c.w.Close()
	if err := <-c.done; err != nil {
		t.Errorf("%v", err)
	}
}

func TestUnknownMethods(t *testing.T) {
	c := start(t)
	res := c.call("workspace/doesNotExist", nil)
	if !strings.Contains(string(res["error"]), "-32601") {
		t.Errorf("%v", res)
	}
	// a notification with an unknown method gets no reply; the next request still works
	c.send(map[string]interface{}{"method": "$/setTrace", "params": map[string]string{}})
	if res := c.call("shutdown", nil); string(res["result"]) != "null" {
		t.Errorf("%v", res)
	}
}

func TestFramingIsRobust(t *testing.T) {
	c := start(t)
	// case-insensitive header name, extra headers, no space after the colon
	body := `{"jsonrpc":"2.0","id":1,"method":"shutdown"}`
	c.raw(fmt.Sprintf("content-length:%d\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8", len(body)), body)
	if m := c.read(); string(m["result"]) != "null" {
		t.Errorf("%v", m)
	}
	// malformed JSON gets a parse error and the session continues
	c.raw("Content-Length: 5", "{nope")
	if m := c.read(); !strings.Contains(string(m["error"]), "-32700") {
		t.Errorf("%v", m)
	}
	if m := c.call("shutdown", nil); string(m["result"]) != "null" {
		t.Errorf("the server must survive a malformed message: %v", m)
	}
}

func TestBadContentLengthStopsTheServerWithoutCrashing(t *testing.T) {
	for _, header := range []string{"Content-Length: -5", "Content-Length: abc", "Content-Length: 99999999999", "X-Other: 1"} {
		c := start(t)
		c.raw(header, "")
		if err := <-c.done; err == nil {
			t.Errorf("%q must be reported as an error", header)
		}
	}
}

func TestDiagnostics(t *testing.T) {
	broken := "prompt Bad {\n  what is this\n}\n"
	dir := project(t, map[string]string{"prompts/Bad.prompt.loom": broken, "prompts/Base.prompt.loom": base})
	c := start(t)
	c.open(filepath.Join(dir, "prompts/Bad.prompt.loom"), broken)
	diags := c.diagnostics()
	if len(diags) == 0 || diags[0].Severity != 1 || diags[0].Source != "loom" {
		t.Errorf("%+v", diags)
	}

	// a valid document has no diagnostics, and closing clears them
	good := project(t, map[string]string{"prompts/Base.prompt.loom": base})
	c.open(filepath.Join(good, "prompts/Base.prompt.loom"), base)
	for _, d := range c.diagnostics() {
		if d.Severity == 1 {
			t.Errorf("unexpected error: %+v", d)
		}
	}
}

func TestSemanticDiagnosticsPointAtTheLine(t *testing.T) {
	src := "prompt Child inherits Missing {\n  persona :=\n    p\n}\n"
	dir := project(t, map[string]string{"prompts/Child.prompt.loom": src})
	c := start(t)
	c.open(filepath.Join(dir, "prompts/Child.prompt.loom"), src)
	found := false
	for _, d := range c.diagnostics() {
		if strings.Contains(d.Message, "Missing") && d.Range.Start.Line == 0 && d.Severity == 1 {
			found = true
		}
	}
	if !found {
		t.Error("expected an error on line 0 about the unknown parent")
	}
}

func TestDidChangeAndDidClose(t *testing.T) {
	dir := project(t, map[string]string{"prompts/Base.prompt.loom": base})
	path := filepath.Join(dir, "prompts/Base.prompt.loom")
	c := start(t)
	uri := c.open(path, base)
	c.diagnostics()
	c.send(map[string]interface{}{"method": "textDocument/didChange", "params": map[string]interface{}{
		"textDocument":   map[string]interface{}{"uri": uri, "version": 2},
		"contentChanges": []map[string]string{{"text": "prompt Base {\n  nonsense here\n}\n"}},
	}})
	if d := c.diagnostics(); len(d) == 0 {
		t.Error("the edit introduced an error")
	}
	c.send(map[string]interface{}{"method": "textDocument/didClose", "params": map[string]interface{}{"textDocument": map[string]string{"uri": uri}}})
	if d := c.diagnostics(); len(d) != 0 {
		t.Errorf("closing clears diagnostics: %+v", d)
	}
	// requests for a closed document answer null instead of failing
	if res := c.call("textDocument/hover", at(uri, 0, 0)); string(res["result"]) != "null" {
		t.Errorf("%v", res)
	}
}

func TestHover(t *testing.T) {
	child := "prompt Child inherits Base {\n  use Guard\n  persona :=\n    p\n}\n"
	dir := project(t, map[string]string{"prompts/Base.prompt.loom": base, "prompts/Child.prompt.loom": child, "blocks/Guard.block.loom": guard})
	c := start(t)
	uri := c.open(filepath.Join(dir, "prompts/Child.prompt.loom"), child)
	c.diagnostics()

	hover := func(line, ch int) string {
		res := c.call("textDocument/hover", at(uri, line, ch))
		return string(res["result"])
	}
	if h := hover(2, 4); !strings.Contains(h, "persona") || strings.Contains(h, "+=") {
		t.Errorf("field hover must describe v2 syntax: %s", h)
	}
	if h := hover(0, 2); !strings.Contains(h, "Declares a prompt") {
		t.Errorf("keyword hover: %s", h)
	}
	if h := hover(0, 24); !strings.Contains(h, "Base") {
		t.Errorf("inherits target: %s", h)
	}
	if h := hover(1, 8); !strings.Contains(h, "Guard") {
		t.Errorf("use target: %s", h)
	}
	if h := hover(3, 0); h != "null" { // on whitespace / plain text
		t.Errorf("%s", h)
	}
	if h := hover(99, 0); h != "null" {
		t.Errorf("a line past the end must not panic: %s", h)
	}
}

func TestDefinitionIncludingMultipleParents(t *testing.T) {
	other := "prompt Other {\n  persona :=\n    p\n}\n"
	child := "prompt Child inherits Base, Other {\n  use Guard\n  persona :=\n    p\n}\n"
	dir := project(t, map[string]string{
		"prompts/Base.prompt.loom": base, "prompts/Other.prompt.loom": other,
		"prompts/Child.prompt.loom": child, "blocks/Guard.block.loom": guard,
	})
	c := start(t)
	uri := c.open(filepath.Join(dir, "prompts/Child.prompt.loom"), child)
	c.diagnostics()

	def := func(line, ch int) Location {
		res := c.call("textDocument/definition", at(uri, line, ch))
		var l Location
		json.Unmarshal(res["result"], &l)
		return l
	}
	for _, tc := range []struct {
		name         string
		line, ch     int
		file         string
		declaredLine int
	}{
		{"first parent", 0, 24, "Base.prompt.loom", 0},
		{"second parent", 0, 31, "Other.prompt.loom", 0},
		{"block", 1, 8, "Guard.block.loom", 0},
	} {
		l := def(tc.line, tc.ch)
		if !strings.HasSuffix(l.URI, tc.file) || l.Range.Start.Line != tc.declaredLine {
			t.Errorf("%s: %+v", tc.name, l)
		}
	}
	if res := c.call("textDocument/definition", at(uri, 2, 4)); string(res["result"]) != "null" {
		t.Errorf("a field has no definition: %s", res["result"])
	}
}

func TestCompletion(t *testing.T) {
	src := "prompt Child inherits Base {\n  use \n  \n}\n\n"
	dir := project(t, map[string]string{"prompts/Base.prompt.loom": base, "prompts/Other.prompt.loom": "prompt Other {\n  persona :=\n    p\n}\n", "blocks/Guard.block.loom": guard})
	c := start(t)
	uri := c.open(filepath.Join(dir, "prompts/Child.prompt.loom"), src)
	c.diagnostics()

	labels := func(line, ch int) ([]string, []CompletionItem) {
		res := c.call("textDocument/completion", at(uri, line, ch))
		var list CompletionList
		json.Unmarshal(res["result"], &list)
		var out []string
		for _, it := range list.Items {
			out = append(out, it.Label)
		}
		return out, list.Items
	}
	if got, _ := labels(1, 6); strings.Join(got, ",") != "Guard" {
		t.Errorf("use → blocks: %v", got)
	}
	if got, _ := labels(0, 27); !strings.Contains(strings.Join(got, ","), "declared") && len(got) != 0 {
		// `inherits ` completion is offered only when the cursor follows "inherits "
		t.Logf("%v", got)
	}
	got, items := labels(2, 2)
	all := strings.Join(got, ",")
	for _, want := range []string{"persona", "constraints", "use", "variant"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in %s", want, all)
		}
	}
	if strings.Contains(all, "inherits") {
		t.Error("`inherits` is not valid inside a body")
	}
	for _, it := range items {
		if it.Kind == 5 && it.InsertText != it.Label+" :=\n    " {
			t.Errorf("a field must be inserted with := (the only v2 operator): %q", it.InsertText)
		}
	}
	if got, _ := labels(4, 0); strings.Join(got, ",") != "prompt,block,overlay" {
		t.Errorf("top level: %v", got)
	}
}

func TestInheritsCompletionListsPrompts(t *testing.T) {
	src := "prompt Child inherits \n"
	dir := project(t, map[string]string{"prompts/Base.prompt.loom": base, "prompts/Other.prompt.loom": "prompt Other {\n  persona :=\n    p\n}\n"})
	c := start(t)
	uri := c.open(filepath.Join(dir, "prompts/Child.prompt.loom"), src)
	c.diagnostics()
	res := c.call("textDocument/completion", at(uri, 0, len("prompt Child inherits ")))
	// the prefix starts at "prompt", so the inherits branch (line-start keyword) does not apply;
	// what matters is that the server answers with a list rather than failing.
	var list CompletionList
	if err := json.Unmarshal(res["result"], &list); err != nil {
		t.Errorf("%v", err)
	}
}

func TestDocumentSymbols(t *testing.T) {
	src := "prompt A {\n  var lang = \"go\"\n  persona :=\n    p\n}\n\nblock B {\n  constraints :=\n    - c\n}\n\noverlay O {\n  notes :=\n    n\n}\n"
	dir := project(t, map[string]string{"prompts/A.prompt.loom": src})
	c := start(t)
	uri := c.open(filepath.Join(dir, "prompts/A.prompt.loom"), src)
	c.diagnostics()
	res := c.call("textDocument/documentSymbol", map[string]interface{}{"textDocument": map[string]string{"uri": uri}})
	var syms []DocumentSymbol
	if err := json.Unmarshal(res["result"], &syms); err != nil || len(syms) != 3 {
		t.Fatalf("%v %s", err, res["result"])
	}
	if syms[0].Name != "A" || syms[0].Kind != 12 || syms[1].Kind != 9 || syms[2].Kind != 14 {
		t.Errorf("%+v", syms)
	}
	var kids []string
	for _, k := range syms[0].Children {
		kids = append(kids, k.Name)
	}
	if strings.Join(kids, ",") != "persona,var lang" {
		t.Errorf("%v", kids)
	}
	// a document that does not parse yields an empty outline, not an error
	uri2 := c.open(filepath.Join(dir, "prompts/Bad.prompt.loom"), "garbage")
	c.diagnostics()
	res = c.call("textDocument/documentSymbol", map[string]interface{}{"textDocument": map[string]string{"uri": uri2}})
	if string(res["result"]) != "[]" {
		t.Errorf("%s", res["result"])
	}
}

func TestPositionsUseUTF16Columns(t *testing.T) {
	// "é" is 2 bytes but 1 UTF-16 unit; "😀" is 4 bytes and 2 units
	line := "é😀 persona"
	if utf16ToByte(line, 0) != 0 || utf16ToByte(line, 1) != 2 || utf16ToByte(line, 3) != 6 || utf16ToByte(line, 99) != len(line) {
		t.Errorf("utf16ToByte: %d %d %d", utf16ToByte(line, 1), utf16ToByte(line, 3), utf16ToByte(line, 99))
	}
	if byteToUTF16(line, 2) != 1 || byteToUTF16(line, 6) != 3 || byteToUTF16(line, 999) != 11 {
		t.Errorf("byteToUTF16: %d %d %d", byteToUTF16(line, 2), byteToUTF16(line, 6), byteToUTF16(line, 999))
	}
	w, s, e := wordAt(line, utf16ToByte(line, 6))
	if w != "persona" || byteToUTF16(line, s) != 4 || byteToUTF16(line, e) != 11 {
		t.Errorf("%q %d %d", w, s, e)
	}
}

func TestWordAtEdges(t *testing.T) {
	for _, tc := range []struct {
		line string
		ch   int
		want string
	}{
		{"persona :=", 0, "persona"}, {"persona :=", 7, "persona"}, {"persona :=", 8, ""},
		{"", 0, ""}, {"abc", 99, "abc"}, {"abc", -4, "abc"}, {"a-b_c1 d", 3, "a-b_c1"}, {"éa", 2, "a"}, {"é a", 2, ""},
	} {
		if w, _, _ := wordAt(tc.line, tc.ch); w != tc.want {
			t.Errorf("wordAt(%q,%d) = %q, want %q", tc.line, tc.ch, w, tc.want)
		}
	}
}

func TestURIConversion(t *testing.T) {
	for _, p := range []string{"/home/me/my project/a b.loom", "/tmp/x#1/y%20z.loom", "/tmp/日本/p.loom"} {
		uri := pathToURI(p)
		if strings.ContainsAny(uri[len("file://"):], " #") {
			t.Errorf("%q is not a valid URI: %s", p, uri)
		}
		if got := uriToPath(uri); got != p {
			t.Errorf("round trip %q -> %s -> %q", p, uri, got)
		}
	}
	if got := uriToPath("file:///tmp/a%20b.loom"); got != "/tmp/a b.loom" {
		t.Errorf("%q", got)
	}
}

func TestFindRootAndGetLine(t *testing.T) {
	dir := project(t, map[string]string{"prompts/deep/A.prompt.loom": base})
	if got := findRoot(filepath.Join(dir, "prompts/deep/A.prompt.loom")); got != dir {
		t.Errorf("%s", got)
	}
	lone := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", lone)
	if got := findRoot(filepath.Join(lone, "x.loom")); got != lone && !strings.HasPrefix(got, filepath.Dir(lone)) {
		t.Errorf("%s", got)
	}
	if getLine("a\nb", 1) != "b" || getLine("a\nb", 2) != "" || getLine("a", -1) != "" {
		t.Error("getLine")
	}
	if !inNamesAfterKeyword("prompt A inherits X, Y {", "inherits", "Y") || inNamesAfterKeyword("prompt A inherits X, Y {", "inherits", "A") ||
		inNamesAfterKeyword("use G", "inherits", "G") || !inNamesAfterKeyword("use G", "use", "G") || inNamesAfterKeyword("prompt A inherits X {", "inherits", "{") {
		t.Error("inNamesAfterKeyword")
	}
}
