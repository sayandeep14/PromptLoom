// Package parser builds an AST from a .prompt source file by consuming the
// token stream produced by the lexer.
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/lexer"
)

// Parse tokenizes src and returns all top-level nodes declared in the file.
func Parse(filename, src string) ([]*ast.Node, error) {
	nodes, _, err := ParseWithComments(filename, src)
	return nodes, err
}

// ParseWithComments is Parse that also returns the file's full-line // comments,
// which the formatter uses to keep them.
func ParseWithComments(filename, src string) ([]*ast.Node, []lexer.Comment, error) {
	tokens, comments, err := lexer.ScanWithComments(filename, src)
	if err != nil {
		return nil, nil, err
	}
	p := &parser{filename: filename, tokens: tokens}
	nodes, err := p.parseAll()
	if err != nil {
		return nil, nil, err
	}
	return nodes, comments, nil
}

type parser struct {
	filename string
	tokens   []lexer.Token
	pos      int
}

func (p *parser) peek() lexer.Token {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return lexer.Token{Type: lexer.TokEOF}
}

// lastLine returns the line of the most recently consumed token (1 if none),
// used to point end-of-file errors at where the input stopped.
func (p *parser) lastLine() int {
	for i := p.pos - 1; i >= 0; i-- {
		if i < len(p.tokens) && p.tokens[i].Line > 0 {
			return p.tokens[i].Line
		}
	}
	return 1
}

func (p *parser) next() lexer.Token {
	t := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return t
}

func (p *parser) expect(typ lexer.TokType) (lexer.Token, error) {
	t := p.next()
	if t.Type != typ {
		return t, fmt.Errorf("%s:%d: expected %v, got %v (%q)", p.filename, t.Line, typ, t.Type, t.Text)
	}
	return t, nil
}

