import { test } from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { DiagnosticSeverity } from 'vscode-languageserver-types';
import { parseLoomDocument } from '../src/server/parser';
import { LoomRegistry } from '../src/server/registry';
import { validateDocument } from '../src/server/validator';
import { parseToml } from '../src/server/toml-config';
import { QUIET, loomFiles, TESTDATA } from './helpers';

/**
 * The extension and `loom inspect` must agree. These tests run the extension over the
 * SAME fixtures the Go suite uses (testdata/), which are the source of truth.
 */

/** The fixture's own loom.toml when it has one (the CLI reads it), else the quiet test config. */
function configFor(dir: string) {
  const toml = path.join(dir, 'loom.toml');
  return fs.existsSync(toml) ? parseToml(fs.readFileSync(toml, 'utf8')) : QUIET;
}

function validateProject(dir: string): { errors: string[]; warnings: string[] } {
  const files = loomFiles(dir);
  const config = configFor(dir);
  const registry = new LoomRegistry();
  for (const [uri, text] of Object.entries(files)) registry.updateFile(uri, parseLoomDocument(text, uri).nodes, []);
  const errors: string[] = [];
  const warnings: string[] = [];
  for (const [uri, text] of Object.entries(files)) {
    registry.removeFile(uri);                       // like the server: validate against OTHER files
    const nodes = parseLoomDocument(text, uri).nodes;
    for (const d of validateDocument(nodes, text, uri, registry, config)) {
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

// Rules the extension does not reproduce yet, each with the reason. Everything else in
// testdata/invalid must produce the same diagnostics as `loom inspect`.
const KNOWN_GAPS: Record<string, string> = {};

// Weave-time rules need a renderer; the extension validates sources, it does not weave.
const isWeaveTime = (name: string) => name.startsWith('weave-');

const INVALID_ROOT = path.join(TESTDATA, 'invalid');
const FIXTURES = fs.readdirSync(INVALID_ROOT)
  .filter(d => fs.statSync(path.join(INVALID_ROOT, d)).isDirectory() && !isWeaveTime(d))
  .sort();

for (const name of FIXTURES) {
  test(`same diagnostics as the CLI: ${name}`, { skip: KNOWN_GAPS[name] ? `known gap: ${KNOWN_GAPS[name]}` : false }, () => {
    const dir = path.join(INVALID_ROOT, name);
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
