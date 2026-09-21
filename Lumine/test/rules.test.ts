import { test } from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { parseFromExpression, looksLikeFromExpr } from '../src/server/fromexpr';
import { findSyntaxError } from '../src/server/syntax';
import { parseLoomDocument } from '../src/server/parser';
import { LoomRegistry } from '../src/server/registry';
import { analyze } from './helpers';

// ── from() expression parser: same grammar and messages as internal/parser/fromexpr.go ──

const parse = (text: string) => parseFromExpression(text.split('\n'));

test('from(): every form parses to the same structure the Go parser builds', () => {
  assert.deepEqual(parse('from(parent[*])').units, [{ kind: 'parentRef', sub: { kind: 'all', n: 0, m: 0 } }]);
  assert.deepEqual(parse('from(parent[2])').units, [{ kind: 'parentRef', sub: { kind: 'index', n: 2, m: 0 } }]);
  assert.deepEqual(parse('from(parent[0..2])').units, [{ kind: 'parentRef', sub: { kind: 'range', n: 0, m: 2 } }]);
  assert.deepEqual(parse('from(Base)').units, [{ kind: 'namedRef', name: 'Base' }]);
  assert.deepEqual(parse('from(pack.Base)').units, [{ kind: 'namedRef', name: 'pack.Base' }]);
  assert.deepEqual(parse('parent[1].constraints[1..3]').units, [{
    kind: 'fieldRef', source: { kind: 'index', n: 1, m: 0 }, field: 'constraints', fieldSub: { kind: 'range', n: 1, m: 3 },
  }]);
  assert.deepEqual(parse('from(parent[*]) and {\n- one\n- two\n}').units, [
    { kind: 'parentRef', sub: { kind: 'all', n: 0, m: 0 } },
    { kind: 'literal', items: ['one', 'two'] },
  ]);
  assert.equal(parse('from(parent[0]) and\nfrom(parent[1]) and\n{\n- x\n}').units.length, 3);
});

test('from(): syntax errors carry the Go parser\'s wording', () => {
  const err = (text: string) => { try { parse(text); return ''; } catch (e) { return (e as Error).message; } };
  assert.match(err('from(parent[*]) from(parent[0])'), /expected 'and' between from\(\) units/);
  assert.match(err('from(parent[*'), /expected "\]"/);
  assert.match(err('from(parent[x])'), /expected '\*' or integer in subscript/);
  assert.match(err('from(parent[0..])'), /expected integer after '\.\.' in subscript/);
  assert.match(err('from(parent[*]) and {\n- a\n'), /unexpected end of from\(\) literal block/);
  assert.match(err('from(parent[*]) and {\nno bullet\n}'), /expected '- item' in literal block/);
  assert.match(err('bogus'), /expected 'from', 'parent\[', or '\{' in from\(\) expression/);
  assert.match(err('from(parent[*]) & x'), /unexpected character "&" in from\(\) expression/);
  assert.match(err('parent[0] x'), /expected "\.", got "x"/);
  assert.match(err('from(9)'), /expected 'parent' or parent name in from\(\)/);
});

test('looksLikeFromExpr matches only real expressions', () => {
  assert.ok(looksLikeFromExpr('  from(parent[0])') && looksLikeFromExpr('parent[0].x[*]'));
  assert.ok(!looksLikeFromExpr('from the start') && !looksLikeFromExpr('- from(x)'));
});

// ── load errors: the lexer's structural rules ─────────────────────────────────────────

const syn = (src: string) => findSyntaxError(src);

test('syntax: valid sources have no load error (including CRLF, comments, nested blocks)', () => {
  const ok = [
    'prompt A {\n  persona :=\n    x\n}\n',
    '// c\nprompt A inherits B, pack.C {\n  use G\n  var v = "a # b"\n  slot s\n  slot t { required: true }\n  tags: a, b\n  variant v1 {\n    persona :=\n      x\n  }\n  env prod {\n    persona :=\n      y\n  }\n  contract {\n    must_include:\n      - z\n  }\n}\n',
    'block B {\n  constraints :=\n    - c\n}\n\noverlay O {\n  notes :=\n    n\n}\n',
    'prompt A {\r\n  persona :=\r\n    x\r\n}\r\n',
    'prompt A inherits B {\n  instructions :=\n    from(parent[*]) and {\n      - x\n    }\n\n  notes :=\n    text ending in {\n}\n',
    '',
    '// only a comment\n',
  ];
  for (const src of ok) assert.equal(syn(src), undefined, JSON.stringify(src));
});

