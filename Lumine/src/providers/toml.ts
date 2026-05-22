import {
  CompletionItem,
  CompletionItemKind,
  Hover,
  InsertTextFormat,
  MarkupKind,
} from 'vscode-languageserver/node';
import { TextDocuments } from 'vscode-languageserver/node';
import { TextDocument } from 'vscode-languageserver-textdocument';

import { getTomlSectionAt, TomlSection } from '../server/toml-config';

// ─── Section / key metadata ───────────────────────────────────────────────────

interface KeyDoc { type: string; default: string; desc: string; }

const SECTION_DOCS: Record<string, string> = {
  project:    'Project identity metadata (name, version).',
  paths:      'Directory paths for prompt, block, overlay, and output files.',
  render:     'Default rendering options applied when running `loom weave`.',
  validation: 'Validation knobs that control which checks are enabled and their thresholds.',
  profile:    'Named variable overrides applied with `--profile <name>`.',
  targets:    'Individual render targets — each entry maps a prompt to an output file.',
};

const SECTION_KEYS: Record<TomlSection & string, Record<string, KeyDoc>> = {
  project: {
    name:    { type: 'string',  default: '',      desc: 'Human-readable project name.' },
    version: { type: 'string',  default: '"0.1.0"', desc: 'Semantic version of this prompt library.' },
  },
  paths: {
    prompts:  { type: 'string', default: '"prompts"',  desc: 'Directory containing `.prompt.loom` files.' },
    blocks:   { type: 'string', default: '"blocks"',   desc: 'Directory containing `.block.loom` files.' },
    overlays: { type: 'string', default: '"overlays"', desc: 'Directory containing `.overlay.loom` files.' },
    out:      { type: 'string', default: '"dist/prompts"', desc: 'Render output directory.' },
  },
  render: {
    default_format:      { type: '"markdown" | "json" | "tool"', default: '"markdown"', desc: 'Default output format for `loom weave`.' },
    include_metadata:    { type: 'bool', default: 'false', desc: 'Include prompt metadata header in rendered output.' },
    include_sourcemap:   { type: 'bool', default: 'false', desc: 'Embed source map in rendered output.' },
    include_fingerprint: { type: 'bool', default: 'false', desc: 'Include a content fingerprint in rendered output.' },
  },
  validation: {
    require_objective:        { type: 'bool', default: 'true',  desc: 'Error if a prompt is missing an `objective` field.' },
    require_format:           { type: 'bool', default: 'true',  desc: 'Error if a prompt is missing a `format` field.' },
    require_contract:         { type: 'bool', default: 'false', desc: 'Error if a prompt is missing a `contract` block.' },
    warn_on_empty_context:    { type: 'bool', default: 'true',  desc: 'Warn when a declared `context` field has no content.' },
    warn_on_deep_inheritance: { type: 'bool', default: 'true',  desc: 'Warn when the inheritance chain is deeper than `max_inheritance_depth`.' },
    max_inheritance_depth:    { type: 'int',  default: '3',     desc: 'Maximum allowed inheritance depth before a warning is emitted.' },
    smell_constraint_limit:   { type: 'int',  default: '25',    desc: 'Maximum constraints before a code-smell warning.' },
    token_limit_warn:         { type: 'int',  default: '0',     desc: 'Estimated token count above which a warning is emitted. `0` disables the check.' },
  },
  targets: {
    prompt: { type: 'string', default: '',            desc: 'Name of the prompt to render.' },
    format: { type: 'string', default: '"markdown"',  desc: 'Output format for this target.' },
    dest:   { type: 'string', default: '',            desc: 'Destination file path for rendered output.' },
  },
  profile: {},
};

const ALL_SECTIONS = ['[project]', '[paths]', '[render]', '[validation]', '[[targets]]'];

// ─── Helpers ──────────────────────────────────────────────────────────────────

function md(value: string) {
  return { kind: MarkupKind.Markdown, value };
}

function hover(value: string): Hover {
  return { contents: md(value) };
}

function isAtLineStart(lineUpTo: string): boolean {
  return lineUpTo.trim() === '' || /^\s*\[/.test(lineUpTo);
}

// ─── Completions ──────────────────────────────────────────────────────────────

export function getTomlCompletions(
  params: { position: { line: number; character: number }; textDocument: { uri: string } },
  documents: TextDocuments<TextDocument>,
): CompletionItem[] {
  const doc = documents.get(params.textDocument.uri);
  if (!doc) return [];

  const text = doc.getText();
  const lines = text.split('\n');
  const { line: li, character: ci } = params.position;
  const lineUpTo = (lines[li] ?? '').slice(0, ci);

  // Section header completions — cursor is on a line starting with [
  if (/^\s*\[/.test(lineUpTo)) {
    return ALL_SECTIONS.map(s => ({
      label: s,
      kind: CompletionItemKind.Module,
      documentation: md(SECTION_DOCS[s.replace(/[[\]]/g, '').split('.')[0]] ?? ''),
      insertText: s,
    }));
  }

  const section = getTomlSectionAt(lines, li);
  if (!section || section === 'profile') return [];

  const keys = SECTION_KEYS[section] ?? {};

  return Object.entries(keys).map(([key, info]) => ({
    label: key,
    kind: CompletionItemKind.Property,
    detail: `${info.type} — default: ${info.default}`,
    documentation: md(info.desc),
    insertText: `${key} = ${info.default.startsWith('"') ? info.default : `\${1:${info.default}}`}`,
    insertTextFormat: InsertTextFormat.Snippet,
  }));
}

// ─── Hover ────────────────────────────────────────────────────────────────────

export function getTomlHover(
  params: { position: { line: number; character: number }; textDocument: { uri: string } },
  documents: TextDocuments<TextDocument>,
): Hover | null {
  const doc = documents.get(params.textDocument.uri);
  if (!doc) return null;

  const text = doc.getText();
  const lines = text.split('\n');
  const { line: li, character: ci } = params.position;
  const line = lines[li] ?? '';

  // Hover on a section header
  if (/^\s*\[/.test(line)) {
    const rawSection = line.trim().replace(/[[\]]/g, '').split('.')[0];
    const desc = SECTION_DOCS[rawSection];
    if (desc) return hover(`**\`[${rawSection}]\`**\n\n${desc}`);
    return null;
  }

  // Hover on a key name
  const eqIdx = line.indexOf('=');
  if (eqIdx === -1) return null;

  const key = line.slice(0, eqIdx).trim();
  if (ci > eqIdx) return null; // cursor is on the value side

  const section = getTomlSectionAt(lines, li);
  if (!section || section === 'profile') return null;

  const info = (SECTION_KEYS[section] ?? {})[key];
  if (!info) return null;

  return hover([
    `**\`${key}\`** *(${info.type})*`,
    `Default: \`${info.default}\``,
    '',
    info.desc,
  ].join('\n'));
}
