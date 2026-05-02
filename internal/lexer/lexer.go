// Package lexer tokenizes .prompt DSL source files into a flat token stream.
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

type scanner struct {
	filename          string
	lines             []string
	state             scanState
	fieldIndent       int
	nestedFieldIndent int
	tokens            []Token
}

// Scan tokenizes src and returns the full token stream, including a terminal TokEOF.
func Scan(filename, src string) ([]Token, error) {
	s := &scanner{
		filename: filename,
		lines:    strings.Split(src, "\n"),
	}
	if err := s.scan(); err != nil {
		return nil, err
	}
	return s.tokens, nil
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

// parseFieldDecl tries to recognise a field declaration in trimmed.
func parseFieldDecl(trimmed string) (name, op string, ok bool) {
	for _, candidate := range []string{":=", "+=", "-="} {
		if strings.HasSuffix(trimmed, candidate) {
			n := strings.TrimRight(trimmed[:len(trimmed)-len(candidate)], " \t")
			if isIdent(n) {
				return n, candidate, true
			}
		}
	}
	bare := strings.TrimRight(trimmed, " \t")
	if strings.HasSuffix(bare, ":") {
		n := bare[:len(bare)-1]
		if isIdent(n) {
			return n, ":", true
		}
	}
	return "", "", false
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
			if indent <= s.fieldIndent {
				s.state = sInBody
				if err := s.scanBodyLine(indent, trimmed, lineNum); err != nil {
					return err
				}
			} else {
				s.emit(Token{Type: TokTextLine, Text: trimmed, Line: lineNum, Col: indent + 1})
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

		switch len(parts) {
		case 3:
			if !isIdent(parts[1]) {
				return s.errorf(lineNum, "expected prompt name, got %q", parts[1])
			}
			s.emit(Token{Type: TokIdent, Text: parts[1], Line: lineNum})
		case 5:
			if parts[2] != "inherits" {
				return s.errorf(lineNum, "expected 'inherits', got %q", parts[2])
			}
			if !isIdent(parts[1]) {
				return s.errorf(lineNum, "expected prompt name, got %q", parts[1])
			}
			s.emit(Token{Type: TokIdent, Text: parts[1], Line: lineNum})
			s.emit(Token{Type: TokKwInherits, Text: "inherits", Line: lineNum})
			if !isIdent(parts[3]) {
				return s.errorf(lineNum, "expected parent prompt name, got %q", parts[3])
			}
			s.emit(Token{Type: TokIdent, Text: parts[3], Line: lineNum})
		default:
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
		if !isIdent(parts[1]) {
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

	if name, op, ok := parseFieldDecl(trimmed); ok {
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

	if name, op, ok := parseFieldDecl(trimmed); ok {
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
		s.state = sInNestedFieldContent
		s.nestedFieldIndent = indent
		return nil
	}

	return s.errorf(lineNum, "unexpected token in nested body: %q", trimmed)
}
