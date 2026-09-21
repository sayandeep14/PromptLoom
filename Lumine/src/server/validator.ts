import { Diagnostic, DiagnosticSeverity, Range } from 'vscode-languageserver-types';
import { LoomNode, FieldOp } from './parser';
import { LoomRegistry } from './registry';
import { LoomConfig, DEFAULT_CONFIG } from './toml-config';
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

export function validateDocument(
  nodes: LoomNode[],
  text: string,
  uri: string,
  registry: LoomRegistry,
  config: LoomConfig = DEFAULT_CONFIG,
): Diagnostic[] {
  const diags: Diagnostic[] = [];

  // ── 1. Duplicate names within this file ──────────────────────────────────
  const seenInFile = new Map<string, LoomNode>();
  for (const node of nodes) {
    const prev = seenInFile.get(node.name);
    if (prev) {
      diags.push(mkError(
        node.nameRange,
        `Duplicate ${node.kind} name "${node.name}" (first defined at line ${prev.nameRange.start.line + 1})`,
      ));
    } else {
      seenInFile.set(node.name, node);
    }
  }

  // ── 2. Duplicate names across workspace (registry has OTHER files only) ──
  for (const node of nodes) {
    const entry =
      node.kind === 'prompt'  ? registry.lookupPrompt(node.name) :
      node.kind === 'block'   ? registry.lookupBlock(node.name) :
                                registry.lookupOverlay(node.name);
    if (entry) {
      diags.push(mkError(
        node.nameRange,
        `Duplicate ${node.kind} name "${node.name}" (also defined in ${basename(entry.uri)})`,
      ));
    }
  }

  for (const node of nodes) {
    // ── 3. Unknown parent(s) ──────────────────────────────────────────────
    if (node.parents.length > 0) {
      for (let pi = 0; pi < node.parents.length; pi++) {
        const parentName = node.parents[pi];
        const parentRange = node.parentRanges[pi] ?? node.nameRange;
        if (!resolvePrompt(parentName, registry, seenInFile)) {
          const localCandidates = [
            ...registry.allPromptNames(),
            ...[...seenInFile.values()].filter(n => n.kind === 'prompt').map(n => n.name),
          ];
          const suggestion = closestMatch(localNameOf(parentName) ?? parentName, localCandidates);
          diags.push(mkError(
            parentRange,
            suggestion
              ? `Unknown prompt "${parentName}" — did you mean "${suggestion}"?`
              : `Unknown prompt "${parentName}"`,
          ));
        }
      }
    }

    // ── 4. Unknown block references ───────────────────────────────────────
    for (const ref of node.uses) {
      if (!resolveBlock(ref.name, registry, seenInFile)) {
        const localCandidates = [
          ...registry.allBlockNames(),
          ...[...seenInFile.values()].filter(n => n.kind === 'block').map(n => n.name),
        ];
        const suggestion = closestMatch(localNameOf(ref.name) ?? ref.name, localCandidates);
        diags.push(mkError(
          ref.range,
          suggestion
            ? `Unknown block "${ref.name}" — did you mean "${suggestion}"?`
            : `Unknown block "${ref.name}"`,
        ));
      }
    }

    // ── 5. Inheritance cycle ──────────────────────────────────────────────
    if (node.kind === 'prompt' && node.parents.length > 0 && registry.hasCycle(node.name)) {
      diags.push(mkError(
        node.nameRange,
        `Inheritance cycle detected involving "${node.name}"`,
      ));
    }

    // ── 6. -=  on scalar field ────────────────────────────────────────────
    checkScalarRemove(node.kind, node.name, node.fields, diags);
    for (const v of node.variants) checkScalarRemove('variant', v.name, v.fields, diags);
    for (const e of node.envBlocks) checkScalarRemove('env', e.name, e.fields, diags);

    // ── 7. Duplicate var/slot names within a node ─────────────────────────
    const seenVars = new Map<string, number>();
    for (const v of node.vars) {
      const prev = seenVars.get(v.name);
      if (prev !== undefined) {
        diags.push(mkError(
          v.nameRange,
          `Duplicate variable "${v.name}" (first declared at line ${prev + 1})`,
        ));
      } else {
        seenVars.set(v.name, v.nameRange.start.line);
      }
    }
  }

  // ── 8. Undefined {{ variable }} tokens (scans raw text for accuracy) ────
  validateVarTokens(text, nodes, registry, diags);

  // ── 9. Unknown field names (scans raw text for field-like lines) ─────────
  checkUnknownFields(text, nodes, diags);

  // ── 10. v1 syntax: +=  -=  bare ':'  extends ─────────────────────────────
  checkLegacySyntax(nodes, text, registry, diags);

  // ── 11. Warning diagnostics ───────────────────────────────────────────────
  validateWarnings(nodes, registry, config, diags);

  return diags;
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
    if (!line.includes('{{')) continue;

    // Find the node whose body contains this line
    const node = nodes.find(
      n => n.bodyRange.start.line <= lineIdx && lineIdx < n.range.end.line,
    );
    if (!node) continue;

    const localVars = new Set([
      ...node.vars.map(v => v.name),
      ...globalVarNames,
    ]);

    varPattern.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = varPattern.exec(line)) !== null) {
      const varName = m[1].trim();
      if (!localVars.has(varName)) {
        diags.push(mkError(
          rng(lineIdx, m.index, lineIdx, m.index + m[0].length),
          `Undefined variable "{{ ${varName} }}" — declare it with \`var ${varName} = "..."\` or \`slot ${varName} { ... }\``,
        ));
      }
    }
  }
}