test('syntax: each load error, its line, and the Go message', () => {
  const cases: [string, number, RegExp][] = [
    ['prompt A\n}\n', 0, /expected '\{' at end of prompt declaration/],
    ['prompt {\n}\n', 0, /invalid prompt declaration/],
    ['prompt A bogus B {\n}\n', 0, /invalid prompt declaration/],
    ['prompt A inherits B C {\n}\n', 0, /parent names must be separated by commas/],
    ['prompt A inherits B,,C {\n}\n', 0, /empty parent name in inherits list/],
    ['block A B {\n}\n', 0, /expected 'block Name \{'/],
    ['overlay {\n}\n', 0, /expected 'overlay Name \{'/],
    ['\n\nnonsense\n', 2, /unexpected token at top level/],
    ['prompt A {\n  what is this\n}\n', 1, /unexpected token in body/],
    ['prompt A {\n  var x\n}\n', 1, /invalid var declaration/],
    ['prompt A {\n  var x = "open\n}\n', 1, /invalid var declaration/],
    ['prompt A {\n  slot a b\n}\n', 1, /invalid slot declaration/],
    ['prompt A {\n  variant a b {\n  }\n}\n', 1, /unexpected token in body|expected variant name/],
    ['prompt A {\n  variant v {\n    what\n  }\n}\n', 2, /unexpected token in nested body/],
    ['block B {\n  use G\n}\n', 1, /'use' is only valid inside prompts/],
    ['overlay O {\n  var x = "1"\n}\n', 1, /'var' is only valid inside prompts/],
    ['block B {\n  contract {\n  }\n}\n', 1, /'contract' is only valid inside prompts/],
    ['prompt Open {\n  persona :=\n    x\n', 3, /unexpected end of file inside body of "Open" \(declared at line 1\) — add the missing closing '\}'/],
  ];
  for (const [src, line, re] of cases) {
    const e = syn(src);
    assert.ok(e, `no error for ${JSON.stringify(src)}`);
    assert.match(e.message, re, JSON.stringify(src));
    assert.equal(e.line, line, `${JSON.stringify(src)} → line ${e.line}`);
  }
});

test('syntax: a trailing { in field text does not swallow the closing brace (Go lexer parity)', () => {
  // an unbalanced "{" in ordinary text is just text; the prompt still closes
  assert.equal(syn('prompt A {\n  notes :=\n    Respond with an object like {\n}\n\nprompt B {\n  notes :=\n    n\n}\n'), undefined);
});

test('syntax: reports only the first error, and diagnostics stop at a load error', () => {
  const a = analyze('prompt A {\n  bogus line\n}\n\nprompt B inherits Missing {\n}\n');
  assert.equal(a.diags.length, 1, 'nothing else is checked until load errors are fixed (like loom inspect)');
  assert.match(a.errors[0], /unexpected token in body/);
});

// ── diagnostics beyond the shared fixtures ────────────────────────────────────────────

const two = 'prompt A {\n  persona :=\n    a\n  instructions :=\n    - a\n}\n\nprompt B {\n  persona :=\n    b\n  instructions :=\n    - b\n}\n\n';

test('from(): type rules for scalars and lists', () => {
  const ok = analyze(two + 'prompt C inherits A, B {\n  persona :=\n    from(parent[1])\n  instructions :=\n    from(parent[*]) and {\n      - x\n    }\n}\n');
  assert.deepEqual(ok.errors, []);

  const bad = analyze(two + 'prompt C inherits A, B {\n  persona :=\n    from(parent[0]) and from(parent[1])\n\n  notes :=\n    from(parent[*])\n\n  summary :=\n    {\n      - x\n    }\n}\n');
  assert.equal(bad.errors.filter(e => e.includes("'and' cannot be used on scalar")).length, 1, bad.errors.join('\n'));
  assert.equal(bad.errors.filter(e => e.includes('from(parent[*]) cannot be used on scalar')).length, 1);
});

test('from(): checked in variants and env blocks too, and against THIS prompt\'s parents', () => {
  const a = analyze(two + 'prompt C inherits A {\n  variant v {\n    instructions :=\n      from(parent[3])\n  }\n  env prod {\n    instructions :=\n      from(Nope)\n  }\n}\n');
  assert.ok(a.errors.some(e => e.includes('parent[3] is out of range — prompt has 1 parent(s)')), a.errors.join('\n'));
  assert.ok(a.errors.some(e => e.includes('from(Nope) references "Nope" which is not a declared parent')));
});

test('from(): a malformed expression is a diagnostic, not a crash', () => {
  const a = analyze(two + 'prompt C inherits A {\n  instructions :=\n    from(parent[*]) but more\n}\n');
  assert.ok(a.errors.some(e => e.includes('in from() expression for "instructions"')), a.errors.join('\n'));
});

