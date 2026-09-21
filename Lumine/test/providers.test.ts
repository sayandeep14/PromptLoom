import { test } from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { TextDocument } from 'vscode-languageserver-textdocument';
import { parseLoomDocument } from '../src/server/parser';
import { LoomRegistry } from '../src/server/registry';
import { getCompletions } from '../src/providers/completion';
import { getHover } from '../src/providers/hover';
import { getDefinition, fromTargetAt } from '../src/providers/definition';
import { getReferences } from '../src/providers/references';

/** A workspace of in-memory files, wired to the providers the way the server wires them. */
class Workspace {
  private docs = new Map<string, TextDocument>();
  readonly registry = new LoomRegistry();

  add(name: string, text: string): string {
    const uri = `file:///w/${name}`;
    this.docs.set(uri, TextDocument.create(uri, 'loom', 1, text));
    this.registry.updateFile(uri, parseLoomDocument(text, uri).nodes, []);
    return uri;
  }
  get documents(): any { return { get: (u: string) => this.docs.get(u) }; }

  /** Position of the first occurrence of `marker` in the document (character = start of it + offset). */
  at(uri: string, marker: string, offset = 0) {
    const lines = this.docs.get(uri)!.getText().split('\n');
    for (let l = 0; l < lines.length; l++) {
      const c = lines[l].indexOf(marker);
      if (c >= 0) return { line: l, character: c + offset };
    }
    throw new Error(`"${marker}" not found`);
  }
  complete(uri: string, position: { line: number; character: number }) {
    return getCompletions({ textDocument: { uri }, position } as any, this.documents, this.registry);
  }
  hover(uri: string, position: { line: number; character: number }) {
    return getHover({ textDocument: { uri }, position } as any, this.documents, this.registry);
  }
  definition(uri: string, position: { line: number; character: number }) {
    return getDefinition({ textDocument: { uri }, position } as any, this.documents, this.registry) as any;
  }
  references(uri: string, position: { line: number; character: number }, includeDeclaration = false) {
    return getReferences({ textDocument: { uri }, position, context: { includeDeclaration } } as any, this.documents, this.registry);
  }
}

const labels = (items: { label: string }[]) => items.map(i => i.label);
const hoverText = (h: any) => (h ? String(h.contents.value ?? h.contents) : '');

const BASE = 'prompt Base {\n  persona :=\n    p\n\n  instructions :=\n    - a\n}\n';
const OTHER = 'prompt Other {\n  instructions :=\n    - b\n}\n';
const GUARD = 'block Guard {\n  constraints :=\n    - safe\n}\n';

function workspace(child: string) {
  const w = new Workspace();
  w.add('Base.prompt.loom', BASE);
  w.add('Other.prompt.loom', OTHER);
  w.add('Guard.block.loom', GUARD);
  const uri = w.add('Child.prompt.loom', child);
  return { w, uri };
}

// ── completion ────────────────────────────────────────────────────────────────

test('completion: a LIST field after := offers all from() forms, with the real parent count', () => {
  const { w, uri } = workspace('prompt Child inherits Base, Other {\n  instructions := \n}\n');
  const items = w.complete(uri, { line: 1, character: '  instructions := '.length });
  const l = labels(items);
  for (const want of ['from(parent[*])', 'from(parent[*]) and { ... }', 'from(parent[0])', 'from(parent[1])', 'from(Base)', 'from(Other)', 'from(parent[0..2])', 'parent[0].instructions[1..3]']) {
    assert.ok(l.includes(want), `missing ${want} in ${l.join(', ')}`);
  }
  assert.ok(!l.includes('from(parent[2])'), 'no such parent');
});

test('completion: the range snippet uses two dots (three dots is a syntax error)', () => {
  const { w, uri } = workspace('prompt Child inherits Base, Other {\n  instructions := \n}\n');
  for (const it of w.complete(uri, { line: 1, character: '  instructions := '.length })) {
    assert.ok(!String(it.insertText).includes('...'), `${it.label} inserts "..."`);
  }
});

test('completion: a SCALAR field only gets single-parent forms (no [*], ranges, or "and")', () => {
  const { w, uri } = workspace('prompt Child inherits Base, Other {\n  persona := \n}\n');
  const l = labels(w.complete(uri, { line: 1, character: '  persona := '.length }));
  assert.deepEqual(l.sort(), ['from(Base)', 'from(Other)', 'from(parent[0])', 'from(parent[1])'].sort());
});

test('completion: no from() suggestions where from() would be an error', () => {
  // a prompt without parents
  let { w, uri } = workspace('prompt Solo {\n  instructions := \n}\n');
  assert.deepEqual(w.complete(uri, { line: 1, character: '  instructions := '.length }), []);
  // a block (blocks have no parents)
  ({ w, uri } = workspace('block B {\n  constraints := \n}\n'));
  assert.deepEqual(w.complete(uri, { line: 1, character: '  constraints := '.length }), []);
});

