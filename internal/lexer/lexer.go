// Package lexer tokenizes .loom DSL source files into a flat token stream.
// The scanner is line-aware and stateful: indented lines following a field
// declaration are emitted as TokTextLine tokens rather than being broken into
// structural tokens.
package lexer

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// TokType classifies a lexed token.
type TokType int

const (
	TokEOF TokType = iota
	TokKwPrompt
	TokKwBlock
	TokKwInherits
	TokKwUse
	TokIdent
	TokLBrace
	TokRBrace
	TokColon
	TokColonEq
	TokPlusEq
	TokMinusEq
	TokTextLine
	TokKwVar
	TokKwSlot
	TokKwVariant
	TokKwContract
	TokKwCapabilities
	TokKwOverlay
	TokKwEnv
	TokComma  // separates parent names in "inherits A, B, C"
	TokKwTags // `tags:` metadata declaration
)

func (t TokType) String() string {
	switch t {
	case TokEOF:
		return "EOF"
	case TokKwPrompt:
		return "prompt"
	case TokKwBlock:
		return "block"
	case TokKwInherits:
		return "inherits"
	case TokKwUse:
		return "use"
	case TokIdent:
		return "IDENT"
	case TokLBrace:
		return "{"
	case TokRBrace:
		return "}"
	case TokColon:
		return ":"
	case TokColonEq:
		return ":="
	case TokPlusEq:
		return "+="
	case TokMinusEq:
		return "-="
	case TokTextLine:
		return "TEXT"
	case TokKwVar:
		return "var"
	case TokKwSlot:
		return "slot"
	case TokKwVariant:
		return "variant"
	case TokKwContract:
		return "contract"
	case TokKwCapabilities:
		return "capabilities"
	case TokKwOverlay:
		return "overlay"
	case TokKwEnv:
		return "env"
	case TokComma:
		return ","
	case TokKwTags:
		return "tags"
	}
	return "UNKNOWN"
}

// Token is a lexed token with source position.
type Token struct {
	Type TokType
	Text string
	Line int
	Col  int
}

type scanState int

const (
	sTop scanState = iota
	sInBody
	sInFieldContent
	sInNestedBody
	sInNestedFieldContent
)

// Comment is a full-line `//` comment, kept so that formatting can preserve it.
type Comment struct {
	Line int    // 1-based source line
	Text string // the line trimmed of surrounding whitespace, including the leading //
}

type scanner struct {
	comments          []Comment
	filename          string
	lines             []string
	state             scanState
	fieldIndent       int
	nestedFieldIndent int
	tokens            []Token
	fromBlockDepth    int  // tracks open { ... } blocks inside a from() expression
	awaitFirst        bool // the field had no inline value; its first text line decides inFrom
	inFrom            bool // the current field is a from() expression (only those have structural braces)
}

// Scan tokenizes src and returns the full token stream, including a terminal TokEOF.
func Scan(filename, src string) ([]Token, error) {
	tokens, _, err := ScanWithComments(filename, src)
	return tokens, err
}

// ScanWithComments is Scan that also returns every full-line // comment, in source order.
func ScanWithComments(filename, src string) ([]Token, []Comment, error) {
	s := &scanner{
		filename: filename,
		lines:    strings.Split(src, "\n"),
	}
	if err := s.scan(); err != nil {
		return nil, nil, err
	}
	return s.tokens, s.comments, nil
}

func (s *scanner) errorf(line int, format string, args ...interface{}) error {
	return fmt.Errorf("%s:%d: %s", s.filename, line, fmt.Sprintf(format, args...))
}

func (s *scanner) emit(t Token) {
	s.tokens = append(s.tokens, t)
}

// indentOf returns the number of leading spaces in line, or -1 for a blank line.
func indentOf(line string) int {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return -1
}

// isIdent returns true if s is a valid DSL identifier.
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// isNamespacedIdent returns true for plain identifiers and for namespace-qualified
// identifiers of the form "slug.Name" (v2 dot notation) or legacy "pack/Name".
func isNamespacedIdent(s string) bool {
	if dot := strings.IndexByte(s, '.'); dot > 0 {
		return isIdent(s[:dot]) && isIdent(s[dot+1:])
	}
	if slash := strings.IndexByte(s, '/'); slash > 0 {
		return isIdent(s[:slash]) && isIdent(s[slash+1:])
	}
	return isIdent(s)
}

