package lsp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/loader"
	"github.com/sayandeepgiri/promptloom/internal/parser"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
	"github.com/sayandeepgiri/promptloom/internal/validate"
)

// ---- initialize ----

func (s *Server) handleInitialize(id interface{}, p InitializeParams) {
	s.respond(id, InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync:       1, // full sync
			HoverProvider:          true,
			DefinitionProvider:     true,
			DocumentSymbolProvider: true,
			CompletionProvider: &CompletionOptions{
				TriggerCharacters: []string{" ", "\t"},
			},
		},
		ServerInfo: &ServerInfo{Name: "loom-lsp", Version: "0.1.0"},
	})
}

// ---- document lifecycle ----

func (s *Server) handleDidOpen(p DidOpenTextDocumentParams) {
	uri := p.TextDocument.URI
	s.docs[uri] = p.TextDocument.Text
	s.roots[uri] = findRoot(uriToPath(uri))
	s.publishDiagnostics(uri)
}

func (s *Server) handleDidChange(p DidChangeTextDocumentParams) {
	uri := p.TextDocument.URI
	if len(p.ContentChanges) > 0 {
		s.docs[uri] = p.ContentChanges[len(p.ContentChanges)-1].Text
	}
	s.publishDiagnostics(uri)
}

func (s *Server) handleDidClose(p DidCloseTextDocumentParams) {
	uri := p.TextDocument.URI
	delete(s.docs, uri)
	delete(s.roots, uri)
	// Clear diagnostics on close.
	s.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: []Diagnostic{},
	})
}

// ---- diagnostics ----

