import {
  Definition,
  DefinitionParams,
  Location,
} from 'vscode-languageserver/node';
import { TextDocuments } from 'vscode-languageserver/node';
import { TextDocument } from 'vscode-languageserver-textdocument';

import { parseLoomDocument, parseVarsFile } from '../server/parser';
import { LoomRegistry } from '../server/registry';

// ─── Regexes ──────────────────────────────────────────────────────────────────

const VAR_TOKEN_RE  = /\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g;
const INHERITS_RE   = /\binherits\s+[a-zA-Z0-9_./-]+(?:\s*,\s*[a-zA-Z0-9_./-]+)*$/;
const USE_RE        = /^\s+use\s+([a-zA-Z0-9_-]+)\s*$/;
const VAR_DECL_RE   = /^(\s+)(var|slot)\s+([a-zA-Z0-9_-]+)/;
const NODE_DECL_RE  = /^(prompt|block|overlay)\s+([a-zA-Z0-9_-]+)/;

// ─── Helpers ──────────────────────────────────────────────────────────────────

function getWordAt(line: string, ci: number): { word: string; start: number } | null {
  const isWordChar = (c: string) => /[a-zA-Z0-9_\-/]/.test(c);
  let start = ci;
  while (start > 0 && isWordChar(line[start - 1])) start--;
  let end = ci;
  while (end < line.length && isWordChar(line[end])) end++;
  if (start === end) return null;
  return { word: line.slice(start, end), start };
}

/**
 * The parent a `from(Name)` or `parent[N]` under the cursor refers to (its name), or undefined
 * when the cursor is not on one. `parent[0..2]` resolves to its first parent.
 */
export function fromTargetAt(line: string, ci: number, parents: string[]): string | undefined {
  const named = /\bfrom\(\s*(?!parent\b)([A-Za-z_][A-Za-z0-9_.-]*)\s*\)/g;
  let m: RegExpExecArray | null;
  while ((m = named.exec(line)) !== null) {
    const start = m.index + m[0].indexOf(m[1]);
    if (ci >= start && ci <= start + m[1].length) return m[1];
  }
  const indexed = /\bparent\[(\d+)(?:\.\.\d+)?\]/g;
  while ((m = indexed.exec(line)) !== null) {
    if (ci >= m.index && ci <= m.index + m[0].length) return parents[parseInt(m[1], 10)];
  }
  return undefined;
}

// ─── Main export ──────────────────────────────────────────────────────────────

export function getDefinition(
  params: DefinitionParams,
  documents: TextDocuments<TextDocument>,
  registry: LoomRegistry,
): Definition | null {
  const doc = documents.get(params.textDocument.uri);
  if (!doc) return null;

  const text  = doc.getText();
  const lines = text.split('\n');
  const { line: li, character: ci } = params.position;
  const line  = lines[li] ?? '';

  if (line.trimStart().startsWith('//')) return null;

  const isVarsFile = params.textDocument.uri.endsWith('.vars.loom');
  const nodes = isVarsFile ? [] : parseLoomDocument(text, params.textDocument.uri).nodes;

  // ── 1. {{ varName }} token ─────────────────────────────────────────────────
  if (line.includes('{{')) {
    VAR_TOKEN_RE.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = VAR_TOKEN_RE.exec(line)) !== null) {
      if (ci >= m.index && ci <= m.index + m[0].length) {
        const varName = m[1].trim();
        const node = nodes.find(n => n.range.start.line <= li && li <= n.range.end.line);

        // Check local vars first
        if (node) {
          const v = node.vars.find(v => v.name === varName);
          if (v) {
            return Location.create(params.textDocument.uri, v.nameRange);
          }
        }

        // Check global vars
        for (const { entry, uri } of registry.allGlobalVarsWithUri()) {
          if (entry.name === varName) {
            return Location.create(uri, entry.nameRange);
          }
        }

        return null;
      }
    }
  }

  const wi = getWordAt(line, ci);
  if (!wi) return null;
  const { word, start: ws } = wi;

  // ── 2. `inherits ParentName` (including multi-parent lists) ─────────────────
  {
    // Works for any parent in a comma-separated list: check that `inherits`
    // keyword appears somewhere before the word start in the same line.
    const inheritsIdx = line.indexOf('inherits');
    if (inheritsIdx >= 0 && ws > inheritsIdx) {
      const afterInherits = line.slice(inheritsIdx + 'inherits'.length).trimStart();
      // Verify word is within the inherits clause (before any `{`)
      const braceIdx = line.indexOf('{');
      if (braceIdx < 0 || ws < braceIdx) {
        const entry = registry.lookupPrompt(word);
        if (entry) return Location.create(entry.uri, entry.node.nameRange);
        return null;
      }
    }
  }

  // ── 3. `use BlockName` ────────────────────────────────────────────────────
  {
    const um = line.match(USE_RE);
    if (um && um[1] === word) {
      const entry = registry.lookupBlock(word);
      if (entry) return Location.create(entry.uri, entry.node.nameRange);
      return null;
    }
  }

  // ── 3b. from(ParentName) and parent[N] inside a from() expression ─────────────
  {
    const node = nodes.find(n => n.kind === 'prompt' && n.range.start.line <= li && li <= n.range.end.line);
    if (node) {
      const target = fromTargetAt(line, ci, node.parents);
      if (target !== undefined) {
        const entry = registry.lookupPrompt(target) ?? registry.lookupPrompt(target.slice(target.lastIndexOf('.') + 1));
        return entry ? Location.create(entry.uri, entry.node.nameRange) : null;
      }
    }
  }

  // ── 4. Node declaration name — go to self (no-op but valid) ───────────────
  {
    const dm = line.match(NODE_DECL_RE);
    if (dm && dm[2] === word) {
      const expectedStart = dm[1].length + 1;
      if (ws === expectedStart) {
        const node = nodes.find(n => n.name === word && n.range.start.line === li);
        if (node) return Location.create(params.textDocument.uri, node.nameRange);
      }
    }
  }

  return null;
}
