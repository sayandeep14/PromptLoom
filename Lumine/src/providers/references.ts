import {
  Location,
  ReferenceParams,
} from 'vscode-languageserver/node';
import { TextDocuments } from 'vscode-languageserver/node';
import { TextDocument } from 'vscode-languageserver-textdocument';
import * as fs from 'fs';
import { fileURLToPath } from 'url';

import { parseLoomDocument, parseVarsFile, LoomNode } from '../server/parser';
import { LoomRegistry } from '../server/registry';

// ─── Regexes ──────────────────────────────────────────────────────────────────

const VAR_TOKEN_RE = /\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g;
const NODE_DECL_RE = /^(prompt|block|overlay)\s+([a-zA-Z0-9_-]+)/;
const VAR_DECL_RE  = /^(\s+)(var|slot)\s+([a-zA-Z0-9_-]+)/;
const USE_RE       = /^\s+use\s+([a-zA-Z0-9_-]+)\s*$/;

// ─── Helpers ──────────────────────────────────────────────────────────────────

function getWordAt(line: string, ci: number): { word: string; start: number } | null {
  const isWordChar = (c: string) => /[a-zA-Z0-9_-]/.test(c);
  let start = ci;
  while (start > 0 && isWordChar(line[start - 1])) start--;
  let end = ci;
  while (end < line.length && isWordChar(line[end])) end++;
  if (start === end) return null;
  return { word: line.slice(start, end), start };
}

function readUri(uri: string, documents: TextDocuments<TextDocument>): string | null {
  const open = documents.get(uri);
  if (open) return open.getText();
  try { return fs.readFileSync(fileURLToPath(uri), 'utf8'); } catch { return null; }
}

// ─── Reference finders ────────────────────────────────────────────────────────

function findInheritsRefs(promptName: string, registry: LoomRegistry, documents: TextDocuments<TextDocument>): Location[] {
  const locs: Location[] = [];

  for (const uri of registry.allUris()) {
    const text = readUri(uri, documents);
    if (!text) continue;

    const { nodes } = parseLoomDocument(text, uri);
    for (const node of nodes) {
      if (node.kind === 'prompt' && node.parent === promptName && node.parentRange) {
        locs.push(Location.create(uri, node.parentRange));
      }
    }
  }

  return locs;
}

function findUseRefs(blockName: string, registry: LoomRegistry, documents: TextDocuments<TextDocument>): Location[] {
  const locs: Location[] = [];
  const usePattern = new RegExp(`^\\s+use\\s+(${escapeRegex(blockName)})\\s*$`);

  for (const uri of registry.allUris()) {
    const text = readUri(uri, documents);
    if (!text) continue;

    if (!text.includes(blockName)) continue;

    const lines = text.split('\n');
    for (let li = 0; li < lines.length; li++) {
      const m = lines[li].match(usePattern);
      if (m) {
        const col = lines[li].indexOf(m[1]);
        locs.push(Location.create(uri, {
          start: { line: li, character: col },
          end:   { line: li, character: col + m[1].length },
        }));
      }
    }
  }

  return locs;
}

function findVarTokenRefs(varName: string, text: string, uri: string, nodes: LoomNode[]): Location[] {
  const locs: Location[] = [];
  if (!text.includes('{{')) return locs;

  const lines = text.split('\n');
  const pattern = new RegExp(`\\{\\{\\s*(${escapeRegex(varName)})\\s*\\}\\}`, 'g');

  for (let li = 0; li < lines.length; li++) {
    const line = lines[li];
    if (!line.includes('{{')) continue;

    pattern.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = pattern.exec(line)) !== null) {
      locs.push(Location.create(uri, {
        start: { line: li, character: m.index },
        end:   { line: li, character: m.index + m[0].length },
      }));
    }
  }

  return locs;
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

// ─── Main export ──────────────────────────────────────────────────────────────

export function getReferences(
  params: ReferenceParams,
  documents: TextDocuments<TextDocument>,
  registry: LoomRegistry,
): Location[] {
  const doc = documents.get(params.textDocument.uri);
  if (!doc) return [];

  const text  = doc.getText();
  const lines = text.split('\n');
  const uri   = params.textDocument.uri;
  const { line: li, character: ci } = params.position;
  const line  = lines[li] ?? '';

  if (line.trimStart().startsWith('//')) return [];

  const isVarsFile = uri.endsWith('.vars.loom');
  const nodes = isVarsFile ? [] : parseLoomDocument(text, uri).nodes;

  const wi = getWordAt(line, ci);
  if (!wi) return [];
  const { word, start: ws } = wi;

  // ── 1. Node declaration — find all usages of this prompt/block ────────────
  {
    const dm = line.match(NODE_DECL_RE);
    if (dm && dm[2] === word) {
      const expectedStart = dm[1].length + 1;
      if (ws === expectedStart) {
        if (dm[1] === 'prompt') {
          const locs = findInheritsRefs(word, registry, documents);
          if (params.context.includeDeclaration) {
            const node = nodes.find(n => n.name === word && n.range.start.line === li);
            if (node) locs.unshift(Location.create(uri, node.nameRange));
          }
          return locs;
        }
        if (dm[1] === 'block') {
          const locs = findUseRefs(word, registry, documents);
          if (params.context.includeDeclaration) {
            const node = nodes.find(n => n.name === word && n.range.start.line === li);
            if (node) locs.unshift(Location.create(uri, node.nameRange));
          }
          return locs;
        }
      }
    }
  }

  // ── 2. var/slot declaration — find all {{ name }} in this file ────────────
  {
    const vm = line.match(VAR_DECL_RE);
    if (vm && vm[3] === word) {
      const locs = findVarTokenRefs(word, text, uri, nodes);
      if (params.context.includeDeclaration) {
        const node = nodes.find(n => n.range.start.line <= li && li <= n.range.end.line);
        const v = node?.vars.find(v => v.name === word) ??
          (isVarsFile ? parseVarsFile(text).find(v => v.name === word) : undefined);
        if (v) locs.unshift(Location.create(uri, v.nameRange));
      }
      return locs;
    }
  }

  // ── 3. `inherits ParentName` — go find references to that prompt ──────────
  {
    const before = line.slice(0, ws);
    if (/\binherits\s+$/.test(before)) {
      return findInheritsRefs(word, registry, documents);
    }
  }

  // ── 4. `use BlockName` — go find references to that block ────────────────
  {
    const um = line.match(USE_RE);
    if (um && um[1] === word) {
      return findUseRefs(word, registry, documents);
    }
  }

  // ── 5. {{ varName }} token — find other usages in file ───────────────────
  if (line.includes('{{')) {
    VAR_TOKEN_RE.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = VAR_TOKEN_RE.exec(line)) !== null) {
      if (ci >= m.index && ci <= m.index + m[0].length) {
        const varName = m[1].trim();
        return findVarTokenRefs(varName, text, uri, nodes);
      }
    }
  }

  return [];
}