// parseFieldDecl recognises a field declaration in trimmed.
// Returns (name, op, inline, ok) where inline is any content on the same line
// after the operator (used for from() expressions and inline scalars).
func parseFieldDecl(trimmed string) (name, op, inline string, ok bool) {
	for _, candidate := range []string{":=", "+=", "-="} {
		if idx := strings.Index(trimmed, candidate); idx > 0 {
			n := strings.TrimRight(trimmed[:idx], " \t")
			if isIdent(n) {
				rest := strings.TrimSpace(trimmed[idx+len(candidate):])
				return n, candidate, rest, true
			}
		}
	}
	// bare colon: "fieldname:" with nothing after
	bare := strings.TrimRight(trimmed, " \t")
	if strings.HasSuffix(bare, ":") && !strings.Contains(bare[:len(bare)-1], ":") {
		n := bare[:len(bare)-1]
		if isIdent(n) {
			return n, ":", "", true
		}
	}
	return "", "", "", false
}

func stripInlineComment(s string) string {
	var b strings.Builder
	inString := false
	escaped := false

	for _, r := range s {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\':
			b.WriteRune(r)
			escaped = true
		case r == '"':
			inString = !inString
			b.WriteRune(r)
		case r == '#' && !inString:
			return strings.TrimSpace(b.String())
		default:
			b.WriteRune(r)
		}
	}

	return strings.TrimSpace(b.String())
}

func parseQuotedValue(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.HasPrefix(raw, "\"") {
		return raw, nil
	}
	val, err := strconv.Unquote(raw)
	if err != nil {
		return "", err
	}
	return val, nil
}

func parseVarLine(trimmed string) (name, def string, ok bool, err error) {
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "var"))
	if rest == trimmed {
		return "", "", false, nil
	}
	rest = stripInlineComment(rest)
	parts := strings.SplitN(rest, "=", 2)
	if len(parts) != 2 {
		return "", "", false, nil
	}
	name = strings.TrimSpace(parts[0])
	if !isIdent(name) {
		return "", "", false, nil
	}
	def, err = parseQuotedValue(parts[1])
	if err != nil {
		return "", "", false, err
	}
	return name, def, true, nil
}

func parseSlotLine(trimmed string) (name, metadata string, ok bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "slot"))
	if rest == trimmed {
		return "", "", false
	}
	open := strings.Index(rest, "{")
	close := strings.LastIndex(rest, "}")
	if open < 0 && close < 0 {
		// bare `slot name`: a required slot with no metadata, the same as `slot name {}`
		if !isIdent(rest) {
			return "", "", false
		}
		return rest, "", true
	}
	if open < 0 || close < 0 || close < open {
		return "", "", false
	}
	name = strings.TrimSpace(rest[:open])
	if !isIdent(name) {
		return "", "", false
	}
	metadata = strings.TrimSpace(rest[open+1 : close])
	return name, metadata, true
}

func (s *scanner) scan() error {
	for i, rawLine := range s.lines {
		lineNum := i + 1
		indent := indentOf(rawLine)
		trimmed := strings.TrimSpace(rawLine)

		if trimmed == "" {
			switch s.state {
			case sInFieldContent:
				s.state = sInBody
			case sInNestedFieldContent:
				s.state = sInNestedBody
			}
			continue
		}

		if strings.HasPrefix(trimmed, "//") {
			s.comments = append(s.comments, Comment{Line: lineNum, Text: trimmed})
			continue
		}

		switch s.state {
		case sTop:
			if err := s.scanTopLine(trimmed, lineNum); err != nil {
				return err
			}
		case sInBody:
			if err := s.scanBodyLine(indent, trimmed, lineNum); err != nil {
				return err
			}
		case sInFieldContent:
			// A closing brace that belongs to a from() "and { ... }" block.
			if trimmed == "}" && s.fromBlockDepth > 0 {
				s.emit(Token{Type: TokTextLine, Text: "}", Line: lineNum, Col: indent + 1})
				s.fromBlockDepth--
			} else if indent <= s.fieldIndent {
				s.state = sInBody
				if err := s.scanBodyLine(indent, trimmed, lineNum); err != nil {
					return err
				}
			} else {
				if s.awaitFirst {
					s.inFrom = looksLikeFrom(trimmed)
					s.awaitFirst = false
				}
				s.emit(Token{Type: TokTextLine, Text: trimmed, Line: lineNum, Col: indent + 1})
				// Track opening braces inside from() expressions only: in ordinary text a
				// trailing "{" (a code or JSON example) must not swallow the prompt's own "}".
				if s.inFrom && strings.HasSuffix(trimmed, "{") {
					s.fromBlockDepth++
				}
			}
		case sInNestedBody:
			if err := s.scanNestedBodyLine(indent, trimmed, lineNum); err != nil {
				return err
			}
		case sInNestedFieldContent:
			if indent <= s.nestedFieldIndent {
				s.state = sInNestedBody
				if err := s.scanNestedBodyLine(indent, trimmed, lineNum); err != nil {
					return err
				}
			} else {
				s.emit(Token{Type: TokTextLine, Text: trimmed, Line: lineNum, Col: indent + 1})
			}
		}
	}

	s.emit(Token{Type: TokEOF, Line: len(s.lines) + 1})
	return nil
}