func (s *Server) publishDiagnostics(uri string) {
	path := uriToPath(uri)
	root := s.roots[uri]

	var diags []Diagnostic

	// Parse errors from in-memory content.
	if text, ok := s.docs[uri]; ok {
		_, parseErr := parser.Parse(path, text)
		if parseErr != nil {
			diags = append(diags, Diagnostic{
				Range:    LSPRange{Start: Position{0, 0}, End: Position{0, 0}},
				Severity: 1,
				Message:  parseErr.Error(),
				Source:   "loom",
			})
		}
	}

	// Semantic diagnostics from the full project registry (on-disk state).
	if root != "" {
		reg, cfg, err := loader.Load(root)
		if err == nil {
			for _, d := range validate.Validate(reg, cfg) {
				if !strings.Contains(d.Pos.File, filepath.Base(path)) &&
					d.Pos.File != path {
					continue // only emit diagnostics for the current file
				}
				sev := 1
				if d.Sev == validate.Warning {
					sev = 2
				}
				line := d.Pos.Line - 1
				if line < 0 {
					line = 0
				}
				diags = append(diags, Diagnostic{
					Range: LSPRange{
						Start: Position{line, 0},
						End:   Position{line, 200},
					},
					Severity: sev,
					Message:  d.Message,
					Source:   "loom",
				})
			}
		}
	}

	s.notify("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	})
}

// ---- hover ----

var fieldDocs = map[string]string{
	"summary":      "**summary** *(scalar)* — One-line description of the prompt's purpose.\nOperators: `:`  `:=`  `+=`",
	"persona":      "**persona** *(scalar)* — Defines the AI's role and voice.\nOperators: `:`  `:=`  `+=`",
	"context":      "**context** *(scalar)* — Background the AI should know before acting.\nOperators: `:`  `:=`  `+=`",
	"objective":    "**objective** *(scalar)* — The goal the AI is trying to achieve.\nOperators: `:`  `:=`  `+=`",
	"notes":        "**notes** *(scalar)* — Free-form remarks (not included in rendered output by default).\nOperators: `:`  `:=`  `+=`",
	"instructions": "**instructions** *(list)* — Ordered steps the AI should follow.\nOperators: `:`  `:=`  `+=`  `-=`",
	"constraints":  "**constraints** *(list)* — Hard rules the AI must not violate.\nOperators: `:`  `:=`  `+=`  `-=`",
	"examples":     "**examples** *(list)* — Input/output examples to guide behaviour.\nOperators: `:`  `:=`  `+=`  `-=`",
	"format":       "**format** *(list)* — Expected output structure.\nOperators: `:`  `:=`  `+=`  `-=`",
}

var keywordDocs = map[string]string{
	"prompt":       "Declares a prompt. Syntax: `prompt Name { ... }` or `prompt Name inherits Parent { ... }`",
	"block":        "Declares a reusable instruction block. Syntax: `block Name { ... }`",
	"overlay":      "Declares an overlay that can be applied at render time with `--overlay Name`.",
	"inherits":     "Sets the parent prompt for inheritance. The child inherits all parent fields.",
	"use":          "Mixes a block's fields into this prompt. Syntax: `use BlockName`",
	"var":          "Declares a render variable with an optional default. Syntax: `var name = \"default\"`",
	"slot":         "Declares a required input slot that prompts interactively. Syntax: `slot name` or `slot name { secret: true }`",
	"variant":      "Declares an alternative field set activated with `--variant Name`.",
	"env":          "Declares environment-specific field overrides. Syntax: `env prod { ... }`",
	"contract":     "Declares output contract assertions checked by `loom test`.",
	"capabilities": "Declares allowed and forbidden capabilities, used for MCP manifest generation.",
}

func (s *Server) handleHover(id interface{}, p TextDocumentPositionParams) {
	uri := p.TextDocument.URI
	text, ok := s.docs[uri]
	if !ok {
		s.respond(id, nil)
		return
	}

	line := getLine(text, p.Position.Line)
	word, wStart, wEnd := wordAt(line, p.Position.Character)
	if word == "" {
		s.respond(id, nil)
		return
	}

	wordRange := &LSPRange{
		Start: Position{p.Position.Line, wStart},
		End:   Position{p.Position.Line, wEnd},
	}

	// Check if it's a known field name.
	if doc, ok := fieldDocs[strings.ToLower(word)]; ok {
		s.respond(id, Hover{
			Contents: MarkupContent{Kind: "markdown", Value: doc},
			Range:    wordRange,
		})
		return
	}

	// Check if it's a keyword.
	if doc, ok := keywordDocs[strings.ToLower(word)]; ok {
		s.respond(id, Hover{
			Contents: MarkupContent{Kind: "markdown", Value: doc},
			Range:    wordRange,
		})
		return
	}

	// Check if cursor is on a name after `inherits` or `use`.
	trimmed := strings.TrimSpace(line)
	root := s.roots[uri]

	if root != "" {
		reg, _, err := loader.Load(root)
		if err == nil {
			if after, ok := wordAfterKeyword(trimmed, "inherits"); ok && after == word {
				if node, ok := reg.LookupPrompt(word); ok {
					s.respond(id, Hover{
						Contents: MarkupContent{
							Kind:  "markdown",
							Value: formatNodeHover(node, reg),
						},
						Range: wordRange,
					})
					return
				}
			}
			if after, ok := wordAfterKeyword(trimmed, "use"); ok && after == word {
				if node, ok := reg.LookupBlock(word); ok {
					s.respond(id, Hover{
						Contents: MarkupContent{
							Kind:  "markdown",
							Value: formatNodeHover(node, reg),
						},
						Range: wordRange,
					})
					return
				}
			}
			// Check if it's a prompt name at the declaration line.
			if strings.HasPrefix(trimmed, "prompt "+word) || strings.HasPrefix(trimmed, "block "+word) {
				if _, ok := reg.LookupPrompt(word); ok {
					rp, err2 := resolve.Resolve(word, reg)
					if err2 == nil {
						s.respond(id, Hover{
							Contents: MarkupContent{Kind: "markdown", Value: formatResolvedHover(rp)},
							Range:    wordRange,
						})
						return
					}
				}
			}
		}
	}

	s.respond(id, nil)
}

func wordAfterKeyword(line, keyword string) (string, bool) {
	prefix := keyword + " "
	if !strings.Contains(line, prefix) {
		return "", false
	}
	rest := line[strings.Index(line, prefix)+len(prefix):]
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return "", false
	}
	return parts[0], true
}

