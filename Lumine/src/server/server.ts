import {
  createConnection,
  TextDocuments,
  ProposedFeatures,
  InitializeParams,
  InitializeResult,
  TextDocumentSyncKind,
  DocumentSymbol,
  SymbolKind,
  CompletionItem,
  Location,
  TextEdit,
  Range,
  DidChangeWatchedFilesNotification,
  FileChangeType,
  CodeAction,
  CodeActionKind,
} from 'vscode-languageserver/node';
import { TextDocument } from 'vscode-languageserver-textdocument';
import * as fs from 'fs';
import * as path from 'path';
import { fileURLToPath, pathToFileURL } from 'url';

import {
  parseLoomDocument,
  parseVarsFile,
  LoomNode,
  FieldOp,
  VarEntry,
} from './parser';
import { LoomRegistry } from './registry';
import { validateDocument } from './validator';
import { LoomConfig, readLoomConfig } from './toml-config';
import { getCompletions }      from '../providers/completion';
import { getHover }            from '../providers/hover';
import { getDefinition }       from '../providers/definition';
import { getReferences }       from '../providers/references';
import { formatText }          from './formatter';
import { computeQuickFixes, fixAllEdits } from './quickfix';
import { getTomlCompletions, getTomlHover } from '../providers/toml';

// ─── Setup ────────────────────────────────────────────────────────────────────

const connection = createConnection(ProposedFeatures.all);
const documents: TextDocuments<TextDocument> = new TextDocuments(TextDocument);
const registry = new LoomRegistry();

let workspaceFolders: string[] = [];
let loomConfig: LoomConfig = readLoomConfig([]);

// ─── Lifecycle ────────────────────────────────────────────────────────────────

connection.onInitialize((params: InitializeParams): InitializeResult => {
  workspaceFolders = (params.workspaceFolders ?? [])
    .map(f => { try { return fileURLToPath(f.uri); } catch { return ''; } })
    .filter(Boolean);

  loomConfig = readLoomConfig(workspaceFolders);

  return {
    capabilities: {
      textDocumentSync: TextDocumentSyncKind.Incremental,
      documentSymbolProvider: true,
      completionProvider: {
        resolveProvider: false,
        triggerCharacters: [' ', '{', '[', '=', '.'],
      },
      hoverProvider: true,
      definitionProvider: true,
      referencesProvider: true,
      documentFormattingProvider: true,
      codeActionProvider: {
        codeActionKinds: [CodeActionKind.QuickFix, CodeActionKind.SourceFixAll],
      },
    },
  };
});

connection.onInitialized(() => {
  // Seed registry from all .loom files on disk before any document events fire
  for (const folder of workspaceFolders) {
    seedFolder(folder);
  }

  // Watch loom.toml for changes
  connection.client.register(DidChangeWatchedFilesNotification.type, {
    watchers: [{ globPattern: '**/loom.toml' }],
  });
});

// ─── Workspace Scan ───────────────────────────────────────────────────────────

function seedFolder(dir: string): void {
  let entries: fs.Dirent[];
  try { entries = fs.readdirSync(dir, { withFileTypes: true }); } catch { return; }

  for (const entry of entries) {
    if (entry.name === 'node_modules') continue;

    const full = path.join(dir, entry.name);

    // Read .metadata.loom to register the pack slug for this directory
    if (entry.name === '.metadata.loom') {
      try {
        const text = fs.readFileSync(full, 'utf8');
        const meta = JSON.parse(text) as { slug?: string; name?: string };
        if (meta.slug) {
          const dirUri = pathToFileURL(dir).toString() + '/';
          registry.registerPack(meta.slug, meta.name ?? meta.slug, dirUri);
        }
      } catch { /* skip malformed */ }
      continue;
    }

    // Skip other dot-files (but NOT .metadata.loom which we handled above)
    if (entry.name.startsWith('.')) continue;

    if (entry.isDirectory()) {
      seedFolder(full);
    } else if (entry.name.endsWith('.loom')) {
      try {
        const text = fs.readFileSync(full, 'utf8');
        const uri  = pathToFileURL(full).toString();
        updateRegistry(uri, text);
      } catch { /* skip unreadable */ }
    }
  }
}

