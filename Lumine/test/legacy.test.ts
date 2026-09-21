import { test } from 'node:test';
import assert from 'node:assert/strict';
import { analyze } from './helpers';
import { parseLoomDocument } from '../src/server/parser';

const V2 = `
prompt Base {
  tags: go, review

  persona :=
    You are a reviewer.

  instructions :=
    - Read the diff.
}

prompt Child inherits Base {
  // extends the parent's list
  instructions :=
    from(parent[0]) and {
      - Check errors.
    }

  variant strict {
    constraints :=
      - Every change needs tests.
  }

  env prod {
    constraints :=
      - No debug logging.
  }

  contract {
    required_sections:
      - Summary
    must_not_include:
      - "LGTM"
  }

  capabilities {
    allowed:
      - read_code
    forbidden:
      - delete_files
  }
}
`;

test('clean v2 source produces no diagnostics (contract/capabilities keys keep their colon)', () => {
  const a = analyze(V2);
  assert.deepEqual(a.diags.map(d => d.message), []);
});

test('env blocks are parsed as env blocks, not as fields of the prompt', () => {
  const { nodes } = analyze(V2);
  const child = nodes.find(n => n.name === 'Child')!;
  assert.equal(child.envBlocks.length, 1);
  assert.equal(child.envBlocks[0].name, 'prod');
  assert.equal(child.envBlocks[0].fields[0].fieldName, 'constraints');
  assert.deepEqual(child.fields.map(f => f.fieldName), ['instructions']);
});

test("'+=' is an error carrying the exact rewrite", () => {
  const a = analyze(`
prompt A {
  instructions :=
    - a
}

prompt B inherits A {
  instructions +=
    - b
}
`);
  assert.equal(a.errors.length, 1);
  assert.match(a.errors[0], /'\+=' is not valid in v2/);
  assert.match(a.errors[0], /instructions :=\n\s+from\(parent\[0\]\) and \{/);
  const d = a.diags.find(x => x.code === 'legacy-append')!;
  assert.equal(d.range.start.line, 7);           // points at the offending line
});

test("'+=' advice depends on the situation", () => {
  const cases: [string, string, RegExp[], RegExp[]][] = [
    ['several parents',
      'prompt A {\n  instructions :=\n    - a\n}\nprompt B {\n  instructions :=\n    - b\n}\nprompt C inherits A, B {\n  instructions +=\n    - c\n}',
      [/from\(parent\[\*\]\) and \{/, /from\(parent\[N\]\)/], []],
    ['no parent', 'prompt A {\n  instructions +=\n    - a\n}',
      [/no parent to append to/], [/from\(/]],
    ['scalar', 'prompt A {\n  persona :=\n    x\n}\nprompt B inherits A {\n  persona +=\n    more\n}',
      [/scalar field cannot be appended/], []],
    ['block', 'block B {\n  constraints +=\n    - x\n}',
      [/already ADD their list items/], [/from\(/]],
    ['overlay', 'overlay O {\n  constraints +=\n    - x\n}',
      [/already ADD their list items/], []],
    ['variant', 'prompt A {\n  constraints :=\n    - a\n}\nprompt B inherits A {\n  variant v {\n    constraints +=\n      - b\n  }\n}',
      [/variant "v" field "constraints"/, /this takes the PARENT's items/], []],
    ['env without parent', 'prompt A {\n  env prod {\n    constraints +=\n      - b\n  }\n}',
      [/env "prod" field "constraints"/, /Write the complete list for this env/], [/from\(/]],
  ];
  for (const [name, src, want, not] of cases) {
    const a = analyze(src);
    const msg = a.errors.find(e => e.includes("'+=' is not valid")) ?? '';
    assert.ok(msg, `${name}: expected an error, got ${JSON.stringify(a.errors)}`);
    for (const re of want) assert.match(msg, re, name);
    for (const re of not) assert.doesNotMatch(msg, re, name);
  }
});

test("'-=' is an error with no automatic replacement; on a scalar it is reported once", () => {
  let a = analyze('prompt A {\n  constraints :=\n    - a\n    - b\n}\nprompt B inherits A {\n  constraints -=\n    - b\n}');
  const msg = a.errors.find(e => e.includes("'-=' is not valid in v2"))!;
  assert.ok(msg);
  assert.match(msg, /no direct replacement/);
  assert.match(msg, /parent\[0\]\.constraints\[1\.\.3\]/);

  a = analyze('prompt A {\n  summary :=\n    x\n}\nprompt B inherits A {\n  summary -=\n    x\n}');
  assert.equal(a.errors.filter(e => e.includes("-=")).length, 1);
});

test("a bare ':' warns once, with the replacement", () => {
  let a = analyze('prompt A {\n  persona:\n    x\n}');
  assert.deepEqual(a.warnings, ['prompt "A" field "persona" uses \':\' — v2 uses \':=\'. Change "persona:" to "persona :="']);

  // new field on a child: still warns
  a = analyze('prompt A {\n  persona :=\n    x\n}\nprompt B inherits A {\n  objective:\n    y\n}');
  assert.ok(a.warnings.some(w => w.includes('Change "objective:" to "objective :="')));

  // redefining an inherited field: the specific message, and only that one
  a = analyze('prompt A {\n  persona :=\n    x\n}\nprompt B inherits A {\n  persona:\n    y\n}');
  assert.equal(a.warnings.length, 1);
  assert.match(a.warnings[0], /redefines inherited field "persona"/);
});

test("tags keep their inline colon syntax", () => {
  assert.deepEqual(analyze('prompt A {\n  tags: a, b\n  persona :=\n    x\n}').diags, []);
});

test("'extends' is reported (the CLI rejects it too)", () => {
  const a = analyze('prompt A {\n  persona :=\n    a\n}\nprompt B extends A {\n  persona :=\n    b\n}');
  assert.deepEqual(a.errors, [`'extends' is not valid — use 'inherits': "prompt B inherits A {"`]);
  // still understood as inheritance, so no follow-on "unknown prompt" noise
  assert.equal(a.errors.length, 1);
  assert.deepEqual(parseLoomDocument('prompt B extends A {\n}\n', 'x').nodes[0].parents, ['A']);
});
