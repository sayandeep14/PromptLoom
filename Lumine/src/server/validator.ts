import { Diagnostic, DiagnosticSeverity, Range } from 'vscode-languageserver-types';
import { LoomNode, FieldOp } from './parser';
import { LoomRegistry } from './registry';
import { LoomConfig, DEFAULT_CONFIG } from './toml-config';
import { findSyntaxError } from './syntax';
import { parseFromExpression } from './fromexpr';
import * as path from 'path';
import { fileURLToPath } from 'url';

// ─── Constants ────────────────────────────────────────────────────────────────

const SCALAR_FIELDS = new Set([
  'summary', 'persona', 'context', 'objective', 'notes', 'kind',
]);

const ALL_VALID_FIELDS = new Set([
  'summary', 'persona', 'context', 'objective', 'notes', 'kind',
  'instructions', 'constraints', 'examples', 'format', 'todo', 'compatible_with',
  'required_sections', 'forbidden_sections', 'must_include', 'must_not_include',
  'allowed', 'forbidden',
]);

const BODY_KEYWORDS = new Set([
  'use', 'var', 'slot', 'variant', 'contract', 'capabilities', 'env', 'tags',
  'prompt', 'block', 'overlay', 'inherits', 'required', 'default',
]);

// ─── Helpers ──────────────────────────────────────────────────────────────────

function rng(sl: number, sc: number, el: number, ec: number): Range {
  return { start: { line: sl, character: sc }, end: { line: el, character: ec } };
}

function mkError(range: Range, message: string): Diagnostic {
  return { severity: DiagnosticSeverity.Error, range, message, source: 'lumine' };
}

function mkWarning(range: Range, message: string): Diagnostic {
  return { severity: DiagnosticSeverity.Warning, range, message, source: 'lumine' };
}

function basename(uri: string): string {
  try { return path.basename(fileURLToPath(uri)); } catch { return uri.split('/').pop() ?? uri; }
}

function levenshtein(a: string, b: string): number {
  const m = a.length, n = b.length;
  const dp: number[][] = Array.from({ length: m + 1 }, (_, i) => [i, ...Array(n).fill(0)]);
  for (let j = 0; j <= n; j++) dp[0][j] = j;
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      dp[i][j] = a[i - 1] === b[j - 1]
        ? dp[i - 1][j - 1]
        : 1 + Math.min(dp[i - 1][j - 1], dp[i][j - 1], dp[i - 1][j]);
    }
  }
  return dp[m][n];
}

function closestMatch(name: string, candidates: string[]): string | undefined {
  let best: string | undefined;
  let bestDist = Infinity;
  const lower = name.toLowerCase();
  for (const c of candidates) {
    const d = levenshtein(lower, c.toLowerCase());
    if (d < bestDist && d <= 3) { bestDist = d; best = c; }
  }
  return best;
}

function indentOf(line: string): number {
  let n = 0;
  for (const c of line) { if (c === ' ') n++; else if (c === '\t') n += 2; else break; }
  return n;
}

// Extract the local name from a qualified cross-pack reference (e.g. "python-backend.Engineer" → "Engineer").
// Returns undefined when the name has no dot (already a local name).
function localNameOf(qualified: string): string | undefined {
  const dot = qualified.lastIndexOf('.');
  return dot >= 0 ? qualified.slice(dot + 1) : undefined;
}

// Look up a prompt by name, falling back to the local part of a qualified name.
function resolvePrompt(
  name: string,
  registry: LoomRegistry,
  seenInFile: Map<string, LoomNode>,
): boolean {
  if (registry.lookupPrompt(name) ?? seenInFile.get(name)) return true;
  const local = localNameOf(name);
  if (local && (registry.lookupPrompt(local) ?? seenInFile.get(local))) return true;
  return false;
}

// Look up a block by name, falling back to the local part of a qualified name.
function resolveBlock(
  name: string,
  registry: LoomRegistry,
  seenInFile: Map<string, LoomNode>,
): boolean {
  if (registry.lookupBlock(name) ?? [...seenInFile.values()].find(n => n.kind === 'block' && n.name === name)) return true;
  const local = localNameOf(name);
  if (local) {
    if (registry.lookupBlock(local) ?? [...seenInFile.values()].find(n => n.kind === 'block' && n.name === local)) return true;
  }
  return false;
}