function updateRegistry(uri: string, text: string): void {
  if (uri.endsWith('.vars.loom')) {
    registry.updateFile(uri, [], parseVarsFile(text));
  } else {
    const { nodes } = parseLoomDocument(text, uri);
    registry.updateFile(uri, nodes, []);
  }
}

// ─── loom.toml watcher ────────────────────────────────────────────────────────

connection.onDidChangeWatchedFiles(event => {
  const hasTomlChange = event.changes.some(c => c.uri.endsWith('loom.toml'));
  if (hasTomlChange) {
    loomConfig = readLoomConfig(workspaceFolders);
    // Re-validate all open documents with the new config
    for (const doc of documents.all()) {
      runValidation(doc);
    }
  }
});

// ─── Validation Pipeline ──────────────────────────────────────────────────────

const debounceTimers = new Map<string, ReturnType<typeof setTimeout>>();

function scheduleValidation(doc: TextDocument): void {
  const uri = doc.uri;
  const prev = debounceTimers.get(uri);
  if (prev) clearTimeout(prev);
  debounceTimers.set(uri, setTimeout(() => {
    debounceTimers.delete(uri);
    runValidation(doc);
  }, 300));
}

function runValidation(doc: TextDocument): void {
  const uri  = doc.uri;

  // Skip loom.toml — no loom diagnostics for it
  if (uri.endsWith('loom.toml')) return;

  const text = doc.getText();

  let nodes: LoomNode[] = [];
  if (!uri.endsWith('.vars.loom')) {
    nodes = parseLoomDocument(text, uri).nodes;
  }

  // Remove stale entries BEFORE validating so cross-file duplicate detection works
  registry.removeFile(uri);
  const diags = validateDocument(nodes, text, uri, registry, loomConfig);
  // Re-add entries AFTER validation
  updateRegistry(uri, text);

  connection.sendDiagnostics({ uri, diagnostics: diags });
}

// ─── Document Events ──────────────────────────────────────────────────────────

documents.onDidOpen(event => {
  runValidation(event.document);
});

documents.onDidChangeContent(change => {
  scheduleValidation(change.document);
});

documents.onDidClose(event => {
  // Clear editor squiggles; keep registry entry (file still exists on disk)
  connection.sendDiagnostics({ uri: event.document.uri, diagnostics: [] });
});

// ─── Completions ─────────────────────────────────────────────────────────────

connection.onCompletion((params): CompletionItem[] => {
  if (params.textDocument.uri.endsWith('loom.toml')) {
    return getTomlCompletions(params, documents);
  }
  return getCompletions(params, documents, registry);
});

// ─── Hover ───────────────────────────────────────────────────────────────────

connection.onHover(params => {
  if (params.textDocument.uri.endsWith('loom.toml')) {
    return getTomlHover(params, documents);
  }
  return getHover(params, documents, registry);
});

// ─── Definition ───────────────────────────────────────────────────────────────

connection.onDefinition(params => getDefinition(params, documents, registry));

// ─── References ───────────────────────────────────────────────────────────────

connection.onReferences(params => getReferences(params, documents, registry));

// ─── Formatting ───────────────────────────────────────────────────────────────

connection.onDocumentFormatting((params): TextEdit[] => {
  const doc = documents.get(params.textDocument.uri);
  if (!doc || doc.uri.endsWith('.vars.loom') || doc.uri.endsWith('loom.toml')) return [];

  const text = doc.getText();
  const formatted = formatText(text);
  if (formatted === text) return [];

  const lastLine = doc.lineCount - 1;
  const lastChar = text.split('\n').pop()?.length ?? 0;
  const fullRange: Range = {
    start: { line: 0, character: 0 },
    end:   { line: lastLine, character: lastChar },
  };

  return [TextEdit.replace(fullRange, formatted)];
});

// ─── Code actions (v1 -> v2 quick fixes) ─────────────────────────────────────

