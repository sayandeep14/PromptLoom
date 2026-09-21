package parser

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/sayandeep14/PromptLoom/internal/ast"
)

// looksLikeFromExpr reports whether the first content line of a := field
// starts with a from() expression rather than plain text.
func looksLikeFromExpr(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "from(") || strings.HasPrefix(s, "parent[")
}

// parseFromExpression parses the collected text lines of a := field body into
// a structured FromExpression. Lines are the raw TokTextLine values; the first
// line contains the expression header (e.g. "from(parent[*]) and {"), subsequent
// lines are items inside an "and { ... }" block, and a "}" line closes it.
func parseFromExpression(lines []string, pos ast.Position) (*ast.FromExpression, error) {
	text := strings.Join(lines, "\n")
	p := &feParser{text: text, pos: pos}
	return p.parse()
}

// ---- internal from-expression tokenizer / parser ----

type feTokenType int

const (
	feTokIdent   feTokenType = iota
	feTokInt                 // integer literal
	feTokLParen              // (
	feTokRParen              // )
	feTokLBrack              // [
	feTokRBrack              // ]
	feTokLBrace              // {
	feTokRBrace              // }
	feTokDot                 // .
	feTokStar                // *
	feTokDotDot              // ..
	feTokNewline             // \n
	feTokDash                // - (at start of a bullet item line)
	feTokEOF
)

type feToken struct {
	typ  feTokenType
	text string
}

type feParser struct {
	text   string
	cursor int
	pos    ast.Position // source position for error messages
}

func (p *feParser) peek() (feToken, error) {
	saved := p.cursor
	tok, err := p.next()
	p.cursor = saved
	return tok, err
}

func (p *feParser) next() (feToken, error) {
	// Skip spaces and tabs (but not newlines).
	for p.cursor < len(p.text) && (p.text[p.cursor] == ' ' || p.text[p.cursor] == '\t') {
		p.cursor++
	}
	if p.cursor >= len(p.text) {
		return feToken{typ: feTokEOF}, nil
	}

	ch := p.text[p.cursor]

	if ch == '\n' {
		p.cursor++
		return feToken{typ: feTokNewline, text: "\n"}, nil
	}
	if ch == '(' {
		p.cursor++
		return feToken{typ: feTokLParen, text: "("}, nil
	}
	if ch == ')' {
		p.cursor++
		return feToken{typ: feTokRParen, text: ")"}, nil
	}
	if ch == '[' {
		p.cursor++
		return feToken{typ: feTokLBrack, text: "["}, nil
	}
	if ch == ']' {
		p.cursor++
		return feToken{typ: feTokRBrack, text: "]"}, nil
	}
	if ch == '{' {
		p.cursor++
		return feToken{typ: feTokLBrace, text: "{"}, nil
	}
	if ch == '}' {
		p.cursor++
		return feToken{typ: feTokRBrace, text: "}"}, nil
	}
	if ch == '*' {
		p.cursor++
		return feToken{typ: feTokStar, text: "*"}, nil
	}
	if ch == '-' {
		// Could be the start of a bullet item "- text" or the ".." range.
		// Here it's only "-" at the start of a bullet line (after a newline).
		p.cursor++
		return feToken{typ: feTokDash, text: "-"}, nil
	}
	if ch == '.' {
		// Check for ".." range operator.
		if p.cursor+1 < len(p.text) && p.text[p.cursor+1] == '.' {
			p.cursor += 2
			return feToken{typ: feTokDotDot, text: ".."}, nil
		}
		p.cursor++
		return feToken{typ: feTokDot, text: "."}, nil
	}
	if unicode.IsDigit(rune(ch)) {
		start := p.cursor
		for p.cursor < len(p.text) && unicode.IsDigit(rune(p.text[p.cursor])) {
			p.cursor++
		}
		return feToken{typ: feTokInt, text: p.text[start:p.cursor]}, nil
	}
	if unicode.IsLetter(rune(ch)) || ch == '_' {
		start := p.cursor
		for p.cursor < len(p.text) && (unicode.IsLetter(rune(p.text[p.cursor])) || unicode.IsDigit(rune(p.text[p.cursor])) || p.text[p.cursor] == '_' || p.text[p.cursor] == '-') {
			p.cursor++
		}
		return feToken{typ: feTokIdent, text: p.text[start:p.cursor]}, nil
	}

	return feToken{}, fmt.Errorf("unexpected character %q in from() expression", string(ch))
}