// ─── Main Validator ───────────────────────────────────────────────────────────
// The checks and messages below mirror internal/validate (`loom inspect`); the parity test
// runs them over the same fixtures as the Go suite.

const LIST_FIELDS = new Set(['instructions', 'constraints', 'examples', 'format', 'todo', 'compatible_with']);
/** Same set as ast.ValidFields in Go: the fields a prompt, block or overlay may declare. */
const VALID_FIELDS = new Set([...SCALAR_FIELDS, ...LIST_FIELDS]);

function lineRange(text: string, line: number): Range {
  const l = text.split('\n')[line] ?? '';
  return rng(line, 0, line, l.replace(/\r$/, '').length);
}

export function validateDocument(
  nodes: LoomNode[],
  text: string,
  uri: string,
  registry: LoomRegistry,
  config: LoomConfig = DEFAULT_CONFIG,
): Diagnostic[] {
  // Like the CLI, nothing else is checked until load errors are fixed.
  const syn = findSyntaxError(text);
  if (syn) return [{ ...mkError(lineRange(text, syn.line), syn.message), code: 'syntax' }];

  const diags: Diagnostic[] = [];
  const ctx = makeContext(nodes, uri, registry);

  checkDuplicateNames(nodes, uri, registry, diags);

  for (const node of nodes) {
    if (node.kind === 'prompt') checkPrompt(node, ctx, config, diags);
    else checkBlockOrOverlay(node, ctx, diags);
  }

  // v1 syntax: +=  -=  bare ':'  extends
  checkLegacySyntax(nodes, text, registry, diags);

  validateVarTokens(text, nodes, registry, diags);
  return diags;
}

// ─── Context: nodes of this file + the rest of the workspace ──────────────────

interface Ctx {
  uri: string;
  registry: LoomRegistry;
  inFile: Map<string, LoomNode>;
  prompt(name: string): LoomNode | undefined;
  block(name: string): LoomNode | undefined;
  promptNames(): string[];
  blockNames(): string[];
}

function makeContext(nodes: LoomNode[], uri: string, registry: LoomRegistry): Ctx {
  const inFile = new Map<string, LoomNode>();
  for (const n of nodes) if (!inFile.has(`${n.kind}:${n.name}`)) inFile.set(`${n.kind}:${n.name}`, n);
  const find = (kind: 'prompt' | 'block', name: string): LoomNode | undefined => {
    const direct = inFile.get(`${kind}:${name}`) ?? (kind === 'prompt' ? registry.lookupPrompt(name) : registry.lookupBlock(name))?.node;
    if (direct) return direct;
    const local = localNameOf(name);
    return local ? (inFile.get(`${kind}:${local}`) ?? (kind === 'prompt' ? registry.lookupPrompt(local) : registry.lookupBlock(local))?.node) : undefined;
  };
  return {
    uri, registry, inFile,
    prompt: n => find('prompt', n),
    block: n => find('block', n),
    promptNames: () => [...new Set([...registry.allPromptNames(), ...nodes.filter(n => n.kind === 'prompt').map(n => n.name)])],
    blockNames: () => [...new Set([...registry.allBlockNames(), ...nodes.filter(n => n.kind === 'block').map(n => n.name)])],
  };
}

function checkDuplicateNames(nodes: LoomNode[], uri: string, registry: LoomRegistry, diags: Diagnostic[]): void {
  const first = new Map<string, LoomNode>();
  for (const node of nodes) {
    const key = `${node.kind}:${node.name}`;
    const prev = first.get(key);
    if (prev) {
      diags.push(mkError(node.nameRange,
        `duplicate ${node.kind} name "${node.name}" (first defined at ${basename(uri)}:${prev.nameRange.start.line + 1})`));
      continue;
    }
    first.set(key, node);
    const other =
      node.kind === 'prompt' ? registry.lookupPrompt(node.name) :
      node.kind === 'block'  ? registry.lookupBlock(node.name) :
                               registry.lookupOverlay(node.name);
    if (other) {
      diags.push(mkError(node.nameRange,
        `duplicate ${node.kind} name "${node.name}" (first defined at ${basename(other.uri)}:${other.node.nameRange.start.line + 1})`));
    }
  }
}