func formatNodeHover(node *ast.Node, reg *registry.Registry) string {
	var b strings.Builder
	kind := "prompt"
	if node.Kind == ast.KindBlock {
		kind = "block"
	} else if node.Kind == ast.KindOverlay {
		kind = "overlay"
	}
	fmt.Fprintf(&b, "**%s** `%s`", kind, node.Name)
	if node.Parent != "" {
		fmt.Fprintf(&b, " *(inherits %s)*", node.Parent)
	}
	b.WriteString("\n\n")
	if len(node.Uses) > 0 {
		fmt.Fprintf(&b, "Uses: %s\n\n", strings.Join(node.Uses, ", "))
	}
	fields := make([]string, 0)
	for _, f := range node.Fields {
		fields = append(fields, f.FieldName)
	}
	if len(fields) > 0 {
		fmt.Fprintf(&b, "Fields: `%s`\n", strings.Join(fields, "`, `"))
	}
	return b.String()
}

func formatResolvedHover(rp *ast.ResolvedPrompt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**prompt** `%s`\n\n", rp.Name)
	if len(rp.InheritsChain) > 0 {
		fmt.Fprintf(&b, "Inherits: %s\n\n", strings.Join(rp.InheritsChain, " → "))
	}
	if rp.Summary != "" {
		fmt.Fprintf(&b, "**Summary:** %s\n\n", rp.Summary)
	}
	if rp.Persona != "" {
		fmt.Fprintf(&b, "**Persona:** %s\n", rp.Persona)
	}
	return b.String()
}

// ---- go-to-definition ----

func (s *Server) handleDefinition(id interface{}, p TextDocumentPositionParams) {
	uri := p.TextDocument.URI
	text, ok := s.docs[uri]
	if !ok {
		s.respond(id, nil)
		return
	}

	line := getLine(text, p.Position.Line)
	word, _, _ := wordAt(line, p.Position.Character)
	if word == "" {
		s.respond(id, nil)
		return
	}

	root := s.roots[uri]
	if root == "" {
		s.respond(id, nil)
		return
	}

	reg, _, err := loader.Load(root)
	if err != nil {
		s.respond(id, nil)
		return
	}

	trimmed := strings.TrimSpace(line)

	// `inherits Name` → jump to prompt declaration.
	if after, ok := wordAfterKeyword(trimmed, "inherits"); ok && after == word {
		if loc := nodeLocation(word, reg, true); loc != nil {
			s.respond(id, loc)
			return
		}
	}

	// `use Name` → jump to block declaration.
	if after, ok := wordAfterKeyword(trimmed, "use"); ok && after == word {
		if loc := nodeLocation(word, reg, false); loc != nil {
			s.respond(id, loc)
			return
		}
	}

	s.respond(id, nil)
}

func nodeLocation(name string, reg *registry.Registry, isPrompt bool) *Location {
	var node *ast.Node
	var ok bool
	if isPrompt {
		node, ok = reg.LookupPrompt(name)
	} else {
		node, ok = reg.LookupBlock(name)
	}
	if !ok {
		return nil
	}
	line := node.Pos.Line - 1
	if line < 0 {
		line = 0
	}
	return &Location{
		URI: pathToURI(node.Pos.File),
		Range: LSPRange{
			Start: Position{line, 0},
			End:   Position{line, len(name) + 10},
		},
	}
}

// ---- completion ----

var fieldNames = []string{
	"summary", "persona", "context", "objective",
	"instructions", "constraints", "examples", "format", "notes",
}

var bodyKeywords = []string{
	"inherits", "use", "var", "slot", "variant", "env", "contract", "capabilities",
}