connection.onCodeAction((params): CodeAction[] => {
  const doc = documents.get(params.textDocument.uri);
  if (!doc || doc.uri.endsWith('.vars.loom') || doc.uri.endsWith('loom.toml')) return [];

  const fixes = computeQuickFixes(doc.getText(), parseLoomDocument(doc.getText(), doc.uri).nodes);
  if (fixes.length === 0) return [];

  const overlaps = (a: Range, b: Range) =>
    !(a.end.line < b.start.line || b.end.line < a.start.line);

  const actions: CodeAction[] = [];
  for (const fix of fixes) {
    if (!overlaps(fix.anchor, params.range)) continue;
    const diagnostics = params.context.diagnostics.filter(d => d.code === fix.code && overlaps(d.range, fix.anchor));
    actions.push({
      title: fix.title,
      kind: CodeActionKind.QuickFix,
      diagnostics,
      isPreferred: true,
      edit: { changes: { [doc.uri]: fix.edits } },
    });
  }

  if (fixes.length > 1 || actions.length > 0) {
    actions.push({
      title: `Fix all v1 syntax in this file (${fixes.length})`,
      kind: CodeActionKind.SourceFixAll,
      edit: { changes: { [doc.uri]: fixAllEdits(fixes) } },
    });
  }
  return actions;
});

// ─── Document Symbols ─────────────────────────────────────────────────────────

connection.onDocumentSymbol(params => {
  const doc = documents.get(params.textDocument.uri);
  if (!doc) return [];

  const uri  = params.textDocument.uri;
  const text = doc.getText();

  if (uri.endsWith('.vars.loom')) {
    return parseVarsFile(text).map(varToSymbol);
  }

  const { nodes } = parseLoomDocument(text, uri);
  return nodes.map(nodeToSymbol);
});

// ─── Symbol Builders ─────────────────────────────────────────────────────────

function nodeToSymbol(node: LoomNode): DocumentSymbol {
  const kind =
    node.kind === 'prompt'  ? SymbolKind.Class :
    node.kind === 'block'   ? SymbolKind.Module :
                              SymbolKind.Interface;

  const detail = node.kind === 'prompt' && node.parents.length > 0
    ? `inherits ${node.parents.join(', ')}` : node.kind;

  const children: DocumentSymbol[] = [
    ...node.vars.map(varToSymbol),
    ...node.fields.map(fieldToSymbol),
    ...node.variants.map(v => {
      const s = DocumentSymbol.create(`variant ${v.name}`, '', SymbolKind.EnumMember, v.range, v.nameRange);
      s.children = v.fields.map(fieldToSymbol);
      return s;
    }),
    ...node.envBlocks.map(e => {
      const s = DocumentSymbol.create(`env ${e.name}`, '', SymbolKind.EnumMember, e.range, e.nameRange);
      s.children = e.fields.map(fieldToSymbol);
      return s;
    }),
  ];

  if (node.contract) {
    const s = DocumentSymbol.create('contract', '', SymbolKind.Struct, node.contract.range, node.contract.range);
    s.children = node.contract.fields.map(fieldToSymbol);
    children.push(s);
  }

  if (node.capabilities) {
    const s = DocumentSymbol.create('capabilities', '', SymbolKind.Struct, node.capabilities.range, node.capabilities.range);
    s.children = node.capabilities.fields.map(fieldToSymbol);
    children.push(s);
  }

  const sym = DocumentSymbol.create(node.name, detail, kind, node.range, node.nameRange);
  sym.children = children;
  return sym;
}

function fieldToSymbol(f: FieldOp): DocumentSymbol {
  return DocumentSymbol.create(`${f.fieldName} ${f.op}`, '', SymbolKind.Field, f.range, f.nameRange);
}

function varToSymbol(v: VarEntry): DocumentSymbol {
  return DocumentSymbol.create(v.name, v.isSlot ? 'slot' : 'var', SymbolKind.Variable, v.range, v.nameRange);
}

// ─── Boot ─────────────────────────────────────────────────────────────────────

documents.listen(connection);
connection.listen();
