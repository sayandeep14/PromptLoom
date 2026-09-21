import {
  CompletionItem,
  CompletionItemKind,
  CompletionParams,
  InsertTextFormat,
  MarkupKind,
} from 'vscode-languageserver/node';
import { TextDocuments } from 'vscode-languageserver/node';
import { TextDocument } from 'vscode-languageserver-textdocument';
import * as path from 'path';
import { fileURLToPath } from 'url';

import { parseLoomDocument, LoomNode } from '../server/parser';
import { LoomRegistry } from '../server/registry';

// ─── Field metadata ───────────────────────────────────────────────────────────

const SCALAR_FIELDS = [
  { name: 'summary',   doc: 'Brief description of the prompt.' },
  { name: 'persona',   doc: 'Role/identity statement for the LLM.' },
  { name: 'context',   doc: 'Background information provided to the LLM.' },
  { name: 'objective', doc: 'The primary goal or task.' },
  { name: 'notes',     doc: 'Miscellaneous annotations.' },
  { name: 'kind',      doc: 'Category or type tag for this prompt (v2).' },
] as const;

const LIST_FIELDS = [
  { name: 'instructions',    doc: 'Ordered steps or directives.' },
  { name: 'constraints',     doc: 'Restrictions and guardrails applied to the response.' },
  { name: 'examples',        doc: 'Concrete few-shot examples.' },
  { name: 'format',          doc: 'Output structure section headers.' },
  { name: 'todo',            doc: 'Work items or pending tasks for this prompt (v2).' },
  { name: 'compatible_with', doc: 'List of compatible prompt names or pack references (v2).' },
] as const;

const CONTRACT_FIELDS = [
  { name: 'required_sections',  doc: 'Section names that must appear in the output.' },
  { name: 'forbidden_sections', doc: 'Section names that must not appear.' },
  { name: 'must_include',       doc: 'Exact strings that must be present in the output.' },
  { name: 'must_not_include',   doc: 'Exact strings that must not appear in the output.' },
] as const;

const CAPABILITIES_FIELDS = [
  { name: 'allowed',   doc: 'Permitted LLM capabilities.' },
  { name: 'forbidden', doc: 'Prohibited LLM capabilities.' },
] as const;

// v2 has exactly one field operator. contract / capabilities keys are the exception:
// they are written `key:` followed by a list.
const OP_DETAIL: Record<string, string> = {
  ':=': 'set the field (replaces any inherited value; use from(parent[..]) to extend)',
  ':':  'key — items follow on the next lines',
};

// ─── Context detection ────────────────────────────────────────────────────────

type BodyCtx = 'top-level' | 'node-body' | 'variant-body' | 'contract-body' | 'capabilities-body';

function getEnclosingContext(lines: string[], lineIdx: number, charIdx: number): BodyCtx {
  let depth = 0;

  for (let i = lineIdx; i >= 0; i--) {
    const raw = lines[i] ?? '';
    const text = i === lineIdx ? raw.slice(0, charIdx) : raw;

    for (let j = text.length - 1; j >= 0; j--) {
      if (text[j] === '}') {
        depth++;
      } else if (text[j] === '{') {
        if (depth > 0) {
          depth--;
        } else {
          // Found the enclosing opening brace
          const before = text.slice(0, j).trimEnd();
          if (/\bcontract\s*$/.test(before))                      return 'contract-body';
          if (/\bcapabilities\s*$/.test(before))                  return 'capabilities-body';
          if (/\b(variant|env)\s+[a-zA-Z0-9_-]+\s*$/.test(before)) return 'variant-body';
          if (/^(prompt|block|overlay)\s+/.test(before.trimStart())) return 'node-body';
          return 'node-body';
        }
      }
    }
  }

  return 'top-level';
}

// ─── Main entry point ─────────────────────────────────────────────────────────

