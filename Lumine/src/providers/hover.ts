import {
  Hover,
  HoverParams,
  MarkupContent,
  MarkupKind,
} from 'vscode-languageserver/node';
import { TextDocuments } from 'vscode-languageserver/node';
import { TextDocument } from 'vscode-languageserver-textdocument';
import * as path from 'path';
import { fileURLToPath } from 'url';

import { parseLoomDocument, parseVarsFile, LoomNode, VarEntry } from '../server/parser';
import { LoomRegistry } from '../server/registry';

// ─── Field metadata ───────────────────────────────────────────────────────────

const FIELD_DOCS: Record<string, { type: 'scalar' | 'list'; desc: string }> = {
  summary:           { type: 'scalar', desc: 'Brief description of the prompt.' },
  persona:           { type: 'scalar', desc: 'Role/identity statement for the LLM.' },
  context:           { type: 'scalar', desc: 'Background information provided to the LLM.' },
  objective:         { type: 'scalar', desc: 'The primary goal or task.' },
  notes:             { type: 'scalar', desc: 'Miscellaneous annotations.' },
  kind:              { type: 'scalar', desc: 'Category or type tag for this prompt (v2).' },
  instructions:      { type: 'list',   desc: 'Ordered steps or directives for the LLM.' },
  constraints:       { type: 'list',   desc: 'Restrictions and guardrails applied to the response.' },
  examples:          { type: 'list',   desc: 'Concrete few-shot examples.' },
  format:            { type: 'list',   desc: 'Output structure section headers.' },
  todo:              { type: 'list',   desc: 'Work items or pending tasks for this prompt (v2).' },
  compatible_with:   { type: 'list',   desc: 'Compatible prompt names or pack references (v2).' },
  required_sections: { type: 'list',   desc: '*(contract)* Section names that must appear in the output.' },
  forbidden_sections:{ type: 'list',   desc: '*(contract)* Section names that must not appear in the output.' },
  must_include:      { type: 'list',   desc: '*(contract)* Exact strings that must be present in the output.' },
  must_not_include:  { type: 'list',   desc: '*(contract)* Exact strings that must not appear in the output.' },
  allowed:           { type: 'list',   desc: '*(capabilities)* Permitted LLM capabilities.' },
  forbidden:         { type: 'list',   desc: '*(capabilities)* Prohibited LLM capabilities.' },
};

const OP_DOCS: Record<string, { title: string; body: string }> = {
  ':': {
    title: '`:` — define',
    body:  'Sets the field value. When a parent also defines this field, this is an advisory set — prefer `:=` to make an explicit override.',
  },
  ':=': {
    title: '`:=` — override',
    body:  'Unconditionally replaces any inherited value. Use this when a hard override is intended.',
  },
  '+=': {
    title: '`+=` — append',
    body:  'For **list fields**: extends the inherited list with new items.\nFor **scalar fields**: appends text to the inherited value separated by a blank line.',
  },
  '-=': {
    title: '`-=` — remove',
    body:  'For **list fields only**: removes items matching the provided strings from the inherited list.\n\n> ⚠️ Not valid on scalar fields (`summary`, `persona`, `context`, `objective`, `notes`).',
  },
};

// ─── Regexes ──────────────────────────────────────────────────────────────────

const FIELD_OP_RE  = /^(\s+)(summary|persona|context|objective|notes|kind|instructions|constraints|examples|format|todo|compatible_with|required_sections|forbidden_sections|must_include|must_not_include|allowed|forbidden)\s*(:=|\+=|-=|:)/;
const VAR_TOKEN_RE = /\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g;
const NODE_DECL_RE = /^(prompt|block|overlay)\s+([a-zA-Z0-9_-]+)/;
const VAR_DECL_RE  = /^(\s+)(var|slot)\s+([a-zA-Z0-9_-]+)/;
const INHERITS_RE  = /\binherits\s+[a-zA-Z0-9_./-]+(?:\s*,\s*[a-zA-Z0-9_./-]+)*$/;
const USE_RE       = /^\s+use\s+([a-zA-Z0-9_./-]+)\s*$/;

// ─── Helpers ──────────────────────────────────────────────────────────────────

interface WordInfo { word: string; start: number; end: number; }