function checkUnknownFields(
  text: string,
  nodes: LoomNode[],
  diags: Diagnostic[],
): void {
  // Matches indented lines that look like a field declaration (word followed by operator, nothing else)
  const fieldLike = /^(\s+)([a-zA-Z_][a-zA-Z0-9_-]*)\s*(:=|\+=|-=|:)\s*$/;
  const lines = text.split('\n');

  for (let lineIdx = 0; lineIdx < lines.length; lineIdx++) {
    const line = lines[lineIdx];
    const m = line.match(fieldLike);
    if (!m) continue;

    const fieldName = m[2];
    if (ALL_VALID_FIELDS.has(fieldName) || BODY_KEYWORDS.has(fieldName)) continue;

    // Must be inside a node body
    const inBody = nodes.some(
      n => n.bodyRange.start.line <= lineIdx && lineIdx < n.range.end.line,
    );
    if (!inBody) continue;

    // Ignore lines that are nested variant/contract/capabilities declarations
    const lineIndent = indentOf(line);
    if (lineIndent === 0) continue;

    const nameIdx = line.indexOf(fieldName);
    const suggestion = closestMatch(fieldName, [...ALL_VALID_FIELDS]);
    diags.push(mkError(
      rng(lineIdx, nameIdx, lineIdx, nameIdx + fieldName.length),
      suggestion
        ? `Unknown field "${fieldName}" — did you mean "${suggestion}"?`
        : `Unknown field "${fieldName}"`,
    ));
  }
}

// ─── Warning diagnostics ──────────────────────────────────────────────────────

const VAR_TOKEN_RE = /\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g;

function hasField(node: LoomNode, fieldName: string): boolean {
  return node.fields.some(f => f.fieldName === fieldName);
}

function getInheritanceDepth(name: string, registry: LoomRegistry): number {
  return registry.inheritanceChain(name).length - 1;
}

function blockVarTokens(fields: FieldOp[]): Set<string> {
  const out = new Set<string>();
  for (const f of fields) {
    for (const line of f.value) {
      VAR_TOKEN_RE.lastIndex = 0;
      let m: RegExpExecArray | null;
      while ((m = VAR_TOKEN_RE.exec(line)) !== null) {
        out.add(m[1].trim());
      }
    }
  }
  return out;
}

function validateWarnings(
  nodes: LoomNode[],
  registry: LoomRegistry,
  config: LoomConfig,
  diags: Diagnostic[],
): void {
  const vc = config.validation;
  const globalVarNames = new Set(registry.allGlobalVars().map(v => v.name));

  for (const node of nodes) {
    if (node.kind !== 'prompt') continue;

    // ── Missing objective ─────────────────────────────────────────────────
    if (vc.require_objective && !hasField(node, 'objective')) {
      diags.push(mkWarning(
        node.nameRange,
        `Prompt "${node.name}" is missing an "objective" field`,
      ));
    }

    // ── Missing format ────────────────────────────────────────────────────
    if (vc.require_format && !hasField(node, 'format')) {
      diags.push(mkWarning(
        node.nameRange,
        `Prompt "${node.name}" is missing a "format" field`,
      ));
    }

    // ── Missing contract ──────────────────────────────────────────────────
    if (vc.require_contract && !node.contract) {
      diags.push(mkWarning(
        node.nameRange,
        `Prompt "${node.name}" is missing a "contract" block`,
      ));
    }

    // ── Empty context field ───────────────────────────────────────────────
    if (vc.warn_on_empty_context) {
      const ctxField = node.fields.find(f => f.fieldName === 'context');
      if (ctxField && ctxField.value.length === 0) {
        diags.push(mkWarning(
          ctxField.nameRange,
          `Field "context" is empty — add content or remove the declaration`,
        ));
      }
    }

    // ── Deep inheritance chain ────────────────────────────────────────────
    if (vc.warn_on_deep_inheritance && node.parents.length > 0) {
      const depth = getInheritanceDepth(node.name, registry);
      if (depth > vc.max_inheritance_depth) {
        diags.push(mkWarning(
          node.nameRange,
          `Inheritance depth ${depth} exceeds max (${vc.max_inheritance_depth})`,
        ));
      }
    }

    // ── Check #12: from(parent[*]) on scalar field ────────────────────────
    for (const field of node.fields) {
      if (field.fromExprRaw && field.fromExprRaw.includes('parent[*]') && SCALAR_FIELDS.has(field.fieldName)) {
        diags.push(mkError(
          field.nameRange,
          `from(parent[*]) cannot be used on scalar field '${field.fieldName}' — use from(parent[N]) to select one parent`,
        ));
      }
    }

    // ── Block {{ tokens }} not declared in consuming prompt ───────────────
    const localVarNames = new Set([
      ...node.vars.map(v => v.name),
      ...globalVarNames,
    ]);
    for (const ref of node.uses) {
      const blockEntry = registry.lookupBlock(ref.name);
      if (!blockEntry) continue;
      const blockTokens = blockVarTokens(blockEntry.node.fields);
      for (const token of blockTokens) {
        if (!localVarNames.has(token)) {
          diags.push(mkWarning(
            ref.range,
            `Block "${ref.name}" uses {{ ${token} }} which is not declared in "${node.name}"`,
          ));
        }
      }
    }

    // ── Required slot with no default ─────────────────────────────────────
    for (const v of node.vars) {
      if (!v.isSlot) continue;
      if (!v.required && !v.default) {
        diags.push(mkWarning(
          v.nameRange,
          `Slot "${v.name}" has no default and is not required — it will render as empty if not provided`,
        ));
      }
    }
  }
}