// ─── Prompts ──────────────────────────────────────────────────────────────────

function didYouMean(name: string, candidates: string[]): string {
  if (name.includes('.')) return '';
  const best = closestMatch(name, candidates);
  return best ? `\n  Did you mean "${best}"?` : '';
}

/** "A -> B -> C -> A" when a cycle is reachable from name, else "". */
function detectCycle(name: string, ctx: Ctx): string {
  const onPath = new Set<string>();
  const visited = new Set<string>();
  const path: string[] = [];
  const dfs = (cur: string): string => {
    if (onPath.has(cur)) return [...path.slice(path.indexOf(cur)), cur].join(' -> ');
    if (visited.has(cur)) return '';
    onPath.add(cur); path.push(cur);
    for (const p of ctx.prompt(cur)?.parents ?? []) {
      const r = dfs(p);
      if (r) return r;
    }
    path.pop(); onPath.delete(cur); visited.add(cur);
    return '';
  };
  return dfs(name);
}

/** Longest chain of ancestors above name (0 for a prompt with no parent). */
function inheritanceDepth(name: string, ctx: Ctx, seen = new Set<string>()): number {
  if (seen.has(name)) return 0;
  seen.add(name);
  let max = 0;
  for (const p of ctx.prompt(name)?.parents ?? []) max = Math.max(max, 1 + inheritanceDepth(p, ctx, new Set(seen)));
  return max;
}

function hasInheritedField(node: LoomNode, field: string, ctx: Ctx): boolean {
  const seen = new Set<string>([node.name]);
  const walk = (cur: LoomNode): boolean => {
    for (const p of cur.parents) {
      if (seen.has(p)) continue;
      seen.add(p);
      const pn = ctx.prompt(p);
      if (pn && (pn.fields.some(f => f.fieldName === field) || walk(pn))) return true;
    }
    return false;
  };
  return walk(node);
}

const TOKEN_RE = /\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g;

function tokensOf(fields: FieldOp[]): string[] {
  const out: string[] = [];
  for (const f of fields) {
    for (const line of f.value) {
      TOKEN_RE.lastIndex = 0;
      let m: RegExpExecArray | null;
      while ((m = TOKEN_RE.exec(line)) !== null) out.push(m[1].trim());
    }
  }
  return out;
}

function kindTagOf(node: LoomNode): string {
  return node.fields.find(f => f.fieldName === 'kind')?.value[0]?.trim() ?? '';
}