func (p *feParser) errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func (p *feParser) parse() (*ast.FromExpression, error) {
	fe := &ast.FromExpression{Pos: p.pos}

	// Skip leading newlines.
	p.skipNewlines()

	for {
		unit, err := p.parseUnit()
		if err != nil {
			return nil, err
		}
		fe.Units = append(fe.Units, unit)

		p.skipNewlines()
		tok, err := p.peek()
		if err != nil {
			return nil, err
		}
		if tok.typ == feTokEOF {
			break
		}
		// Expect "and" keyword between units.
		if tok.typ != feTokIdent || tok.text != "and" {
			return nil, p.errorf("expected 'and' between from() units, got %q", tok.text)
		}
		p.next() //nolint:errcheck
		p.skipNewlines()
	}

	return fe, nil
}

func (p *feParser) skipNewlines() {
	for {
		saved := p.cursor
		tok, err := p.next()
		if err != nil || tok.typ != feTokNewline {
			p.cursor = saved
			return
		}
	}
}

func (p *feParser) parseUnit() (ast.FromUnit, error) {
	tok, err := p.peek()
	if err != nil {
		return ast.FromUnit{}, err
	}

	switch {
	case tok.typ == feTokIdent && tok.text == "from":
		return p.parseFromCall()
	case tok.typ == feTokLBrace:
		return p.parseLiteralBlock()
	case tok.typ == feTokIdent && tok.text == "parent":
		return p.parseFieldRef()
	default:
		return ast.FromUnit{}, p.errorf("expected 'from', 'parent[', or '{' in from() expression, got %q", tok.text)
	}
}

// parseFromCall parses "from(source)".
func (p *feParser) parseFromCall() (ast.FromUnit, error) {
	p.next() // consume "from"
	if err := p.consumeType(feTokLParen, "("); err != nil {
		return ast.FromUnit{}, err
	}

	tok, err := p.peek()
	if err != nil {
		return ast.FromUnit{}, err
	}

	var unit ast.FromUnit

	if tok.typ == feTokIdent && tok.text == "parent" {
		// from(parent[subscript])
		p.next() // consume "parent"
		sub, err := p.parseSubscript()
		if err != nil {
			return ast.FromUnit{}, err
		}
		unit = ast.FromUnit{Kind: ast.FromParentRef, ParentSub: sub, Pos: p.pos}
	} else if tok.typ == feTokIdent {
		// from(pack.Name) or from(BareName)
		name, err := p.parseNamespacedIdent()
		if err != nil {
			return ast.FromUnit{}, err
		}
		unit = ast.FromUnit{Kind: ast.FromNamedRef, ParentName: name, Pos: p.pos}
	} else {
		return ast.FromUnit{}, p.errorf("expected 'parent' or parent name in from(), got %q", tok.text)
	}

	if err := p.consumeType(feTokRParen, ")"); err != nil {
		return ast.FromUnit{}, err
	}
	return unit, nil
}

// parseFieldRef parses "parent[sub].fieldName[sub]".
func (p *feParser) parseFieldRef() (ast.FromUnit, error) {
	p.next() // consume "parent"
	sourceSub, err := p.parseSubscript()
	if err != nil {
		return ast.FromUnit{}, err
	}
	if err := p.consumeType(feTokDot, "."); err != nil {
		return ast.FromUnit{}, err
	}
	fieldTok, err := p.next()
	if err != nil {
		return ast.FromUnit{}, err
	}
	if fieldTok.typ != feTokIdent {
		return ast.FromUnit{}, p.errorf("expected field name after '.', got %q", fieldTok.text)
	}
	fieldSub, err := p.parseSubscript()
	if err != nil {
		return ast.FromUnit{}, err
	}
	return ast.FromUnit{
		Kind:      ast.FromFieldRef,
		SourceSub: sourceSub,
		FieldName: fieldTok.text,
		FieldSub:  fieldSub,
		Pos:       p.pos,
	}, nil
}

