import { test } from 'node:test';
import assert from 'node:assert/strict';
import { analyze, applyEdits } from './helpers';
import { computeQuickFixes, fixAllEdits } from '../src/server/quickfix';
import { parseLoomDocument } from '../src/server/parser';

const fix = (src: string) => computeQuickFixes(src, parseLoomDocument(src, 'x').nodes);
const apply = (src: string) => applyEdits(src, fixAllEdits(fix(src)));

test('bare colon -> ":="', () => {
  const src = 'prompt A {\n  persona:\n    x\n\n  instructions:\n    - a\n}\n';
  assert.equal(apply(src), 'prompt A {\n  persona :=\n    x\n\n  instructions :=\n    - a\n}\n');
});

test("extends -> inherits", () => {
  const src = 'prompt A {\n  persona :=\n    a\n}\nprompt B extends A {\n  persona :=\n    b\n}\n';
  assert.equal(apply(src), src.replace('extends', 'inherits'));
});

test("'+=' in a child prompt -> from(parent[0]) and { ... }", () => {
  const src = [
    'prompt A {', '  instructions :=', '    - a', '}', '',
    'prompt B inherits A {',
    '  instructions +=',
    '    - one',
    '    - two',
    '',
    '  format :=',
    '    - Answer',
    '}', '',
  ].join('\n');
  const want = [
    'prompt A {', '  instructions :=', '    - a', '}', '',
    'prompt B inherits A {',
    '  instructions :=',
    '    from(parent[0]) and {',
    '      - one',
    '      - two',
    '    }',
    '',
    '  format :=',
    '    - Answer',
    '}', '',
  ].join('\n');
  assert.equal(apply(src), want);
});

test("'+=' with several parents uses parent[*]", () => {
  const src = 'prompt A {\n  instructions :=\n    - a\n}\nprompt B {\n  instructions :=\n    - b\n}\nprompt C inherits A, B {\n  instructions +=\n    - c\n}\n';
  assert.match(apply(src), /from\(parent\[\*\]\) and \{\n\s+- c\n\s+\}/);
});

test("'+=' in a block, overlay, or prompt without parent -> ':='", () => {
  for (const src of [
    'block B {\n  constraints +=\n    - x\n}\n',
    'overlay O {\n  instructions +=\n    - x\n}\n',
    'prompt P {\n  instructions +=\n    - x\n}\n',
  ]) {
    assert.equal(apply(src), src.replace('+=', ':='));
  }
});

test('fixes are NOT offered where the meaning cannot be preserved', () => {
  const notFixable = [
    'prompt A {\n  constraints :=\n    - a\n}\nprompt B inherits A {\n  constraints -=\n    - a\n}\n',            // -=
    'prompt A {\n  persona :=\n    a\n}\nprompt B inherits A {\n  persona +=\n    more\n}\n',                    // scalar +=
    'overlay O {\n  format +=\n    - x\n}\n',                                                                     // format in overlay
    'prompt A {\n  constraints :=\n    - a\n}\nprompt B inherits A {\n  variant v {\n    constraints +=\n      - b\n  }\n}\n', // variant
    'prompt A {\n  env prod {\n    constraints +=\n      - b\n  }\n}\n',                                           // env
  ];
  for (const src of notFixable) {
    assert.equal(fix(src).length, 0, src);
    // ...but the problem is still reported
    assert.ok(analyze(src).errors.length > 0, src);
  }
});

test('after fix-all, the fixable cases validate clean', () => {
  const src = [
    'block B {', '  constraints +=', '    - x', '}', '',
    'prompt A {', '  persona:', '    a', '  instructions:', '    - a', '}', '',
    'prompt C extends A {', '  instructions +=', '    - c', '}', '',
  ].join('\n');
  const fixed = apply(src);
  const a = analyze(fixed);
  assert.deepEqual(a.diags.map(d => d.message), []);
  // idempotent: nothing left to fix
  assert.equal(fix(fixed).length, 0);
  assert.equal(apply(fixed), fixed);
});

test('each fix is anchored on the diagnostic it resolves and shares its code', () => {
  const src = 'prompt A {\n  persona:\n    x\n}\n';
  const a = analyze(src);
  const f = fix(src)[0];
  const d = a.diags.find(x => x.code === f.code)!;
  assert.ok(d);
  assert.deepEqual(f.anchor, d.range);
});

test('CRLF files keep their line endings', () => {
  const src = 'prompt A {\r\n  instructions +=\r\n    - x\r\n}\r\n';
  const out = apply(src);
  assert.equal(out, 'prompt A {\r\n  instructions :=\r\n    - x\r\n}\r\n');
});

// Cross-check against the real CLI: whatever the quick fixes produce must pass `loom inspect`.
import { spawnSync } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

test('fixed output is accepted by `loom inspect` (skipped without a Go toolchain)', (t) => {
  const repo = path.join(__dirname, '..', '..');
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'lumine-xcheck-'));
  const bin = path.join(tmp, 'loom');
  const build = spawnSync('go', ['build', '-o', bin, './cmd/loom'], { cwd: repo });
  if (build.error || build.status !== 0) { t.skip('cannot build loom'); return; }

  const src = [
    'block Rules {', '  constraints +=', '    - Be safe.', '}', '',
    'prompt Base {', '  persona:', '    You review code.', '  instructions:', '    - Read the diff.', '}', '',
    'prompt Child extends Base {', '  use Rules', '  instructions +=', '    - Check errors.', '    - Suggest tests.', '}', '',
  ].join('\n');

  const project = path.join(tmp, 'proj');
  fs.mkdirSync(path.join(project, 'prompts'), { recursive: true });
  fs.writeFileSync(path.join(project, 'loom.toml'),
    '[project]\nname="x"\n[paths]\nprompts="prompts"\nblocks="blocks"\noverlays="overlays"\nout="dist"\n' +
    '[validation]\nrequire_objective=false\nrequire_format=false\n');

  // before: the CLI rejects the v1 source
  fs.writeFileSync(path.join(project, 'prompts', 'a.prompt.loom'), src);
  assert.notEqual(spawnSync(bin, ['inspect'], { cwd: project }).status, 0);

  // after: quick fixes make it pass, and the woven prompt keeps every rule
  fs.writeFileSync(path.join(project, 'prompts', 'a.prompt.loom'), apply(src));
  const inspect = spawnSync(bin, ['inspect'], { cwd: project, encoding: 'utf8' });
  assert.equal(inspect.status, 0, inspect.stdout + inspect.stderr);
  assert.doesNotMatch(inspect.stdout, /deprecated|uses ':'|not valid in v2/);

  const weave = spawnSync(bin, ['weave', 'Child', '--stdout'], { cwd: project, encoding: 'utf8' });
  assert.equal(weave.status, 0, weave.stderr);
  for (const item of ['Read the diff.', 'Check errors.', 'Suggest tests.', 'Be safe.']) {
    assert.match(weave.stdout, new RegExp(item.replace('.', '\\.')));
  }
  fs.rmSync(tmp, { recursive: true, force: true });
});