function checkPrompt(node: LoomNode, ctx: Ctx, config: LoomConfig, diags: Diagnostic[]): void {
  const label = `prompt "${node.name}"`;
  const vc = config.validation;

  // unknown parents
  node.parents.forEach((parent, i) => {
    if (!ctx.prompt(parent)) {
      diags.push(mkError(node.parentRanges[i] ?? node.nameRange,
        `${label} inherits unknown prompt "${parent}"${didYouMean(parent, ctx.promptNames())}`));
    }
  });

  // unknown blocks
  for (const ref of node.uses) {
    if (!ctx.block(ref.name)) {
      diags.push(mkError(ref.range, `${label} uses unknown block "${ref.name}"${didYouMean(ref.name, ctx.blockNames())}`));
    }
  }

  // inheritance cycle
  const cycle = detectCycle(node.name, ctx);
  if (cycle) diags.push(mkError(node.nameRange, `inheritance cycle detected: ${cycle}`));

  // field names
  for (const f of node.fields) {
    if (f.fieldName === 'tags') {
      if (f.op !== ':') {
        diags.push(mkWarning(f.nameRange, `${label}: use \`tags: value1, value2\` inline syntax for tags — operator syntax (${f.op}) is not supported`));
      }
      continue;
    }
    if (!VALID_FIELDS.has(f.fieldName)) diags.push(unknownField(label, f, ''));
  }

  // duplicate var/slot names
  const seenVars = new Map<string, number>();
  for (const v of node.vars) {
    const prev = seenVars.get(v.name);
    if (prev !== undefined) {
      diags.push(mkError(v.nameRange, `${label} declares "${v.name}" more than once (first declared at ${basename(ctx.uri)}:${prev + 1})`));
    } else {
      seenVars.set(v.name, v.nameRange.start.line);
    }
  }

  // duplicate variants (+ unknown fields inside variants)
  const seenVariants = new Map<string, number>();
  for (const v of node.variants) {
    const prev = seenVariants.get(v.name);
    if (prev !== undefined) {
      diags.push(mkError(v.nameRange, `${label} declares variant "${v.name}" more than once (first declared at ${basename(ctx.uri)}:${prev + 1})`));
      continue;
    }
    seenVariants.set(v.name, v.nameRange.start.line);
    for (const f of v.fields) {
      if (!VALID_FIELDS.has(f.fieldName)) diags.push(unknownField(label, f, `variant "${v.name}" `));
    }
  }

  // -= on a scalar
  checkScalarRemove('prompt', node.name, node.fields, diags);
  for (const v of node.variants) checkScalarRemove('variant', v.name, v.fields, diags);
  for (const e of node.envBlocks) checkScalarRemove('env', e.name, e.fields, diags);

  // from() expressions (prompt body, variants, env blocks)
  checkFromExpressions(node, [...node.fields, ...node.variants.flatMap(v => v.fields), ...node.envBlocks.flatMap(e => e.fields)], diags);

  // required fields (configurable)
  const has = (f: string) => node.fields.some(x => x.fieldName === f) || hasInheritedField(node, f, ctx);
  if (vc.require_objective && !has('objective')) diags.push(mkWarning(node.nameRange, `${label} has no objective field`));
  if (vc.require_format && !has('format')) diags.push(mkWarning(node.nameRange, `${label} has no output format field`));
  if (vc.require_contract && !node.contract) diags.push(mkWarning(node.nameRange, `${label} has no contract block`));

  if (vc.warn_on_empty_context) {
    for (const f of node.fields) {
      if (f.fieldName === 'context' && f.value.length === 0) diags.push(mkWarning(f.nameRange, `${label} has an empty context field`));
    }
  }

  if (vc.warn_on_deep_inheritance) {
    const depth = inheritanceDepth(node.name, ctx);
    if (depth > vc.max_inheritance_depth) {
      diags.push(mkWarning(node.nameRange,
        `${label} has inheritance depth ${depth} (max ${vc.max_inheritance_depth}); consider using blocks instead of deep inheritance`));
    }
  }

  // a prompt's own list replaces the items its blocks add to it
  for (const f of node.fields) {
    if (!LIST_FIELDS.has(f.fieldName)) continue;
    for (const ref of node.uses) {
      const blk = ctx.block(ref.name);
      if (blk && blk.fields.some(b => b.fieldName === f.fieldName)) {
        diags.push(mkWarning(f.nameRange,
          `${label} writes "${f.fieldName}", which replaces the items block "${ref.name}" adds to it (a prompt's own list replaces what its blocks contribute).\n` +
          `  Copy the block's items into this prompt, move this prompt's items into the block, or remove "${f.fieldName}" here to keep the block's.`));
      }
    }
  }

  // kind mismatch between prompt and block
  const promptKind = kindTagOf(node);
  if (promptKind) {
    for (const ref of node.uses) {
      const blk = ctx.block(ref.name);
      const blockKind = blk ? kindTagOf(blk) : '';
      if (blockKind && blockKind !== promptKind) {
        diags.push(mkWarning(node.nameRange,
          `${label} (kind: ${promptKind}) uses block "${ref.name}" (kind: ${blockKind}) — kind mismatch may indicate a misapplied block`));
      }
    }
  }

  // slots a value must be supplied for
  const used = new Set(tokensOf([...node.fields, ...node.variants.flatMap(v => v.fields)]));
  const runtime = node.vars.filter(v => v.required && used.has(v.name)).map(v => v.name).sort();
  if (runtime.length > 0) diags.push(mkWarning(node.nameRange, `${label} requires runtime values for: ${runtime.join(', ')}`));

  // tokens a used block relies on but this prompt does not declare
  const declared = new Set([...node.vars.map(v => v.name), ...ctx.registry.allGlobalVars().map(v => v.name)]);
  for (const ref of node.uses) {
    const blk = ctx.block(ref.name);
    if (!blk) continue;
    for (const token of new Set(tokensOf(blk.fields))) {
      if (!declared.has(token)) diags.push(mkWarning(ref.range, `Block "${ref.name}" uses {{ ${token} }} which is not declared in "${node.name}"`));
    }
  }

  for (const v of node.vars) {
    if (v.isSlot && !v.required && !v.default) {
      diags.push(mkWarning(v.nameRange, `Slot "${v.name}" has no default and is not required — it will render as empty if not provided`));
    }
  }
}

