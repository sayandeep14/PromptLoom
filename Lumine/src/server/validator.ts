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
    checkScalarRemove(node.fields, diags);
    for (const v of node.variants) checkScalarRemove(v.fields, diags);

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

  // ── 10. Warning diagnostics ───────────────────────────────────────────────
  validateWarnings(nodes, registry, config, diags);

  return diags;
}

// ─── Sub-checks ───────────────────────────────────────────────────────────────

function checkScalarRemove(fields: FieldOp[], diags: Diagnostic[]): void {
  for (const f of fields) {
    if (f.op === '-=' && SCALAR_FIELDS.has(f.fieldName)) {
      diags.push(mkError(
        f.nameRange,
        `"-=" operator is not allowed on scalar field "${f.fieldName}" — only list fields support remove`,
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

    // ── Ambiguous `:` on inherited field ──────────────────────────────────
    if (node.parents.length > 0) {
      // Collect union of field names from all parents
      const parentFields = new Set<string>();
      for (const parentName of node.parents) {
        const parentEntry = registry.lookupPrompt(parentName);
        if (parentEntry) {
          for (const f of parentEntry.node.fields) parentFields.add(f.fieldName);
        }
      }
      for (const field of node.fields) {
        if (field.op === ':' && parentFields.has(field.fieldName)) {
          diags.push(mkWarning(
            field.nameRange,
            `Using ":" on inherited field "${field.fieldName}" — use ":=" to override`,
          ));
        }
      }
    }

    // ── Check #11: deprecated += / -= operators ───────────────────────────
    for (const field of node.fields) {
      if (field.op === '+=') {
        diags.push(mkWarning(
          field.nameRange,
          `'+=' is deprecated in v2 — use ':= from(parent[*]) and { ... }' instead`,
        ));
      } else if (field.op === '-=') {
        diags.push(mkWarning(
          field.nameRange,
          `'-=' is deprecated in v2 and has no direct replacement`,
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