func (p *parser) parseAll() ([]*ast.Node, error) {
	var nodes []*ast.Node
	for p.peek().Type != lexer.TokEOF {
		node, err := p.parseNode()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (p *parser) parseNode() (*ast.Node, error) {
	t := p.next()
	node := &ast.Node{}

	switch t.Type {
	case lexer.TokKwPrompt:
		node.Kind = ast.KindPrompt
	case lexer.TokKwBlock:
		node.Kind = ast.KindBlock
	case lexer.TokKwOverlay:
		node.Kind = ast.KindOverlay
	default:
		return nil, fmt.Errorf("%s:%d: expected 'prompt', 'block', or 'overlay', got %q", p.filename, t.Line, t.Text)
	}

	node.Pos = ast.Position{File: p.filename, Line: t.Line}

	nameTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	node.Name = nameTok.Text

	if node.Kind == ast.KindPrompt && p.peek().Type == lexer.TokKwInherits {
		p.next()
		// Collect comma-separated parent names: Ident (Comma Ident)*.
		for {
			parentTok, err := p.expect(lexer.TokIdent)
			if err != nil {
				return nil, err
			}
			node.Parents = append(node.Parents, parentTok.Text)
			if p.peek().Type != lexer.TokComma {
				break
			}
			p.next() // consume comma
		}
		if len(node.Parents) > 0 {
			node.Parent = node.Parents[0] // backward compat
		}
	}

	if _, err := p.expect(lexer.TokLBrace); err != nil {
		return nil, err
	}

	if err := p.parseBody(node); err != nil {
		return nil, err
	}
	return node, nil
}

func (p *parser) parseBody(node *ast.Node) error {
	for {
		switch p.peek().Type {
		case lexer.TokRBrace:
			node.EndLine = p.next().Line
			return nil

		case lexer.TokKwUse:
			if node.Kind != ast.KindPrompt {
				t := p.peek()
				return fmt.Errorf("%s:%d: 'use' is only valid inside prompts", p.filename, t.Line)
			}
			useTok := p.next()
			nameTok, err := p.expect(lexer.TokIdent)
			if err != nil {
				return err
			}
			node.Uses = append(node.Uses, nameTok.Text)
			node.UsePos = append(node.UsePos, ast.Position{File: p.filename, Line: useTok.Line, Col: useTok.Col})

		case lexer.TokKwVar:
			if node.Kind != ast.KindPrompt {
				t := p.peek()
				return fmt.Errorf("%s:%d: 'var' is only valid inside prompts", p.filename, t.Line)
			}
			decl, err := p.parseVarDecl()
			if err != nil {
				return err
			}
			node.Vars = append(node.Vars, *decl)

		case lexer.TokKwSlot:
			if node.Kind != ast.KindPrompt {
				t := p.peek()
				return fmt.Errorf("%s:%d: 'slot' is only valid inside prompts", p.filename, t.Line)
			}
			decl, err := p.parseSlotDecl()
			if err != nil {
				return err
			}
			node.Vars = append(node.Vars, *decl)

		case lexer.TokKwVariant:
			if node.Kind != ast.KindPrompt {
				t := p.peek()
				return fmt.Errorf("%s:%d: 'variant' is only valid inside prompts", p.filename, t.Line)
			}
			variant, err := p.parseVariant()
			if err != nil {
				return err
			}
			node.Variants = append(node.Variants, *variant)

		case lexer.TokKwContract:
			if node.Kind != ast.KindPrompt {
				t := p.peek()
				return fmt.Errorf("%s:%d: 'contract' is only valid inside prompts", p.filename, t.Line)
			}
			block, err := p.parseContract()
			if err != nil {
				return err
			}
			node.Contract = block

		case lexer.TokKwCapabilities:
			if node.Kind != ast.KindPrompt {
				t := p.peek()
				return fmt.Errorf("%s:%d: 'capabilities' is only valid inside prompts", p.filename, t.Line)
			}
			block, err := p.parseCapabilities()
			if err != nil {
				return err
			}
			node.Capabilities = block

		case lexer.TokKwEnv:
			if node.Kind != ast.KindPrompt {
				t := p.peek()
				return fmt.Errorf("%s:%d: 'env' is only valid inside prompts", p.filename, t.Line)
			}
			eb, err := p.parseEnvBlock()
			if err != nil {
				return err
			}
			node.EnvBlocks = append(node.EnvBlocks, *eb)

		case lexer.TokKwTags:
			node.TagsLine = p.next().Line // consume TokKwTags
			rawTok, err := p.expect(lexer.TokTextLine)
			if err != nil {
				return err
			}
			node.Tags = parseTagList(rawTok.Text)

		case lexer.TokIdent:
			fieldOp, err := p.parseFieldOp()
			if err != nil {
				return err
			}
			node.Fields = append(node.Fields, *fieldOp)

		case lexer.TokEOF:
			return fmt.Errorf("%s:%d: unexpected end of file inside body of %q (declared at line %d) — add the missing closing '}'",
				p.filename, p.lastLine(), node.Name, node.Pos.Line)

		default:
			t := p.peek()
			return fmt.Errorf("%s:%d: unexpected token in body of %q: %q", p.filename, t.Line, node.Name, t.Text)
		}
	}
}

func (p *parser) parseFieldOp() (*ast.FieldOperation, error) {
	nameTok := p.next()

	opTok := p.next()
	fo := &ast.FieldOperation{
		FieldName: nameTok.Text,
		Pos:       ast.Position{File: p.filename, Line: nameTok.Line, Col: nameTok.Col},
	}

	switch opTok.Type {
	case lexer.TokColon:
		fo.Op = ast.OpDefine
	case lexer.TokColonEq:
		fo.Op = ast.OpOverride
	case lexer.TokPlusEq:
		fo.Op = ast.OpAppend
	case lexer.TokMinusEq:
		fo.Op = ast.OpRemove
	default:
		return nil, fmt.Errorf("%s:%d: expected field operator (:, :=, +=, -=), got %q", p.filename, opTok.Line, opTok.Text)
	}

	for p.peek().Type == lexer.TokTextLine {
		fo.Value = append(fo.Value, p.next().Text)
	}

	// If this is a := assignment and the first value line looks like a from()
	// expression, parse it into a structured FromExpression.
	if fo.Op == ast.OpOverride && len(fo.Value) > 0 && looksLikeFromExpr(fo.Value[0]) {
		fe, err := parseFromExpression(fo.Value, fo.Pos)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: in from() expression for %q: %w", p.filename, fo.Pos.Line, fo.FieldName, err)
		}
		fo.FromExpr = fe
	}

	return fo, nil
}

func (p *parser) parseVarDecl() (*ast.VarDecl, error) {
	kw := p.next()
	nameTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	valTok, err := p.expect(lexer.TokTextLine)
	if err != nil {
		return nil, err
	}
	return &ast.VarDecl{
		Name:     nameTok.Text,
		Default:  valTok.Text,
		Required: valTok.Text == "",
		Pos:      ast.Position{File: p.filename, Line: kw.Line, Col: kw.Col},
	}, nil
}

func (p *parser) parseSlotDecl() (*ast.VarDecl, error) {
	kw := p.next()
	nameTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	metaTok, err := p.expect(lexer.TokTextLine)
	if err != nil {
		return nil, err
	}

	decl := &ast.VarDecl{
		Name:     nameTok.Text,
		IsSlot:   true,
		Required: true,
		Pos:      ast.Position{File: p.filename, Line: kw.Line, Col: kw.Col},
	}

	metadata, err := parseInlineMap(metaTok.Text)
	if err != nil {
		return nil, fmt.Errorf("%s:%d: invalid slot declaration for %q: %v", p.filename, metaTok.Line, nameTok.Text, err)
	}
	if def, ok := metadata["default"]; ok {
		decl.Default = def
		decl.Required = def == ""
	}
	if required, ok := metadata["required"]; ok {
		decl.Required = strings.EqualFold(required, "true")
	}
	if secret, ok := metadata["secret"]; ok {
		decl.Secret = strings.EqualFold(secret, "true")
	}
	if decl.Default != "" {
		decl.Required = false
	}
	return decl, nil
}

func (p *parser) parseVariant() (*ast.VariantBlock, error) {
	kw := p.next()
	nameTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokLBrace); err != nil {
		return nil, err
	}

	fields, err := p.parseNestedFieldOps("variant")
	if err != nil {
		return nil, err
	}
	return &ast.VariantBlock{
		Name:   nameTok.Text,
		Fields: fields,
		Pos:    ast.Position{File: p.filename, Line: kw.Line, Col: kw.Col},
	}, nil
}