func (s *scanner) scanTopLine(trimmed string, lineNum int) error {
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return nil
	}

	switch parts[0] {
	case "prompt":
		if parts[len(parts)-1] != "{" {
			return s.errorf(lineNum, "expected '{' at end of prompt declaration, got %q", parts[len(parts)-1])
		}
		s.emit(Token{Type: TokKwPrompt, Text: "prompt", Line: lineNum, Col: 1})

		if len(parts) < 3 {
			return s.errorf(lineNum, "invalid prompt declaration: %q", trimmed)
		}
		if !isIdent(parts[1]) {
			return s.errorf(lineNum, "expected prompt name, got %q", parts[1])
		}
		s.emit(Token{Type: TokIdent, Text: parts[1], Line: lineNum})

		if len(parts) == 3 {
			// "prompt Name {" — no inheritance
		} else if parts[2] == "inherits" {
			// "prompt Name inherits A, B, C {" — single or multiple parents
			s.emit(Token{Type: TokKwInherits, Text: "inherits", Line: lineNum})
			// Collect everything between "inherits" and the closing "{".
			rawParents := strings.Join(parts[3:len(parts)-1], " ")
			parentNames := strings.Split(rawParents, ",")
			for i, pn := range parentNames {
				pn = strings.TrimSpace(pn)
				if pn == "" {
					return s.errorf(lineNum, "empty parent name in inherits list")
				}
				if strings.ContainsAny(pn, " \t") {
					return s.errorf(lineNum, "parent names must be separated by commas, got %q (write %q)", pn, strings.Join(strings.Fields(pn), ", "))
				}
				if !isNamespacedIdent(pn) {
					return s.errorf(lineNum, "expected parent prompt name, got %q", pn)
				}
				if i > 0 {
					s.emit(Token{Type: TokComma, Text: ",", Line: lineNum})
				}
				s.emit(Token{Type: TokIdent, Text: pn, Line: lineNum})
			}
		} else if parts[2] == "extends" {
			return s.errorf(lineNum, "'extends' is not valid — use 'inherits': %q", strings.Replace(trimmed, "extends", "inherits", 1))
		} else {
			return s.errorf(lineNum, "invalid prompt declaration: %q", trimmed)
		}

		s.emit(Token{Type: TokLBrace, Text: "{", Line: lineNum})
		s.state = sInBody
		return nil

	case "block":
		if len(parts) != 3 || parts[2] != "{" {
			return s.errorf(lineNum, "expected 'block Name {', got %q", trimmed)
		}
		s.emit(Token{Type: TokKwBlock, Text: "block", Line: lineNum, Col: 1})
		if !isIdent(parts[1]) {
			return s.errorf(lineNum, "expected block name, got %q", parts[1])
		}
		s.emit(Token{Type: TokIdent, Text: parts[1], Line: lineNum})
		s.emit(Token{Type: TokLBrace, Text: "{", Line: lineNum})
		s.state = sInBody
		return nil

	case "overlay":
		if len(parts) != 3 || parts[2] != "{" {
			return s.errorf(lineNum, "expected 'overlay Name {', got %q", trimmed)
		}
		s.emit(Token{Type: TokKwOverlay, Text: "overlay", Line: lineNum, Col: 1})
		if !isIdent(parts[1]) {
			return s.errorf(lineNum, "expected overlay name, got %q", parts[1])
		}
		s.emit(Token{Type: TokIdent, Text: parts[1], Line: lineNum})
		s.emit(Token{Type: TokLBrace, Text: "{", Line: lineNum})
		s.state = sInBody
		return nil
	}

	return s.errorf(lineNum, "unexpected token at top level: %q", trimmed)
}