function getWordAt(line: string, ci: number): WordInfo | null {
  // Does NOT span dots — callers that need qualified-name logic handle the dot themselves.
  const isWordChar = (c: string) => /[a-zA-Z0-9_-]/.test(c);
  let start = ci;
  while (start > 0 && isWordChar(line[start - 1])) start--;
  let end = ci;
  while (end < line.length && isWordChar(line[end])) end++;
  if (start === end) return null;
  return { word: line.slice(start, end), start, end };
}

function md(value: string): MarkupContent {
  return { kind: MarkupKind.Markdown, value };
}

function hover(value: string): Hover {
  return { contents: md(value) };
}

function bn(uri: string): string {
  try { return path.basename(fileURLToPath(uri)); } catch { return uri.split('/').pop() ?? uri; }
}

function listOr(arr: string[], none = 'none'): string {
  return arr.length ? arr.join(', ') : none;
}

// ─── Content builders ─────────────────────────────────────────────────────────

function operatorHover(op: string): Hover {
  const d = OP_DOCS[op];
  if (!d) return hover(`\`${op}\``);
  return hover(`**${d.title}**\n\n${d.body}`);
}

function fieldHover(fieldName: string, op: string): Hover {
  const info = FIELD_DOCS[fieldName];
  if (!info) return hover(`**${fieldName}**`);
  const opDoc = OP_DOCS[op];
  const lines = [
    `**\`${fieldName}\`** *(${info.type} field)*`,
    `Operator: \`${op}\` — ${opDoc?.title.split('—')[1].trim() ?? op}`,
    '',
    info.desc,
  ];
  if (info.type === 'list') {
    lines.push('', 'Items are prefixed with `- ` on their own line.');
  }
  return hover(lines.join('\n'));
}

function promptNameHover(qualifiedName: string, registry: LoomRegistry): Hover {
  const localPart = qualifiedName.includes('.') ? qualifiedName.split('.').pop()! : qualifiedName;
  const packPrefix = qualifiedName.includes('.') ? qualifiedName.slice(0, qualifiedName.lastIndexOf('.')) : undefined;
  const entry = registry.lookupPrompt(qualifiedName) ?? registry.lookupPrompt(localPart);
  const name = localPart;
  if (!entry) return hover(`**${qualifiedName}** — unknown prompt${packPrefix ? ` (from pack \`${packPrefix}\`)` : ''}`);

  const node = entry.node;
  const fields = listOr(node.fields.map(f => f.fieldName));

  const inheritedBy = listOr(
    registry.allPromptNames().filter(n => {
      const pNode = registry.lookupPrompt(n)?.node;
      return pNode && (pNode.parents ?? (pNode.parent ? [pNode.parent] : [])).includes(name);
    }),
  );

  const allParents = node.parents ?? (node.parent ? [node.parent] : []);

  return hover([
    `**${name}** *(prompt)*`,
    `Defined in \`${bn(entry.uri)}\``,
    allParents.length > 0 ? `Inherits from: ${allParents.map(p => `**${p}**`).join(', ')}` : '',
    '',
    `Fields defined: ${fields}`,
    `Inherited by: ${inheritedBy}`,
  ].filter(l => l !== '').join('\n'));
}

function blockNameHover(qualifiedName: string, registry: LoomRegistry): Hover {
  const localPart = qualifiedName.includes('.') ? qualifiedName.split('.').pop()! : qualifiedName;
  const packPrefix = qualifiedName.includes('.') ? qualifiedName.slice(0, qualifiedName.lastIndexOf('.')) : undefined;
  const entry = registry.lookupBlock(qualifiedName) ?? registry.lookupBlock(localPart);
  const name = localPart;
  if (!entry) return hover(`**${qualifiedName}** — unknown block${packPrefix ? ` (from pack \`${packPrefix}\`)` : ''}`);

  const node = entry.node;
  const fieldSummary = listOr(node.fields.map(f => {
    const listItems = f.value.filter(v => v.startsWith('-')).length;
    return listItems > 0 ? `${f.fieldName} (${listItems} items)` : f.fieldName;
  }));

  const usedBy = listOr(
    registry.allNodes()
      .filter(e => e.node.uses.some(u => u.name === name))
      .map(e => e.node.name),
  );

  return hover([
    `**${name}** *(block)*`,
    `Defined in \`${bn(entry.uri)}\``,
    '',
    `Fields: ${fieldSummary}`,
    `Used by: ${usedBy}`,
  ].join('\n'));
}