// parseLiteralBlock parses "{ - item1\n- item2\n}".
func (p *feParser) parseLiteralBlock() (ast.FromUnit, error) {
	p.next() // consume "{"
	var items []string
	for {
		p.skipNewlines()
		tok, err := p.peek()
		if err != nil {
			return ast.FromUnit{}, err
		}
		if tok.typ == feTokRBrace {
			p.next()
			break
		}
		if tok.typ == feTokEOF {
			return ast.FromUnit{}, p.errorf("unexpected end of from() literal block")
		}
		// Expect "- item text"
		if tok.typ != feTokDash {
			return ast.FromUnit{}, p.errorf("expected '- item' in literal block, got %q", tok.text)
		}
		p.next() // consume "-"
		// Collect the rest of this line as item text.
		item := p.readRestOfLine()
		items = append(items, strings.TrimSpace(item))
	}
	return ast.FromUnit{Kind: ast.FromLiteral, Items: items, Pos: p.pos}, nil
}

// readRestOfLine reads until newline or EOF, returning the raw text.
func (p *feParser) readRestOfLine() string {
	start := p.cursor
	for p.cursor < len(p.text) && p.text[p.cursor] != '\n' {
		p.cursor++
	}
	return p.text[start:p.cursor]
}

// parseSubscript parses "[*]", "[N]", or "[N..M]".
func (p *feParser) parseSubscript() (ast.Subscript, error) {
	if err := p.consumeType(feTokLBrack, "["); err != nil {
		return ast.Subscript{}, err
	}

	tok, err := p.next()
	if err != nil {
		return ast.Subscript{}, err
	}

	var sub ast.Subscript
	switch tok.typ {
	case feTokStar:
		sub.Kind = ast.SubAll
	case feTokInt:
		n, _ := strconv.Atoi(tok.text)
		// Check for range ".."
		next, err := p.peek()
		if err != nil {
			return ast.Subscript{}, err
		}
		if next.typ == feTokDotDot {
			p.next() // consume ".."
			mTok, err := p.next()
			if err != nil {
				return ast.Subscript{}, err
			}
			if mTok.typ != feTokInt {
				return ast.Subscript{}, p.errorf("expected integer after '..' in subscript, got %q", mTok.text)
			}
			m, _ := strconv.Atoi(mTok.text)
			sub = ast.Subscript{Kind: ast.SubRange, N: n, M: m}
		} else {
			sub = ast.Subscript{Kind: ast.SubIndex, N: n}
		}
	default:
		return ast.Subscript{}, p.errorf("expected '*' or integer in subscript, got %q", tok.text)
	}

	if err := p.consumeType(feTokRBrack, "]"); err != nil {
		return ast.Subscript{}, err
	}
	return sub, nil
}

// parseNamespacedIdent parses "ident" or "ident.ident".
func (p *feParser) parseNamespacedIdent() (string, error) {
	tok, err := p.next()
	if err != nil {
		return "", err
	}
	if tok.typ != feTokIdent {
		return "", p.errorf("expected identifier, got %q", tok.text)
	}
	name := tok.text

	next, err := p.peek()
	if err != nil {
		return "", err
	}
	if next.typ == feTokDot {
		p.next() // consume "."
		tok2, err := p.next()
		if err != nil {
			return "", err
		}
		if tok2.typ != feTokIdent {
			return "", p.errorf("expected identifier after '.', got %q", tok2.text)
		}
		name = name + "." + tok2.text
	}
	return name, nil
}

func (p *feParser) consumeType(typ feTokenType, expected string) error {
	tok, err := p.next()
	if err != nil {
		return err
	}
	if tok.typ != typ {
		return p.errorf("expected %q, got %q", expected, tok.text)
	}
	return nil
}