export function getCompletions(
  params: CompletionParams,
  documents: TextDocuments<TextDocument>,
  registry: LoomRegistry,
): CompletionItem[] {
  const doc = documents.get(params.textDocument.uri);
  if (!doc) return [];

  const text  = doc.getText();
  const lines = text.split('\n');
  const { line: li, character: ci } = params.position;
  const lineUpTo = (lines[li] ?? '').slice(0, ci);

  // ── 1. Inside {{ }} variable token ─────────────────────────────────────────
  {
    const lastOpen  = lineUpTo.lastIndexOf('{{');
    const lastClose = lineUpTo.lastIndexOf('}}');
    if (lastOpen !== -1 && lastOpen > lastClose) {
      const { nodes } = parseLoomDocument(text, params.textDocument.uri);
      const node = nodes.find(n => n.range.start.line <= li && li <= n.range.end.line);
      return varTokenCompletions(node, registry);
    }
  }

  // ── 2. After `inherits ` or after a comma in multi-parent list ─────────────
  {
    const m = lineUpTo.match(/\binherits\s+((?:[a-zA-Z0-9_./-]+\s*,\s*)*)([a-zA-Z0-9_./-]*)$/);
    if (m) {
      const partial = m[2]; // token currently being typed
      const lastDot = partial.lastIndexOf('.');
      if (lastDot >= 0) {
        // 'pack-slug.' already typed — list prompts from that pack
        return packPromptCompletions(partial.slice(0, lastDot), registry);
      }
      // Not past a dot yet — show all prompts AND pack slugs
      return [...promptNameCompletions(registry), ...packSlugCompletions(registry, 'prompt')];
    }
  }

  // ── 3. After `use ` ─────────────────────────────────────────────────────────
  {
    const m = lineUpTo.match(/^\s+use\s+([a-zA-Z0-9_./-]*)$/);
    if (m) {
      const partial = m[1];
      const lastDot = partial.lastIndexOf('.');
      if (lastDot >= 0) {
        return packBlockCompletions(partial.slice(0, lastDot), registry);
      }
      return [...blockNameCompletions(registry), ...packSlugCompletions(registry, 'block')];
    }
  }

  // ── 4. from() expression completions (after := on a field line) ────────────
  if (/^\s+[a-zA-Z_][a-zA-Z0-9_-]*\s*:=\s*$/.test(lineUpTo)) {
    return fromExprCompletions();
  }

  // ── 5. Enclosing block context ──────────────────────────────────────────────
  const ctx = getEnclosingContext(lines, li, ci);

  switch (ctx) {
    case 'top-level':        return topLevelCompletions();
    case 'contract-body':    return contractFieldCompletions();
    case 'capabilities-body': return capabilitiesFieldCompletions();
    case 'variant-body':     return variantBodyCompletions();
    case 'node-body':        return nodeBodyCompletions();
  }
}

// ─── Completion builders ──────────────────────────────────────────────────────

function topLevelCompletions(): CompletionItem[] {
  return [
    kw('prompt',  'Declare a prompt node',        'prompt ${1:Name} {\n  $0\n}'),
    kw('block',   'Declare a reusable block',      'block ${1:Name} {\n  $0\n}'),
    kw('overlay', 'Declare a field-overlay node',  'overlay ${1:Name} {\n  $0\n}'),
  ];
}

function nodeBodyCompletions(): CompletionItem[] {
  return [
    // Body keywords
    kw('use',          'Include a reusable block',                                     'use ${1:BlockName}'),
    kw('var',          'Declare a variable with a default value',                      'var ${1:name} = "${2:value}"'),
    kw('slot',         'Declare a required runtime input',                             'slot ${1:name} { required: ${2:true} }'),
    kw('variant',      'Named sub-block with field overrides',                         'variant ${1:name} {\n  $0\n}'),
    kw('env',          'Environment-specific field overrides (e.g. env production {})', 'env ${1:name} {\n  $0\n}'),
    kw('contract',     'Output contract block',                                        'contract {\n  $0\n}'),
    kw('capabilities', 'LLM capabilities block',                                       'capabilities {\n  $0\n}'),
    kw('tags',         'Metadata labels for indexing — never inherited, never rendered', 'tags: ${1:tag1, tag2}'),
    // All body-level field ops
    ...scalarFieldItems(),
    ...listFieldItems(),
  ];
}

