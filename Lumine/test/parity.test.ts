import { test } from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { DiagnosticSeverity } from 'vscode-languageserver-types';
import { parseLoomDocument } from '../src/server/parser';
import { LoomRegistry } from '../src/server/registry';
import { validateDocument } from '../src/server/validator';
import { QUIET, loomFiles, TESTDATA } from './helpers';

/**
 * The extension and `loom inspect` must agree. These tests run the extension over the
 * SAME fixtures the Go suite uses (testdata/), which are the source of truth.
 */

function validateProject(dir: string): { errors: string[]; warnings: string[] } {
  const files = loomFiles(dir);
  const registry = new LoomRegistry();
  for (const [uri, text] of Object.entries(files)) registry.updateFile(uri, parseLoomDocument(text, uri).nodes, []);
  const errors: string[] = [];
  const warnings: string[] = [];
  for (const [uri, text] of Object.entries(files)) {
    registry.removeFile(uri);                       // like the server: validate against OTHER files
    const nodes = parseLoomDocument(text, uri).nodes;
    for (const d of validateDocument(nodes, text, uri, registry, QUIET)) {
      (d.severity === DiagnosticSeverity.Error ? errors : warnings).push(d.message);
    }
    registry.updateFile(uri, nodes, []);
  }
  return { errors, warnings };
}

function readExpect(dir: string): { kind: string; text: string }[] {
  return fs.readFileSync(path.join(dir, 'expect.txt'), 'utf8').split('\n')
    .map(l => l.trim()).filter(l => l && !l.startsWith('#'))
    .map(l => { const i = l.indexOf(':'); return { kind: l.slice(0, i).trim(), text: l.slice(i + 1).trim() }; });
}

// Fixtures whose diagnostics the extension is expected to reproduce (v1-syntax policy).
const SYNTAX_FIXTURES = [
  'error-append-removed', 'error-append-no-parent', 'error-append-scalar', 'error-append-in-block',
  'error-append-in-variant', 'error-append-in-env', 'error-remove-removed', 'remove-on-scalar',
  'warn-bare-colon', 'warn-redefine-inherited', 'parse-extends',
];

for (const name of SYNTAX_FIXTURES) {
  test(`same diagnostics as the CLI: ${name}`, () => {
    const dir = path.join(TESTDATA, 'invalid', name);
    const { errors, warnings } = validateProject(dir);
    const all = [...errors, ...warnings].join('\n');
    for (const e of readExpect(dir)) {
      if (e.kind === 'error' || e.kind === 'load-error') {
        assert.ok(errors.some(m => m.includes(e.text)), `missing error containing "${e.text}"\n${all}`);
      } else if (e.kind === 'warning') {
        assert.ok(warnings.some(m => m.includes(e.text)), `missing warning containing "${e.text}"\n${all}`);
      } else if (e.kind === 'also') {
        assert.ok(all.includes(e.text), `missing "${e.text}"\n${all}`);
      }
    }
  });
}

test('no false positives: every valid Go fixture is clean of errors and v1-syntax warnings', () => {
  const root = path.join(TESTDATA, 'valid');
  let n = 0;
  for (const name of fs.readdirSync(root).filter(d => fs.statSync(path.join(root, d)).isDirectory())) {
    const { errors, warnings } = validateProject(path.join(root, name));
    assert.deepEqual(errors, [], `${name}: errors`);
    assert.deepEqual(warnings.filter(w => w.includes("uses ':'") || w.includes("instead of ':='")), [], `${name}: v1 warnings`);
    n++;
  }
  assert.ok(n >= 15);
});
