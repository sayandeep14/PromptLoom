import * as fs from 'fs';
import * as path from 'path';
import { Diagnostic, DiagnosticSeverity } from 'vscode-languageserver-types';
import { TextDocument } from 'vscode-languageserver-textdocument';
import { parseLoomDocument, LoomNode } from '../src/server/parser';
import { LoomRegistry } from '../src/server/registry';
import { validateDocument } from '../src/server/validator';
import { DEFAULT_CONFIG, LoomConfig } from '../src/server/toml-config';

/** Config that stays quiet about missing objective/format so tests only see what they test. */
export const QUIET: LoomConfig = {
  ...DEFAULT_CONFIG,
  validation: { ...DEFAULT_CONFIG.validation, require_objective: false, require_format: false, require_contract: false },
};

export interface Analysis {
  nodes: LoomNode[];
  diags: Diagnostic[];
  errors: string[];
  warnings: string[];
}

/** Validate one document (optionally alongside other files that populate the registry). */
export function analyze(text: string, others: Record<string, string> = {}, uri = 'file:///t/main.prompt.loom'): Analysis {
  const registry = new LoomRegistry();
  for (const [u, t] of Object.entries(others)) {
    registry.updateFile(u, parseLoomDocument(t, u).nodes, []);
  }
  const nodes = parseLoomDocument(text, uri).nodes;
  const diags = validateDocument(nodes, text, uri, registry, QUIET);
  return {
    nodes, diags,
    errors:   diags.filter(d => d.severity === DiagnosticSeverity.Error).map(d => d.message),
    warnings: diags.filter(d => d.severity === DiagnosticSeverity.Warning).map(d => d.message),
  };
}

export function applyEdits(text: string, edits: { range: any; newText: string }[]): string {
  const doc = TextDocument.create('file:///t/x.loom', 'loom', 1, text);
  return TextDocument.applyEdits(doc, edits);
}

/** Every .loom file under dir (recursive), as { uri: text }. */
export function loomFiles(dir: string): Record<string, string> {
  const out: Record<string, string> = {};
  const walk = (d: string) => {
    for (const e of fs.readdirSync(d, { withFileTypes: true })) {
      const p = path.join(d, e.name);
      if (e.isDirectory()) walk(p);
      else if (e.isFile() && e.name.endsWith('.loom') && !e.name.endsWith('.vars.loom')) out['file://' + p] = fs.readFileSync(p, 'utf8');
    }
  };
  walk(dir);
  return out;
}

export const TESTDATA = path.join(__dirname, '..', '..', 'testdata');