func (p *parser) parseEnvBlock() (*ast.EnvBlock, error) {
	kw := p.next()
	nameTok, err := p.expect(lexer.TokIdent)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(lexer.TokLBrace); err != nil {
		return nil, err
	}
	fields, err := p.parseNestedFieldOps("env")
	if err != nil {
		return nil, err
	}
	return &ast.EnvBlock{
		Name:   nameTok.Text,
		Fields: fields,
		Pos:    ast.Position{File: p.filename, Line: kw.Line, Col: kw.Col},
	}, nil
}

func (p *parser) parseContract() (*ast.ContractBlock, error) {
	kw := p.next()
	if _, err := p.expect(lexer.TokLBrace); err != nil {
		return nil, err
	}
	fields, err := p.parseNestedFieldOps("contract")
	if err != nil {
		return nil, err
	}

	block := &ast.ContractBlock{Pos: ast.Position{File: p.filename, Line: kw.Line, Col: kw.Col}}
	for _, field := range fields {
		values := stripBullets(field.Value)
		switch field.FieldName {
		case "required_sections":
			block.RequiredSections = append(block.RequiredSections, values...)
		case "forbidden_sections":
			block.ForbiddenSections = append(block.ForbiddenSections, values...)
		case "must_include":
			block.MustInclude = append(block.MustInclude, values...)
		case "must_not_include":
			block.MustNotInclude = append(block.MustNotInclude, values...)
		}
	}
	return block, nil
}

func (p *parser) parseCapabilities() (*ast.CapabilitiesBlock, error) {
	kw := p.next()
	if _, err := p.expect(lexer.TokLBrace); err != nil {
		return nil, err
	}
	fields, err := p.parseNestedFieldOps("capabilities")
	if err != nil {
		return nil, err
	}

	block := &ast.CapabilitiesBlock{Pos: ast.Position{File: p.filename, Line: kw.Line, Col: kw.Col}}
	for _, field := range fields {
		values := stripBullets(field.Value)
		switch field.FieldName {
		case "allowed":
			block.Allowed = append(block.Allowed, values...)
		case "forbidden":
			block.Forbidden = append(block.Forbidden, values...)
		}
	}
	return block, nil
}

func (p *parser) parseNestedFieldOps(kind string) ([]ast.FieldOperation, error) {
	var fields []ast.FieldOperation
	for {
		switch p.peek().Type {
		case lexer.TokRBrace:
			p.next()
			return fields, nil
		case lexer.TokIdent:
			field, err := p.parseFieldOp()
			if err != nil {
				return nil, err
			}
			fields = append(fields, *field)
		case lexer.TokEOF:
			return nil, fmt.Errorf("%s:%d: unexpected end of file inside %s block — add the missing closing '}'",
				p.filename, p.lastLine(), kind)
		default:
			t := p.peek()
			return nil, fmt.Errorf("%s:%d: unexpected token inside %s block: %q", p.filename, t.Line, kind, t.Text)
		}
	}
}

func parseInlineMap(raw string) (map[string]string, error) {
	out := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out, nil
	}

	for _, part := range strings.Split(raw, ",") {
		piece := strings.TrimSpace(part)
		if piece == "" {
			continue
		}
		kv := strings.SplitN(piece, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("expected key: value, got %q", piece)
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if strings.HasPrefix(val, "\"") {
			unquoted, err := strconv.Unquote(val)
			if err != nil {
				return nil, err
			}
			val = unquoted
		}
		out[key] = val
	}
	return out, nil
}

func stripBullets(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = strings.TrimPrefix(line, "- ")
	}
	return out
}

// parseTagList splits a comma-separated tag string into trimmed, non-empty tokens.
// E.g. "python, backend, api" → ["python", "backend", "api"].
func parseTagList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