function packHover(slug: string, registry: LoomRegistry): Hover {
  const name = registry.packName(slug);
  const prompts = registry.promptsInPack(slug);
  const blocks  = registry.blocksInPack(slug);
  const lines = [`**${name ?? slug}** *(pack)*`, `Slug: \`${slug}\``];
  if (prompts.length > 0) lines.push('', `Prompts: ${prompts.join(', ')}`);
  if (blocks.length  > 0) lines.push(blocks.length > 0 && prompts.length > 0 ? '' : '', `Blocks: ${blocks.join(', ')}`);
  if (prompts.length === 0 && blocks.length === 0) lines.push('', '*(no entries indexed yet — open a file from this pack)*');
  return hover(lines.join('\n'));
}

function varTokenHover(varName: string, node: LoomNode | undefined, registry: LoomRegistry): Hover {
  const v =
    node?.vars.find(v => v.name === varName) ??
    registry.allGlobalVars().find(v => v.name === varName);

  if (!v) return hover(`**\`{{ ${varName} }}\`** — undeclared variable`);

  return varEntryHover(v, node);
}

function varEntryHover(v: VarEntry, node: LoomNode | undefined): Hover {
  const scope = node ? `this ${node.kind}` : 'a global vars file';
  if (v.isSlot) {
    const status = v.required
      ? 'required — must be provided at render time via `--set ' + v.name + '=...`'
      : v.default
        ? `optional — defaults to \`"${v.default}"\``
        : 'optional — no default';
    return hover([
      `**\`${v.name}\`** *(slot variable)*`,
      `Declared in ${scope}: \`slot ${v.name} { required: ${v.required}${v.default ? ` default: "${v.default}"` : ''} }\``,
      '',
      `Status: ${status}`,
    ].join('\n'));
  } else {
    return hover([
      `**\`${v.name}\`** *(variable)*`,
      `Declared in ${scope}: \`var ${v.name} = "${v.default}"\``,
      '',
      `Default value: \`"${v.default}"\``,
    ].join('\n'));
  }
}