test('unknown things suggest the closest name', () => {
  const a = analyze(two + 'prompt C inherits Aa {\n  use Nothing\n  instrucions :=\n    - x\n}\n');
  assert.ok(a.errors.some(e => e.includes('inherits unknown prompt "Aa"') && e.includes('Did you mean "A"?')), a.errors.join('\n'));
  assert.ok(a.errors.some(e => e.includes('uses unknown block "Nothing"')));
  assert.ok(a.errors.some(e => e.includes('uses unknown field "instrucions"') && e.includes('Did you mean "instructions"?')));
});

test('cycles report the path, and a prompt in another file counts', () => {
  const others = { 'file:///t/b.prompt.loom': 'prompt B inherits A {\n}\n' };
  const a = analyze('prompt A inherits B {\n}\n', others);
  assert.ok(a.errors.some(e => e.includes('inheritance cycle detected: A -> B -> A')), a.errors.join('\n'));
});

test('placeholders: an error in prompts, a warning in blocks', () => {
  const a = analyze('prompt P {\n  var v = "1"\n  persona :=\n    {{ v }} {{ w }}\n}\n\nblock B {\n  notes :=\n    {{ x }}\n}\n');
  assert.deepEqual(a.errors.map(e => e.split('\n')[0]), ['prompt "P" references undeclared variable "w"']);
  assert.ok(a.warnings.some(w => w.includes('block "B" uses {{ x }} — variables must be declared in the consuming prompt')));
});

test('a prompt that writes a list its block defines is warned (PL-113)', () => {
  const blocks = { 'file:///t/g.block.loom': 'block Guard {\n  constraints :=\n    - safe\n}\n' };
  const a = analyze('prompt P {\n  use Guard\n  constraints :=\n    - mine\n  instructions :=\n    - fine\n}\n', blocks);
  const hits = a.warnings.filter(w => w.includes('replaces the items block "Guard" adds to it'));
  assert.equal(hits.length, 1, a.warnings.join('\n'));
  assert.match(hits[0], /writes "constraints"/);
});

// ── registry: two files may define the same name (a duplicate), and must not erase each other ──

test('registry keeps every definition of a name; removing one file leaves the other', () => {
  const r = new LoomRegistry();
  const n = (text: string) => parseLoomDocument(text, 'x').nodes;
  r.updateFile('file:///a', n('block Same {\n  notes :=\n    a\n}\n'), []);
  r.updateFile('file:///b', n('block Same {\n  notes :=\n    b\n}\n'), []);
  assert.equal(r.lookupBlock('Same')!.uri, 'file:///a');
  assert.equal(r.allNodes().filter(e => e.node.name === 'Same').length, 2);

  r.removeFile('file:///a');
  assert.equal(r.lookupBlock('Same')!.uri, 'file:///b', 'b\'s definition must survive removing a');
  r.updateFile('file:///b', n('block Other {\n}\n'), []);
  assert.equal(r.lookupBlock('Same'), undefined);
  assert.ok(r.lookupBlock('Other'));
});

test('duplicates across files are reported on both sides', () => {
  const other = { 'file:///t/one.block.loom': 'block Same {\n  notes :=\n    a\n}\n' };
  const a = analyze('block Same {\n  notes :=\n    b\n}\n', other);
  assert.ok(a.errors.some(e => e.startsWith('duplicate block name "Same" (first defined at one.block.loom:1)')), a.errors.join('\n'));
});

// ── the grammar: from() syntax is highlighted with the right (two-dot) subscripts ────

const grammar = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'syntaxes', 'loom.tmLanguage.json'), 'utf8'));
const re = (rule: string) => new RegExp(grammar.repository[rule].match);

test('grammar: from() forms match, with two-dot ranges', () => {
  const from = re('from-expression');
  for (const ok of ['from(parent[*])', 'from(parent[0])', 'from(parent[10])', 'from(parent[0..2])', 'from( parent[1] )']) {
    assert.ok(from.test(ok), ok);
  }
  for (const bad of ['from(parent[0...2])', 'from(parent[])', 'from(parent)', 'from(parent[a])']) {
    assert.ok(!from.test(bad), `${bad} must not be highlighted as valid`);
  }
  const named = re('from-named-parent');
  assert.ok(named.test('from(Base)') && named.test('from(pack.Base)'));
  assert.ok(!named.test('from(parent[0])'), 'parent[...] is not a named parent');
  const ref = re('from-field-ref');
  assert.ok(ref.test('parent[0].constraints[1..3]') && ref.test('parent[*].instructions[*]'));
  const and = re('from-and-keyword');
  assert.ok(and.test('from(parent[*]) and {') && and.test('from(parent[0]) and from(parent[1])'));
  assert.ok(!and.test('this and that'), 'plain prose is not the keyword');
});

test('grammar: every from rule is included in the top-level patterns', () => {
  const includes = JSON.stringify(grammar.patterns);
  for (const rule of ['from-expression', 'from-named-parent', 'from-field-ref', 'from-and-keyword']) {
    assert.ok(includes.includes(`#${rule}`), rule);
  }
});