func (s *Server) handleCompletion(id interface{}, p CompletionParams) {
	uri := p.TextDocument.URI
	text, ok := s.docs[uri]
	if !ok {
		s.respond(id, CompletionList{})
		return
	}

	line := getLine(text, p.Position.Line)
	prefix := ""
	if p.Position.Character <= len(line) {
		prefix = strings.TrimLeft(line[:p.Position.Character], " \t")
	}

	var items []CompletionItem

	// After `inherits ` → prompt names.
	if strings.HasPrefix(prefix, "inherits ") {
		root := s.roots[uri]
		if root != "" {
			reg, _, err := loader.Load(root)
			if err == nil {
				for _, node := range reg.Prompts() {
					items = append(items, CompletionItem{
						Label:  node.Name,
						Kind:   9, // Module
						Detail: "prompt",
					})
				}
			}
		}
		s.respond(id, CompletionList{Items: items})
		return
	}

	// After `use ` → block names.
	if strings.HasPrefix(prefix, "use ") {
		root := s.roots[uri]
		if root != "" {
			reg, _, err := loader.Load(root)
			if err == nil {
				for _, node := range reg.Blocks() {
					items = append(items, CompletionItem{
						Label:  node.Name,
						Kind:   9, // Module
						Detail: "block",
					})
				}
			}
		}
		s.respond(id, CompletionList{Items: items})
		return
	}

	// Inside a prompt/block body — offer field names.
	isIndented := strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")
	if isIndented {
		for _, f := range fieldNames {
			items = append(items, CompletionItem{
				Label:         f,
				Kind:          5, // Field
				Detail:        fieldDetail(f),
				InsertText:    f + ":\n    ",
				Documentation: fieldDocs[f],
			})
		}
		for _, kw := range bodyKeywords {
			items = append(items, CompletionItem{
				Label:  kw,
				Kind:   14, // Keyword
				Detail: "keyword",
			})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
		s.respond(id, CompletionList{Items: items})
		return
	}

	// Top-level — offer `prompt`, `block`, `overlay`.
	for _, kw := range []string{"prompt", "block", "overlay"} {
		items = append(items, CompletionItem{
			Label:      kw,
			Kind:       14,
			Detail:     "keyword",
			InsertText: kw + " Name {\n  \n}",
		})
	}
	s.respond(id, CompletionList{Items: items})
}

func fieldDetail(f string) string {
	listF := map[string]bool{"instructions": true, "constraints": true, "examples": true, "format": true}
	if listF[f] {
		return "list field"
	}
	return "scalar field"
}

// ---- documentSymbol ----

func (s *Server) handleDocumentSymbol(id interface{}, p DocumentSymbolParams) {
	uri := p.TextDocument.URI
	text, ok := s.docs[uri]
	if !ok {
		s.respond(id, []DocumentSymbol{})
		return
	}

	path := uriToPath(uri)
	nodes, err := parser.Parse(path, text)
	if err != nil || len(nodes) == 0 {
		s.respond(id, []DocumentSymbol{})
		return
	}

	var symbols []DocumentSymbol
	for _, node := range nodes {
		line := node.Pos.Line - 1
		if line < 0 {
			line = 0
		}
		kind := 12 // Function (prompt)
		if node.Kind == ast.KindBlock {
			kind = 9 // Module (block)
		} else if node.Kind == ast.KindOverlay {
			kind = 14 // Keyword (overlay)
		}

		sym := DocumentSymbol{
			Name: node.Name,
			Kind: kind,
			Range: LSPRange{
				Start: Position{line, 0},
				End:   Position{line + countLines(node), 1},
			},
			SelectionRange: LSPRange{
				Start: Position{line, 0},
				End:   Position{line, len(node.Name) + 10},
			},
		}

		// Add field children.
		for _, f := range node.Fields {
			fl := f.Pos.Line - 1
			if fl < 0 {
				fl = 0
			}
			sym.Children = append(sym.Children, DocumentSymbol{
				Name: f.FieldName,
				Kind: 5, // Field
				Range: LSPRange{
					Start: Position{fl, 0},
					End:   Position{fl, 100},
				},
				SelectionRange: LSPRange{
					Start: Position{fl, 2},
					End:   Position{fl, 2 + len(f.FieldName)},
				},
			})
		}

		// Add var/slot children.
		for _, v := range node.Vars {
			vl := v.Pos.Line - 1
			if vl < 0 {
				vl = 0
			}
			label := v.Name
			if v.IsSlot {
				label = "slot " + v.Name
			} else {
				label = "var " + v.Name
			}
			sym.Children = append(sym.Children, DocumentSymbol{
				Name: label,
				Kind: 6, // Variable
				Range: LSPRange{
					Start: Position{vl, 0},
					End:   Position{vl, 100},
				},
				SelectionRange: LSPRange{
					Start: Position{vl, 2},
					End:   Position{vl, 2 + len(v.Name)},
				},
			})
		}

		symbols = append(symbols, sym)
	}

	s.respond(id, symbols)
}

func countLines(node *ast.Node) int {
	maxLine := node.Pos.Line
	for _, f := range node.Fields {
		if f.Pos.Line > maxLine {
			maxLine = f.Pos.Line + len(f.Value)
		}
	}
	return maxLine - node.Pos.Line + 2
}