test('completion: inside from( it offers parents; parent[ offers indices; parent[N]. offers fields', () => {
  const { w, uri } = workspace('prompt Child inherits Base, Other {\n  instructions :=\n    from(\n}\n');
  let l = labels(w.complete(uri, { line: 2, character: '    from('.length }));
  assert.deepEqual(l.sort(), ['Base', 'Other', 'parent[*]', 'parent[0]', 'parent[1]'].sort());

  const t2 = workspace('prompt Child inherits Base, Other {\n  instructions :=\n    from(parent[\n}\n');
  l = labels(t2.w.complete(t2.uri, { line: 2, character: '    from(parent['.length }));
  assert.deepEqual(l, ['*', '0', '1', '0..2']);

  const t3 = workspace('prompt Child inherits Base {\n  constraints :=\n    parent[0].\n}\n');
  l = labels(t3.w.complete(t3.uri, { line: 2, character: '    parent[0].'.length }));
  assert.ok(l.includes('constraints') && l.includes('instructions') && !l.includes('persona'), l.join(', '));

  // for a scalar the subscript menu has no [*] and no range
  const t4 = workspace('prompt Child inherits Base, Other {\n  persona :=\n    from(parent[\n}\n');
  assert.deepEqual(labels(t4.w.complete(t4.uri, { line: 2, character: '    from(parent['.length })), ['0', '1']);
});

test('completion: after "and" only the continuation forms are offered', () => {
  const { w, uri } = workspace('prompt Child inherits Base {\n  instructions :=\n    from(parent[0]) and \n}\n');
  const l = labels(w.complete(uri, { line: 2, character: '    from(parent[0]) and '.length }));
  assert.ok(l.includes('{ ... }') && l.includes('from(parent[*])'));
  assert.ok(!l.includes('from(parent[*]) and { ... }'));
});

test('completion: inherits and use list the project\'s prompts and blocks', () => {
  const { w, uri } = workspace('prompt Child inherits \n  use \n}\n');
  const p = labels(w.complete(uri, { line: 0, character: 'prompt Child inherits '.length }));
  assert.ok(p.includes('Base') && p.includes('Other'));
  const b = labels(w.complete(uri, { line: 1, character: '  use '.length }));
  assert.ok(b.includes('Guard'));
  // after a comma the next parent is offered too
  const t = workspace('prompt Child inherits Base, \n}\n');
  assert.ok(labels(t.w.complete(t.uri, { line: 0, character: 'prompt Child inherits Base, '.length })).includes('Other'));
});

test('completion: structure keywords by context; fields are written with :=', () => {
  const { w, uri } = workspace('\nprompt P {\n  \n  contract {\n    \n  }\n}\n');
  assert.deepEqual(labels(w.complete(uri, { line: 0, character: 0 })).sort(), ['block', 'overlay', 'prompt']);
  const body = w.complete(uri, { line: 2, character: 2 });
  const l = labels(body);
  for (const want of ['use', 'var', 'slot', 'variant', 'env', 'contract', 'persona :=', 'instructions :=']) assert.ok(l.includes(want), want);
  assert.ok(!l.includes('inherits'), 'inherits belongs on the declaration line');
  const field = body.find(i => i.label === 'constraints :=' && String(i.insertText).startsWith('constraints :=\n'));
  assert.ok(field, 'fields are inserted with :=');
  assert.ok(!body.some(i => i.kind === 5 && /^[a-z_]+:\n/.test(String(i.insertText ?? ''))), 'no v1 bare-colon field');
  assert.deepEqual(labels(w.complete(uri, { line: 4, character: 4 })).sort(),
    ['forbidden_sections :', 'must_include :', 'must_not_include :', 'required_sections :']);
});

test('completion: {{ }} offers the variables the prompt declares', () => {
  const { w, uri } = workspace('prompt P {\n  var lang = "go"\n  slot repo { required: true }\n\n  persona :=\n    {{ \n}\n');
  const l = labels(w.complete(uri, { line: 5, character: '    {{ '.length }));
  assert.ok(l.includes('lang') && l.includes('repo'), l.join(', '));
});

// ── hover ─────────────────────────────────────────────────────────────────────

test('hover: fields, keywords, references, variables and from()', () => {
  const { w, uri } = workspace(
    'prompt Child inherits Base, Other {\n  use Guard\n  var lang = "go"\n\n  persona :=\n    from(parent[1]) in {{ lang }}\n\n  instructions :=\n    from(parent[*]) and {\n      - x\n    }\n}\n');
  assert.match(hoverText(w.hover(uri, w.at(uri, 'persona', 2))), /persona/);
  assert.match(hoverText(w.hover(uri, w.at(uri, 'Base', 1))), /Base/);
  assert.match(hoverText(w.hover(uri, w.at(uri, 'Guard', 1))), /Guard/);
  const p = hoverText(w.hover(uri, w.at(uri, 'parent[1]', 3)));
  assert.match(p, /`Other`/, 'parent[1] is Other');
  assert.match(p, /Base.*\(0\).*Other.*\(1\)/s);
  assert.match(hoverText(w.hover(uri, w.at(uri, '{{ lang }}', 4))), /lang/);
  const f = hoverText(w.hover(uri, w.at(uri, 'from(parent[*])', 1)));
  assert.match(f, /from\(\)/);
  assert.match(f, /parent\[N\.\.M\]/, 'documents the two-dot range');
  assert.ok(!f.includes('N...M'), 'the old three-dot syntax must be gone');
  // nothing to say about plain text or comments
  assert.equal(w.hover(uri, w.at(uri, '- x', 5)), null);
});

