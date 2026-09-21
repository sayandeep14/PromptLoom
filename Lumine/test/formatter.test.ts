import { test } from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { formatText } from '../src/server/formatter';
import { TESTDATA } from './helpers';

test('keeps everything the tree-based formatter used to delete', () => {
  const src = `// Team A reviewer
prompt A {
  persona :=
    a.
}

prompt B {
  persona :=
    b.
}

// The combined reviewer
prompt C inherits A, B {
  tags: go, review

  // keep in sync with A
  instructions :=
    from(parent[*]) and {
      - extra
    }

  env prod {
    constraints :=
      - No debug logging.
  }
}
`;
  assert.equal(formatText(src), src);
});

test('normalises operator spacing, trailing whitespace, tabs and blank lines', () => {
  const src = '\n\nprompt A {   \n\n\tpersona:=   \n\t\tx  \n\n\n\n  instructions :=\n    - a\n\n}\n\n\n';
  assert.equal(formatText(src), 'prompt A {\n  persona :=\n    x\n\n  instructions :=\n    - a\n}\n');
});

test('field values on the same line are kept', () => {
  assert.equal(formatText('prompt A {\n  kind:=   code-assistant\n}\n'), 'prompt A {\n  kind := code-assistant\n}\n');
});

test('keeps CRLF and ends with one newline', () => {
  assert.equal(formatText('prompt A {\r\n  persona:=\r\n    x\r\n}'), 'prompt A {\r\n  persona :=\r\n    x\r\n}\r\n');
  assert.equal(formatText(''), '');
});

test('legacy operators are spaced but never rewritten (that is the quick fix\'s job)', () => {
  assert.equal(formatText('prompt A {\n  instructions+=\n    - x\n}\n'), 'prompt A {\n  instructions +=\n    - x\n}\n');
});

test('contract keys are untouched', () => {
  const src = 'prompt A {\n  contract {\n    required_sections:\n      - Summary\n  }\n}\n';
  assert.equal(formatText(src), src);
});

// Property test over every fixture the Go suite uses: formatting must never lose a
// character other than whitespace, and must be idempotent.
test('lossless and idempotent on every Go fixture', () => {
  let checked = 0;
  const walk = (d: string) => {
    for (const e of fs.readdirSync(d, { withFileTypes: true })) {
      const p = path.join(d, e.name);
      if (e.isDirectory()) { walk(p); continue; }
      if (!e.name.endsWith('.loom')) continue;
      const text = fs.readFileSync(p, 'utf8');
      const once = formatText(text);
      assert.equal(once.replace(/\s+/g, ''), text.replace(/\s+/g, ''), `content changed: ${p}`);
      assert.equal(formatText(once), once, `not idempotent: ${p}`);
      checked++;
    }
  };
  walk(TESTDATA);
  assert.ok(checked > 50, `expected many fixtures, found ${checked}`);
});