function nodeDeclHover(node: LoomNode, registry: LoomRegistry): Hover {
  const lines: string[] = [
    `**${node.name}** *(${node.kind})*`,
  ];

  if (node.kind === 'prompt') {
    const allParents = node.parents ?? (node.parent ? [node.parent] : []);
    if (allParents.length > 0) {
      lines.push(`Inherits: ${allParents.map(p => `**${p}**`).join(', ')}`);
    }
  }

  lines.push('');

  const fields = node.fields.map(f => `\`${f.fieldName}${f.op}\``).join(', ') || 'none';
  lines.push(`Fields: ${fields}`);

  if (node.vars.length) {
    const vars = node.vars.map(v => `\`${v.isSlot ? 'slot' : 'var'} ${v.name}\``).join(', ');
    lines.push(`Variables: ${vars}`);
  }

  if (node.variants.length) {
    lines.push(`Variants: ${node.variants.map(v => `\`${v.name}\``).join(', ')}`);
  }

  if (node.kind === 'prompt') {
    const inheritedBy = listOr(
      registry.allPromptNames().filter(n => {
        const pNode = registry.lookupPrompt(n)?.node;
        return pNode && (pNode.parents ?? (pNode.parent ? [pNode.parent] : [])).includes(node.name);
      }),
    );
    lines.push('', `Inherited by: ${inheritedBy}`);
  }

  if (node.kind === 'block') {
    const usedBy = listOr(
      registry.allNodes()
        .filter(e => e.node.uses.some(u => u.name === node.name))
        .map(e => e.node.name),
    );
    lines.push('', `Used by: ${usedBy}`);
  }

  return hover(lines.join('\n'));
}

// ─── Main export ──────────────────────────────────────────────────────────────

export function getHover(
  params: HoverParams,
  documents: TextDocuments<TextDocument>,
  registry: LoomRegistry,
): Hover | null {
  const doc = documents.get(params.textDocument.uri);
  if (!doc) return null;

  const text  = doc.getText();
  const lines = text.split('\n');
  const { line: li, character: ci } = params.position;
  const line  = lines[li] ?? '';

  // Skip comment lines
  if (line.trimStart().startsWith('//')) return null;

  // Parse document for node context
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
        return varTokenHover(varName, node, registry);
      }
    }
  }

  // ── 2. Field declaration line ──────────────────────────────────────────────
  const fdInfo = (() => {
    const m = line.match(FIELD_OP_RE);
    if (!m) return null;
    const fieldStart = m[1].length;
    const fieldEnd   = fieldStart + m[2].length;
    const opEnd      = m[0].length;
    const opStart    = opEnd - m[3].length;
    return { fieldName: m[2], fieldStart, fieldEnd, op: m[3], opStart, opEnd };
  })();

  if (fdInfo) {
    if (ci >= fdInfo.fieldStart && ci < fdInfo.fieldEnd) {
      return fieldHover(fdInfo.fieldName, fdInfo.op);
    }
    if (ci >= fdInfo.opStart && ci < fdInfo.opEnd) {
      return operatorHover(fdInfo.op);
    }
  }

  // Get word under cursor for remaining checks
  const wi = getWordAt(line, ci);
  if (!wi) return null;
  const { word, start: ws } = wi;

  // ── 3. `inherits ParentName` or `inherits pack-slug.PromptName` ────────────
  {
    const inheritsIdx = line.indexOf('inherits');
    if (inheritsIdx >= 0 && ws > inheritsIdx) {
      // Determine if word is a pack-slug (followed by dot) or a prompt name (optionally after dot)
      if (line[ws + word.length] === '.') {
        // Cursor is on "pack-slug" in "pack-slug.PromptName"
        return packHover(word, registry);
      }
      // Build qualified name if cursor is on the part after a dot
      let lookupName = word;
      if (ws > 0 && line[ws - 1] === '.') {
        let packEnd   = ws - 1;
        let packStart = packEnd;
        while (packStart > 0 && /[a-zA-Z0-9_-]/.test(line[packStart - 1])) packStart--;
        lookupName = `${line.slice(packStart, packEnd)}.${word}`;
      }
      return promptNameHover(lookupName, registry);
    }
  }

  // ── 3b. `from` keyword hover ──────────────────────────────────────────────
  {
    if (word === 'from' && line.includes('from(')) {
      return hover([
        '**`from()`** — v2 expression language for field inheritance',
        '',
        'Syntax forms:',
        '- `from(parent[*])` — all items from all parents *(list fields only)*',
        '- `from(parent[N])` — items from the Nth parent (0-indexed)',
        '- `from(parent[N...M])` — items from parents N through M',
        '- `from(parent[*]) and { - item }` — merge all parents then add new items',
        '',
        '> `from(parent[*])` is an error on scalar fields — use `from(parent[N])` instead.',
      ].join('\n'));
    }
  }

  // ── 4. `use BlockName` or `use pack-slug.BlockName` ───────────────────────
  {
    const um = line.match(USE_RE);
    if (um) {
      const refFull  = um[1];  // may be "pack.Block" or just "Block"
      const dotIdx   = refFull.indexOf('.');
      const refStart = line.indexOf(refFull);

      if (dotIdx >= 0) {
        const packSlug = refFull.slice(0, dotIdx);
        // Cursor is on the pack-slug part (before the dot)
        if (ci >= refStart && ci < refStart + dotIdx) {
          return packHover(packSlug, registry);
        }
        // Cursor is on the block-name part (after the dot)
        return blockNameHover(refFull, registry);
      }

      // Simple unqualified block name
      if (word === refFull) return blockNameHover(refFull, registry);
    }
  }

  // ── 5. `var/slot Name` declaration ────────────────────────────────────────
  {
    const vm = line.match(VAR_DECL_RE);
    if (vm && vm[3] === word) {
      const node = nodes.find(n => n.range.start.line <= li && li <= n.range.end.line);
      const v =
        node?.vars.find(v => v.name === word) ??
        (isVarsFile ? parseVarsFile(text).find(v => v.name === word) : undefined);
      if (v) return varEntryHover(v, node);
    }
  }

  // ── 6. Node declaration name (`prompt/block/overlay Name`) ────────────────
  {
    const dm = line.match(NODE_DECL_RE);
    if (dm && dm[2] === word) {
      const expectedStart = dm[1].length + 1; // kind + space
      if (ws === expectedStart) {
        const node = nodes.find(n => n.name === word && n.range.start.line === li);
        if (node) return nodeDeclHover(node, registry);
      }
    }
  }

  return null;
}