test('hover: parent[N..M] names the parents in the range; an out-of-range index says so', () => {
  const { w, uri } = workspace('prompt Child inherits Base, Other {\n  instructions :=\n    from(parent[0..2]) and from(parent[5])\n}\n');
  assert.match(hoverText(w.hover(uri, w.at(uri, 'parent[0..2]', 3))), /`Base`.*`Other`/s);
  assert.match(hoverText(w.hover(uri, w.at(uri, 'parent[5]', 3))), /no such parent/);
});

// ── definition ────────────────────────────────────────────────────────────────

test('definition: inherits (each parent of several), use, from(Name), parent[N], variables', () => {
  const { w, uri } = workspace(
    'prompt Child inherits Base, Other {\n  use Guard\n  var lang = "go"\n\n  persona :=\n    from(parent[0]) {{ lang }}\n\n  instructions :=\n    from(Other) and from(parent[1])\n}\n');
  const go = (marker: string, off = 1) => w.definition(uri, w.at(uri, marker, off));

  assert.ok(go('Base, Other', 1).uri.endsWith('Base.prompt.loom'), 'first parent');
  assert.ok(go('Other {', 1).uri.endsWith('Other.prompt.loom'), 'second parent');
  assert.ok(go('Guard', 1).uri.endsWith('Guard.block.loom'), 'use');
  assert.ok(go('from(Other)', 6).uri.endsWith('Other.prompt.loom'), 'from(Name)');
  assert.ok(go('parent[0]', 3).uri.endsWith('Base.prompt.loom'), 'parent[0] is Base');
  assert.ok(go('parent[1]', 3).uri.endsWith('Other.prompt.loom'), 'parent[1] is Other');
  const v = go('{{ lang }}', 4);
  assert.equal(v.uri, uri);
  assert.equal(v.range.start.line, 2, 'points at the var declaration');
  assert.equal(w.definition(uri, w.at(uri, 'persona', 2)), null);
  // an unknown parent has no definition
  const u = workspace('prompt C inherits Nope {\n}\n');
  assert.equal(u.w.definition(u.uri, u.w.at(u.uri, 'Nope', 1)), null);
});

test('fromTargetAt: only inside from(Name) and parent[N]', () => {
  const parents = ['Base', 'Other'];
  const line = '    from(Other) and parent[0..2] and from(parent[1])';
  assert.equal(fromTargetAt(line, line.indexOf('Other') + 1, parents), 'Other');
  assert.equal(fromTargetAt(line, line.indexOf('parent[0..2]') + 3, parents), 'Base');
  assert.equal(fromTargetAt(line, line.lastIndexOf('parent[1]') + 3, parents), 'Other');
  assert.equal(fromTargetAt(line, 0, parents), undefined);
  assert.equal(fromTargetAt('    from(parent[*])', 12, parents), undefined);
  assert.equal(fromTargetAt('    from(parent[7])', 12, parents), undefined, 'no such parent');
});

// ── references ────────────────────────────────────────────────────────────────

test('references: prompts by inherits, blocks by use, variables by {{ }}', () => {
  const w = new Workspace();
  const base = w.add('Base.prompt.loom', BASE);
  w.add('A.prompt.loom', 'prompt A inherits Base {\n}\n');
  w.add('B.prompt.loom', 'prompt B inherits Other, Base {\n  use Guard\n}\n');
  w.add('Other.prompt.loom', OTHER);
  const guard = w.add('Guard.block.loom', GUARD);
  const c = w.add('C.prompt.loom', 'prompt C {\n  use Guard\n  var v = "x"\n\n  persona :=\n    {{ v }} and {{ v }}\n}\n');

  const refs = w.references(base, w.at(base, 'Base', 0));
  assert.deepEqual(refs.map(r => r.uri.split('/').pop()).sort(), ['A.prompt.loom', 'B.prompt.loom']);

  assert.equal(w.references(guard, w.at(guard, 'Guard', 0)).length, 2, 'used by B and C');
  // asked from inside `inherits Other, Base` on the second name
  const b = w.add('B.prompt.loom', 'prompt B inherits Other, Base {\n  use Guard\n}\n');
  assert.equal(w.references(b, w.at(b, 'Base', 1)).length, 2, 'A and B');

  const vars = w.references(c, w.at(c, 'v =', 0));
  assert.equal(vars.length, 2, '{{ v }} twice');
  assert.equal(w.references(c, w.at(c, 'v =', 0), true).length, 3, 'with the declaration');
});