function variantBodyCompletions(): CompletionItem[] {
  // Variants only contain field operations (no keywords)
  return [...scalarFieldItems(), ...listFieldItems()];
}

function contractFieldCompletions(): CompletionItem[] {
  return fieldItems(CONTRACT_FIELDS, [':'], '- $0');
}

function capabilitiesFieldCompletions(): CompletionItem[] {
  return fieldItems(CAPABILITIES_FIELDS, [':'], '- $0');
}

function promptNameCompletions(registry: LoomRegistry): CompletionItem[] {
  return registry.allPromptNames().map(name => {
    const uri = registry.lookupPrompt(name)?.uri;
    return {
      label: name,
      kind: CompletionItemKind.Class,
      detail: 'prompt',
      documentation: uri ? md(`Defined in \`${bn(uri)}\``) : undefined,
    };
  });
}

function blockNameCompletions(registry: LoomRegistry): CompletionItem[] {
  return registry.allBlockNames().map(name => {
    const uri = registry.lookupBlock(name)?.uri;
    return {
      label: name,
      kind: CompletionItemKind.Module,
      detail: 'block',
      documentation: uri ? md(`Defined in \`${bn(uri)}\``) : undefined,
    };
  });
}

function varTokenCompletions(node: LoomNode | undefined, registry: LoomRegistry): CompletionItem[] {
  const items: CompletionItem[] = [];

  if (node) {
    for (const v of node.vars) {
      const detail = v.isSlot
        ? `slot${v.required ? ' (required)' : ''}`
        : `var = "${v.default}"`;
      const docText = v.isSlot
        ? `\`slot ${v.name} { required: ${v.required} }\``
        : `\`var ${v.name} = "${v.default}"\``;
      items.push({
        label: v.name,
        kind: CompletionItemKind.Variable,
        detail,
        documentation: md(docText),
      });
    }
  }

  for (const gv of registry.allGlobalVars()) {
    items.push({
      label: gv.name,
      kind: CompletionItemKind.Variable,
      detail: gv.isSlot ? 'global slot' : `global var = "${gv.default}"`,
    });
  }

  return items;
}

// ─── from() expression completions ───────────────────────────────────────────

function fromExprCompletions(): CompletionItem[] {
  return [
    {
      label: 'from(parent[*])',
      kind: CompletionItemKind.Function,
      detail: 'All items from all parents (list fields only)',
      documentation: md('Merges all items from every parent. Only valid on list fields.'),
      insertText: 'from(parent[*])',
      insertTextFormat: InsertTextFormat.Snippet,
      sortText: 'a_from_all',
    },
    {
      label: 'from(parent[0])',
      kind: CompletionItemKind.Function,
      detail: 'Items from first parent (scalar or list)',
      documentation: md('Selects items from the first parent in the inherits list.'),
      insertText: 'from(parent[${1:0}])',
      insertTextFormat: InsertTextFormat.Snippet,
      sortText: 'a_from_one',
    },
    {
      label: 'from(parent[*]) and { ... }',
      kind: CompletionItemKind.Function,
      detail: 'Merge all parents then add new items',
      documentation: md('Merges all parent items then appends new bullet items.'),
      insertText: 'from(parent[*]) and {\n    - ${1:new item}\n  }',
      insertTextFormat: InsertTextFormat.Snippet,
      sortText: 'a_from_add',
    },
    {
      label: 'from(parent[0..N])',
      kind: CompletionItemKind.Function,
      detail: 'Items from a range of parents',
      documentation: md('Selects items from parents at index 0 through N (exclusive).'),
      insertText: 'from(parent[${1:0}...${2:2}])',
      insertTextFormat: InsertTextFormat.Snippet,
      sortText: 'a_from_range',
    },
    {
      label: 'from(ParentName)',
      kind: CompletionItemKind.Function,
      detail: 'Pull from a specific named parent',
      documentation: md('Reference a parent by its exact name. Useful when you have multiple parents and need a specific one regardless of position.'),
      insertText: 'from(${1:ParentName})',
      insertTextFormat: InsertTextFormat.Snippet,
      sortText: 'a_from_named',
    },
  ];
}