func (s *scanner) scanBodyLine(indent int, trimmed string, lineNum int) error {
	if trimmed == "}" {
		s.emit(Token{Type: TokRBrace, Text: "}", Line: lineNum})
		s.state = sTop
		return nil
	}

	parts := strings.Fields(trimmed)
	if len(parts) == 2 && parts[0] == "use" {
		if !isNamespacedIdent(parts[1]) {
			return s.errorf(lineNum, "expected block name after 'use', got %q", parts[1])
		}
		s.emit(Token{Type: TokKwUse, Text: "use", Line: lineNum})
		s.emit(Token{Type: TokIdent, Text: parts[1], Line: lineNum})
		return nil
	}

	if strings.HasPrefix(trimmed, "var ") {
		name, def, ok, err := parseVarLine(trimmed)
		if err != nil {
			return s.errorf(lineNum, "invalid var declaration: %v", err)
		}
		if !ok {
			return s.errorf(lineNum, "invalid var declaration: %q", trimmed)
		}
		s.emit(Token{Type: TokKwVar, Text: "var", Line: lineNum, Col: indent + 1})
		s.emit(Token{Type: TokIdent, Text: name, Line: lineNum})
		s.emit(Token{Type: TokTextLine, Text: def, Line: lineNum})
		return nil
	}

	if strings.HasPrefix(trimmed, "slot ") {
		name, metadata, ok := parseSlotLine(trimmed)
		if !ok {
			return s.errorf(lineNum, "invalid slot declaration: %q", trimmed)
		}
		s.emit(Token{Type: TokKwSlot, Text: "slot", Line: lineNum, Col: indent + 1})
		s.emit(Token{Type: TokIdent, Text: name, Line: lineNum})
		s.emit(Token{Type: TokTextLine, Text: metadata, Line: lineNum})
		return nil
	}

	if len(parts) == 3 && parts[0] == "variant" && parts[2] == "{" {
		if !isIdent(parts[1]) {
			return s.errorf(lineNum, "expected variant name, got %q", parts[1])
		}
		s.emit(Token{Type: TokKwVariant, Text: "variant", Line: lineNum, Col: indent + 1})
		s.emit(Token{Type: TokIdent, Text: parts[1], Line: lineNum})
		s.emit(Token{Type: TokLBrace, Text: "{", Line: lineNum})
		s.state = sInNestedBody
		return nil
	}

	if len(parts) == 3 && parts[0] == "env" && parts[2] == "{" {
		if !isIdent(parts[1]) {
			return s.errorf(lineNum, "expected env name, got %q", parts[1])
		}
		s.emit(Token{Type: TokKwEnv, Text: "env", Line: lineNum, Col: indent + 1})
		s.emit(Token{Type: TokIdent, Text: parts[1], Line: lineNum})
		s.emit(Token{Type: TokLBrace, Text: "{", Line: lineNum})
		s.state = sInNestedBody
		return nil
	}

	if trimmed == "contract {" {
		s.emit(Token{Type: TokKwContract, Text: "contract", Line: lineNum, Col: indent + 1})
		s.emit(Token{Type: TokLBrace, Text: "{", Line: lineNum})
		s.state = sInNestedBody
		return nil
	}

	if trimmed == "capabilities {" {
		s.emit(Token{Type: TokKwCapabilities, Text: "capabilities", Line: lineNum, Col: indent + 1})
		s.emit(Token{Type: TokLBrace, Text: "{", Line: lineNum})
		s.state = sInNestedBody
		return nil
	}

	// `tags: value1, value2, ...` — inline comma-separated metadata declaration.
	// Must be checked before parseFieldDecl because parseFieldDecl's bare-colon
	// rule only matches "fieldname:" with nothing after the colon.
	if strings.HasPrefix(trimmed, "tags:") {
		raw := strings.TrimSpace(strings.TrimPrefix(trimmed, "tags:"))
		s.emit(Token{Type: TokKwTags, Text: "tags", Line: lineNum, Col: indent + 1})
		s.emit(Token{Type: TokTextLine, Text: raw, Line: lineNum})
		return nil
	}

	if name, op, inline, ok := parseFieldDecl(trimmed); ok {
		s.emit(Token{Type: TokIdent, Text: name, Line: lineNum, Col: indent + 1})
		switch op {
		case ":":
			s.emit(Token{Type: TokColon, Text: ":", Line: lineNum})
		case ":=":
			s.emit(Token{Type: TokColonEq, Text: ":=", Line: lineNum})
		case "+=":
			s.emit(Token{Type: TokPlusEq, Text: "+=", Line: lineNum})
		case "-=":
			s.emit(Token{Type: TokMinusEq, Text: "-=", Line: lineNum})
		}
		// Emit inline content (e.g. "from(parent[*])" or "from(parent[*]) and {").
		s.inFrom = looksLikeFrom(inline)
		s.awaitFirst = inline == ""
		s.fromBlockDepth = 0
		if inline != "" {
			s.emit(Token{Type: TokTextLine, Text: inline, Line: lineNum, Col: indent + 1})
			if s.inFrom && strings.HasSuffix(inline, "{") {
				s.fromBlockDepth++
			}
		}
		s.state = sInFieldContent
		s.fieldIndent = indent
		return nil
	}

	return s.errorf(lineNum, "unexpected token in body: %q", trimmed)
}