function unknownField(label: string, f: FieldOp, where: string): Diagnostic {
  const hint = closestMatch(f.fieldName, [...VALID_FIELDS]);
  return mkError(f.nameRange,
    `${label} ${where}uses unknown field "${f.fieldName}"${hint ? `\n  Did you mean "${hint}"?` : ''}`);
}

// ─── Blocks and overlays ──────────────────────────────────────────────────────

function checkBlockOrOverlay(node: LoomNode, _ctx: Ctx, diags: Diagnostic[]): void {
  const label = `${node.kind} "${node.name}"`;
  for (const f of node.fields) {
    if (f.fieldName === 'tags') {
      if (f.op !== ':') diags.push(mkWarning(f.nameRange, `${label}: use \`tags: value1, value2\` inline syntax for tags — operator syntax (${f.op}) is not supported`));
      continue;
    }
    if (!VALID_FIELDS.has(f.fieldName)) diags.push(unknownField(label, f, ''));
    if (node.kind === 'block' && f.fromExprRaw !== undefined) {
      diags.push(mkError(f.nameRange, `${label} field "${f.fieldName}": from() expressions are not valid in blocks — blocks have no parents`));
    }
  }
  checkScalarRemove(node.kind, node.name, node.fields, diags);
}

// ─── from() expressions ───────────────────────────────────────────────────────

function checkFromExpressions(node: LoomNode, fields: FieldOp[], diags: Diagnostic[]): void {
  const label = `prompt "${node.name}"`;
  const parents = node.parents.length;

  for (const f of fields) {
    if (f.fromExprRaw === undefined) continue;
    let expr;
    try {
      expr = parseFromExpression(f.value);
    } catch (e) {
      diags.push(mkError(f.nameRange, `in from() expression for "${f.fieldName}": ${(e as Error).message}`));
      continue;
    }
    const isScalar = SCALAR_FIELDS.has(f.fieldName);
    const at = f.nameRange;
    const field = `${label} field "${f.fieldName}"`;

    // A scalar holds one value: `and` would join several, and a literal block is a list.
    if (isScalar && expr.units.length > 1) {
      diags.push(mkError(at, `${field}: 'and' cannot be used on scalar fields — a scalar takes exactly one value; use a single from(parent[N])`));
    } else if (isScalar && expr.units.length === 1 && expr.units[0].kind === 'literal') {
      diags.push(mkError(at, `${field}: a { - item } block is a list and cannot be used on scalar fields — write the text directly`));
    }

    for (const unit of expr.units) {
      if (unit.kind === 'parentRef') {
        const s = unit.sub;
        if (s.kind === 'all' && isScalar) {
          diags.push(mkError(at, `${field}: from(parent[*]) cannot be used on scalar fields — use from(parent[N]) to select one specific parent`));
        } else if (s.kind === 'index' && s.n >= parents) {
          diags.push(mkError(at, `${field}: parent[${s.n}] is out of range — prompt has ${parents} parent(s) (indices are 0-based)`));
        } else if (s.kind === 'range' && (s.n >= parents || s.m > parents)) {
          diags.push(mkError(at, `${field}: parent[${s.n}..${s.m}] is out of range — prompt has ${parents} parent(s)`));
        }
      } else if (unit.kind === 'namedRef') {
        if (!node.parents.includes(unit.name)) {
          diags.push(mkError(at, `${field}: from(${unit.name}) references "${unit.name}" which is not a declared parent — only declared parents may appear in from() expressions`));
        }
      } else if (unit.kind === 'fieldRef') {
        if (unit.field !== '' && !VALID_FIELDS.has(unit.field)) {
          diags.push(mkError(at, `${field}: from() references unknown field "${unit.field}"`));
        }
        if (unit.source.kind === 'index' && unit.source.n >= parents) {
          diags.push(mkError(at, `${field}: parent[${unit.source.n}] is out of range in from() expression — prompt has ${parents} parent(s)`));
        }
      }
    }
  }
}