// ─── Field item helpers ───────────────────────────────────────────────────────

function scalarFieldItems(): CompletionItem[] {
  return fieldItems(SCALAR_FIELDS, [':='], '$0');
}

function listFieldItems(): CompletionItem[] {
  return fieldItems(LIST_FIELDS, [':='], '- $0');
}

function fieldItems(
  fields: ReadonlyArray<{ name: string; doc: string }>,
  ops: string[],
  contentSuffix: string,
): CompletionItem[] {
  const out: CompletionItem[] = [];
  for (const f of fields) {
    for (const op of ops) {
      // Canonical format: bare `:` has no leading space; all other operators do (e.g. `field :=`)
      const sep = op === ':' ? '' : ' ';
      out.push({
        label: `${f.name} ${op}`,
        kind: CompletionItemKind.Field,
        detail: OP_DETAIL[op],
        documentation: md(f.doc),
        insertText: `${f.name}${sep}${op}\n    ${contentSuffix}`,
        insertTextFormat: InsertTextFormat.Snippet,
        sortText: `z_${f.name}${op}`,
      });
    }
  }
  return out;
}

// ─── Pack-aware completions ───────────────────────────────────────────────────

function packSlugCompletions(registry: LoomRegistry, forKind: 'prompt' | 'block'): CompletionItem[] {
  return registry.allPackSlugs().map(slug => {
    const packName = registry.packName(slug);
    return {
      label: slug,
      kind: CompletionItemKind.Module,
      detail: `pack — type . to see ${forKind}s`,
      documentation: md(`**${packName ?? slug}**\nType \`${slug}.\` to list ${forKind}s from this pack.`),
      // Insert slug + dot, then re-trigger suggestions so prompts appear immediately
      insertText: slug + '.',
      insertTextFormat: InsertTextFormat.PlainText,
      command: { title: 'Trigger Suggest', command: 'editor.action.triggerSuggest' },
      sortText: `b_${slug}`,
    };
  });
}

function packPromptCompletions(packSlug: string, registry: LoomRegistry): CompletionItem[] {
  return registry.promptsInPack(packSlug).map(name => ({
    label: name,
    kind: CompletionItemKind.Class,
    detail: `prompt · ${packSlug}`,
    documentation: md(`\`${packSlug}.${name}\``),
    sortText: `a_${name}`,
  }));
}

function packBlockCompletions(packSlug: string, registry: LoomRegistry): CompletionItem[] {
  return registry.blocksInPack(packSlug).map(name => ({
    label: name,
    kind: CompletionItemKind.Module,
    detail: `block · ${packSlug}`,
    documentation: md(`\`${packSlug}.${name}\``),
    sortText: `a_${name}`,
  }));
}

// ─── Small utilities ──────────────────────────────────────────────────────────

function kw(label: string, doc: string, insertText: string): CompletionItem {
  return {
    label,
    kind: CompletionItemKind.Keyword,
    documentation: md(doc),
    insertText,
    insertTextFormat: InsertTextFormat.Snippet,
    sortText: `a_${label}`,   // keywords sort before fields
  };
}

function md(value: string) {
  return { kind: MarkupKind.Markdown, value };
}

function bn(uri: string): string {
  try { return path.basename(fileURLToPath(uri)); } catch { return uri.split('/').pop() ?? uri; }
}