func (s *scanner) scanNestedBodyLine(indent int, trimmed string, lineNum int) error {
	if trimmed == "}" {
		s.emit(Token{Type: TokRBrace, Text: "}", Line: lineNum})
		s.state = sInBody
		return nil
	}

	if name, op, inline, ok := parseFieldDecl(trimmed); ok {
		s.emit(Token{Type: TokIdent, Text: name, Line: lineNum, Col: indent + 1})
		switch op {
		case ":":
			s.emit(Token{Type: TokColon, Text: ":", Line: lineNum})
		case ":=":
			s.emit(Token{Type: TokColonEq, Text: ":=", Line: lineNum})
		case "+=":
			s.emit(Token{Type: TokPlusEq, Text: "+=", Line: lineNum})
		case "-=":
			s.emit(Token{Type: TokMinusEq, Text: "-=", Line: lineNum})
		}
		if inline != "" {
			s.emit(Token{Type: TokTextLine, Text: inline, Line: lineNum, Col: indent + 1})
		}
		s.state = sInNestedFieldContent
		s.nestedFieldIndent = indent
		return nil
	}

	return s.errorf(lineNum, "unexpected token in nested body: %q", trimmed)
}

// ScanVars parses a .vars.loom file, which contains only top-level var/slot
// declarations (no wrapping prompt or block body).
//
// Format:
//
//	var  name = "default"
//	slot name {}
//	slot name { required: true }
func ScanVars(filename, src string) ([]VarEntry, error) {
	var out []VarEntry
	for i, rawLine := range strings.Split(src, "\n") {
		lineNum := i + 1
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		if strings.HasPrefix(trimmed, "var ") {
			name, def, ok, err := parseVarLine(trimmed)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %v", filename, lineNum, err)
			}
			if !ok {
				return nil, fmt.Errorf("%s:%d: invalid var declaration: %q", filename, lineNum, trimmed)
			}
			out = append(out, VarEntry{Name: name, Default: def, IsSlot: false, Line: lineNum, File: filename})
			continue
		}

		if strings.HasPrefix(trimmed, "slot ") {
			name, metadata, ok := parseSlotLine(trimmed)
			if !ok {
				return nil, fmt.Errorf("%s:%d: invalid slot declaration: %q", filename, lineNum, trimmed)
			}
			required := strings.Contains(metadata, "required")
			out = append(out, VarEntry{Name: name, IsSlot: true, Required: required, Line: lineNum, File: filename})
			continue
		}

		return nil, fmt.Errorf("%s:%d: unexpected token in vars file: %q", filename, lineNum, trimmed)
	}
	return out, nil
}

// VarEntry is a single var or slot declaration parsed from a .vars.loom file.
type VarEntry struct {
	Name     string
	Default  string
	IsSlot   bool
	Required bool
	File     string
	Line     int
}

// looksLikeFrom reports whether s starts a from() / parent[...] expression (the only field
// values whose braces are structural).
func looksLikeFrom(s string) bool {
	return strings.HasPrefix(s, "from(") || strings.HasPrefix(s, "parent[")
}