// ─── v1 syntax ────────────────────────────────────────────────────────────────
// v2 has exactly ONE field operator, ':='. The messages below match `loom inspect`
// (internal/validate) word for word, so editor and CLI never disagree.

/** Diagnostic codes, also used by the quick fixes. */
export const LEGACY_CODES = {
  append:  'legacy-append',
  remove:  'legacy-remove',
  colon:   'legacy-colon',
  extends: 'legacy-extends',
} as const;

type BodyKind = 'prompt' | 'block' | 'overlay' | 'variant' | 'env';

function appendMessage(kind: BodyKind, name: string, field: string, isScalar: boolean, parents: number): string {
  const head = `${kind} "${name}" field "${field}": '+=' is not valid in v2 (the only operator is ':=').`;
  if (kind === 'variant' || kind === 'env') {
    if (isScalar) {
      return `${head}\n  A scalar field cannot be appended to; replace its value with ':=' and write the full text.`;
    }
    if (parents === 0) {
      return `${head}\n  Write the complete list for this ${kind} with ':=':\n    ${field} :=\n      - your item`;
    }
    return `${head}\n  Write the complete list with ':=', or start from the parent's list ` +
      `(note: this takes the PARENT's items, not this prompt's own):\n    ${field} :=\n      from(parent[0]) and {\n        - your item\n      }`;
  }
  if (kind !== 'prompt') {
    if (isScalar) {
      return `${head}\n  A scalar field cannot be appended to; replace its value with ':=' and write the full text.`;
    }
    return `${head}\n  Blocks and overlays already ADD their list items to the prompt, so write:\n    ${field} :=\n      - your item`;
  }
  if (isScalar) {
    return `${head}\n  A scalar field cannot be appended to; replace its value with ':=' and write the full text\n` +
      `  (or copy the parent's with  ${field} :=\n    from(parent[0])  and edit from there).`;
  }
  if (parents === 0) {
    return `${head}\n  This prompt has no parent to append to. Write the whole list with ':=':\n    ${field} :=\n      - your item`;
  }
  const src = parents > 1 ? 'parent[*]' : 'parent[0]';
  const note = parents > 1 ? ' (use from(parent[N]) to take just one parent\'s items)' : '';
  return `${head}\n  To extend the inherited list, write:${note}\n    ${field} :=\n      from(${src}) and {\n        - your item\n      }`;
}

function checkLegacyFields(
  kind: BodyKind,
  name: string,
  fields: FieldOp[],
  parents: number,
  inherited: Set<string>,
  diags: Diagnostic[],
): void {
  for (const f of fields) {
    if (f.fieldName === 'tags') continue; // tags use their own inline syntax
    const isScalar = SCALAR_FIELDS.has(f.fieldName);
    if (f.op === '+=') {
      diags.push({ ...mkError(f.nameRange, appendMessage(kind, name, f.fieldName, isScalar, parents)), code: LEGACY_CODES.append });
    } else if (f.op === '-=') {
      if (isScalar) continue; // reported by the dedicated scalar rule
      diags.push({
        ...mkError(f.nameRange,
          `${kind} "${name}" field "${f.fieldName}": '-=' is not valid in v2 and has no direct replacement.\n` +
          `  Write the list you want with ':=' instead. To keep only some parent items, select them:\n` +
          `    ${f.fieldName} :=\n      parent[0].${f.fieldName}[1..3] and {\n        - an extra item\n      }`),
        code: LEGACY_CODES.remove,
      });
    } else if (f.op === ':') {
      const message = inherited.has(f.fieldName)
        ? `${kind} "${name}" redefines inherited field "${f.fieldName}" with ':' instead of ':=' — use an explicit operator to clarify intent`
        : `${kind} "${name}" field "${f.fieldName}" uses ':' — v2 uses ':='. Change "${f.fieldName}:" to "${f.fieldName} :="`;
      diags.push({ ...mkWarning(f.nameRange, message), code: LEGACY_CODES.colon });
    }
  }
}

