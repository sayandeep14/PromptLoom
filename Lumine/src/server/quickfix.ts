import { Range, TextEdit } from 'vscode-languageserver-types';
import { LoomNode, FieldOp } from './parser';

/**
 * Mechanical fixes for v1 syntax. Pure text-in / edits-out so they can be tested
 * without a language client.
 *
 *   field:                     ->  field :=
 *   extends                    ->  inherits
 *   list += (child prompt)     ->  field := from(parent[0]) and { ... }
 *   list += (block / overlay / prompt without parent)  ->  field :=
 *
 * NOT offered (the meaning cannot be preserved mechanically, so the diagnostic's
 * message explains the manual rewrite instead): `-=`, `+=` on a scalar,
 * `+=` on `format` in a block/overlay, and `+=` inside variant / env blocks.
 */

export interface QuickFix {
  /** Same code as the diagnostic this fixes (see validator.LEGACY_CODES) */
  code: string;
  title: string;
  /** Where the diagnostic is reported (used to match the fix to a diagnostic) */
  anchor: Range;
  edits: TextEdit[];
}

function rng(sl: number, sc: number, el: number, ec: number): Range {
  return { start: { line: sl, character: sc }, end: { line: el, character: ec } };
}

function indentWidth(line: string): number {
  let n = 0;
  for (const c of line) { if (c === ' ') n++; else if (c === '\t') n += 2; else break; }
  return n;
}

const SCALARS = new Set(['summary', 'persona', 'context', 'objective', 'notes', 'kind']);

type Owner = 'prompt' | 'block' | 'overlay' | 'variant' | 'env';

export function computeQuickFixes(text: string, nodes: LoomNode[]): QuickFix[] {
  const lines = text.split(/\r?\n/);
  const eol = text.includes('\r\n') ? '\r\n' : '\n';
  const fixes: QuickFix[] = [];

  const declLineOf = (f: FieldOp): number => f.nameRange.start.line;

  const fixColon = (f: FieldOp, owner: Owner) => {
    const li = declLineOf(f);
    const line = lines[li] ?? '';
    const after = f.nameRange.end.character;
    const m = /^\s*:/.exec(line.slice(after));
    if (!m) return;
    const colonEnd = after + m[0].length;
    fixes.push({
      code: 'legacy-colon',
      title: `Change "${f.fieldName}:" to "${f.fieldName} :="`,
      anchor: f.nameRange,
      edits: [TextEdit.replace(rng(li, after, li, colonEnd), ' :=')],
    });
    void owner;
  };

  const fixAppend = (f: FieldOp, owner: Owner, parents: number) => {
    if (SCALARS.has(f.fieldName)) return;
    if (owner === 'variant' || owner === 'env') return;
    if ((owner === 'block' || owner === 'overlay') && f.fieldName === 'format') return;

    const li = declLineOf(f);
    const line = lines[li] ?? '';
    const after = f.nameRange.end.character;
    const m = /^\s*\+=/.exec(line.slice(after));
    if (!m) return;
    const opEnd = after + m[0].length;

    // block / overlay / prompt without a parent: '+=' -> ':='
    if (owner !== 'prompt' || parents === 0) {
      fixes.push({
        code: 'legacy-append',
        title: `Change "${f.fieldName} +=" to "${f.fieldName} :="`,
        anchor: f.nameRange,
        edits: [TextEdit.replace(rng(li, after, li, opEnd), ' :=')],
      });
      return;
    }

    // child prompt: wrap the items in `from(parent[...]) and { ... }`
    const declIndent = line.length - line.trimStart().length;
    const indentStr = line.slice(0, declIndent);
    let last = li;
    for (let j = li + 1; j < lines.length; j++) {
      const l = lines[j];
      if (l.trim() === '' || indentWidth(l) <= indentWidth(line)) break;
      last = j;
    }
    const src = parents > 1 ? 'parent[*]' : 'parent[0]';
    const body = lines.slice(li + 1, last + 1).map(l => '  ' + l);
    const replacement = [
      `${indentStr}${f.fieldName} :=`,
      `${indentStr}  from(${src}) and {`,
      ...body,
      `${indentStr}  }`,
    ].join(eol);
    fixes.push({
      code: 'legacy-append',
      title: `Convert "${f.fieldName} +=" to ":= from(${src}) and { … }"`,
      anchor: f.nameRange,
      edits: [TextEdit.replace(rng(li, 0, last, (lines[last] ?? '').length), replacement)],
    });
  };

  const visit = (fields: FieldOp[], owner: Owner, parents: number) => {
    for (const f of fields) {
      if (f.fieldName === 'tags') continue;
      if (f.op === ':') fixColon(f, owner);
      else if (f.op === '+=') fixAppend(f, owner, parents);
    }
  };

  for (const node of nodes) {
    if (node.legacyExtends) {
      const r = node.legacyExtends.range;
      fixes.push({
        code: 'legacy-extends',
        title: `Change "extends" to "inherits"`,
        anchor: r,
        edits: [TextEdit.replace(r, 'inherits')],
      });
    }
    const parents = node.parents.length;
    visit(node.fields, node.kind, parents);
    for (const v of node.variants) visit(v.fields, 'variant', parents);
    for (const e of node.envBlocks) visit(e.fields, 'env', parents);
  }

  return fixes;
}

/** Combined edit that applies every fix in the file at once. */
export function fixAllEdits(fixes: QuickFix[]): TextEdit[] {
  return fixes.flatMap(f => f.edits);
}