/** Field names defined by any ancestor. Parents may live in this file OR in another one. */
function ancestorFieldNames(node: LoomNode, registry: LoomRegistry, inFile: Map<string, LoomNode>): Set<string> {
  const out = new Set<string>();
  if (node.kind !== 'prompt') return out;
  const seen = new Set<string>([node.name]);
  const queue = [...node.parents];
  while (queue.length > 0) {
    const name = queue.shift()!;
    if (seen.has(name)) continue;
    seen.add(name);
    const local = localNameOf(name);
    const found =
      inFile.get(name) ?? registry.lookupPrompt(name)?.node ??
      (local ? inFile.get(local) ?? registry.lookupPrompt(local)?.node : undefined);
    if (!found || found.kind !== 'prompt') continue;
    for (const f of found.fields) out.add(f.fieldName);
    queue.push(...found.parents);
  }
  return out;
}

function checkLegacySyntax(nodes: LoomNode[], text: string, registry: LoomRegistry, diags: Diagnostic[]): void {
  const inFile = new Map<string, LoomNode>();
  for (const n of nodes) if (!inFile.has(n.name)) inFile.set(n.name, n);
  const lines = text.split(/\r?\n/);

  for (const node of nodes) {
    if (node.legacyExtends) {
      const declLine = (lines[node.legacyExtends.range.start.line] ?? '').trim();
      diags.push({
        ...mkError(
          node.legacyExtends.range,
          `'extends' is not valid — use 'inherits': "${declLine.replace('extends', 'inherits')}"`,
        ),
        code: LEGACY_CODES.extends,
      });
    }
    const parents = node.parents.length;
    const inherited = ancestorFieldNames(node, registry, inFile);
    checkLegacyFields(node.kind, node.name, node.fields, parents, inherited, diags);
    for (const v of node.variants) checkLegacyFields('variant', v.name, v.fields, parents, new Set(), diags);
    for (const e of node.envBlocks) checkLegacyFields('env', e.name, e.fields, parents, new Set(), diags);
    // contract / capabilities blocks legitimately use `key:` — never checked here.
  }
}

// ─── Sub-checks ───────────────────────────────────────────────────────────────

function checkScalarRemove(kind: string, name: string, fields: FieldOp[], diags: Diagnostic[]): void {
  for (const f of fields) {
    if (f.op === '-=' && SCALAR_FIELDS.has(f.fieldName)) {
      diags.push(mkError(
        f.nameRange,
        `${kind} "${name}": operator '-=' is not supported on scalar field "${f.fieldName}"`,
      ));
    }
  }
}

/**
 * `{{ token }}` placeholders. In a prompt an undeclared one is an error; blocks and overlays
 * cannot declare variables, so any placeholder there is a warning (the consuming prompt must
 * declare it). Scans the raw text so the diagnostic points at the token itself.
 */
function validateVarTokens(
  text: string,
  nodes: LoomNode[],
  registry: LoomRegistry,
  diags: Diagnostic[],
): void {
  if (!text.includes('{{')) return;

  const lines = text.split('\n');
  const varPattern = /\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g;
  const globalVarNames = new Set(registry.allGlobalVars().map(v => v.name));

  for (let lineIdx = 0; lineIdx < lines.length; lineIdx++) {
    const line = lines[lineIdx];
    if (!line.includes('{{') || line.trim().startsWith('//')) continue;

    const node = nodes.find(n => n.bodyRange.start.line <= lineIdx && lineIdx < n.range.end.line);
    if (!node) continue;

    const local = new Set([...node.vars.map(v => v.name), ...globalVarNames]);

    varPattern.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = varPattern.exec(line)) !== null) {
      const name = m[1].trim();
      const range = rng(lineIdx, m.index, lineIdx, m.index + m[0].length);
      if (node.kind !== 'prompt') {
        diags.push(mkWarning(range, `${node.kind} "${node.name}" uses {{ ${name} }} — variables must be declared in the consuming prompt`));
      } else if (!local.has(name)) {
        diags.push(mkError(range, `prompt "${node.name}" references undeclared variable "${name}"\n  Declare it with \`var ${name} = "..."\` or \`slot ${name} { ... }\``));
      }
    }
  }
}
