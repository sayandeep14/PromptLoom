import { Range } from 'vscode-languageserver-types';

// ─── Public Types ─────────────────────────────────────────────────────────────

export interface ParseError {
  range: Range;
  message: string;
}

export interface BlockRef {
  name: string;
  range: Range;
}

export interface FieldOp {
  fieldName: string;
  op: ':' | ':=' | '+=' | '-=';
  value: string[];
  range: Range;
  nameRange: Range;
  /** Raw first value line when the RHS starts with a from() expression (v2 DSL) */
  fromExprRaw?: string;
}

export interface VarEntry {
  name: string;
  default: string;
  isSlot: boolean;
  required: boolean;
  range: Range;
  nameRange: Range;
}

export interface VariantBlock {
  name: string;
  nameRange: Range;
  fields: FieldOp[];
  range: Range;
}

export interface ContractBlock {
  fields: FieldOp[];
  range: Range;
}

export interface CapabilitiesBlock {
  fields: FieldOp[];
  range: Range;
}

export interface LoomNode {
  kind: 'prompt' | 'block' | 'overlay';
  name: string;
  nameRange: Range;
  /** All parent names (v2 multi-parent). Aliases: parent = parents[0], parentRange = parentRanges[0] */
  parents: string[];
  parentRanges: Range[];
  /** Backward-compat alias for parents[0] */
  parent?: string;
  /** Backward-compat alias for parentRanges[0] */
  parentRange?: Range;
  uses: BlockRef[];
  fields: FieldOp[];
  vars: VarEntry[];
  variants: VariantBlock[];
  contract?: ContractBlock;
  capabilities?: CapabilitiesBlock;
  range: Range;
  bodyRange: Range;
}

export interface ParseResult {
  nodes: LoomNode[];
  globalVars: VarEntry[];
  errors: ParseError[];
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

function rng(sl: number, sc: number, el: number, ec: number): Range {
  return { start: { line: sl, character: sc }, end: { line: el, character: ec } };
}

function indentOf(line: string): number {
  let n = 0;
  for (const c of line) {
    if (c === ' ') n++;
    else if (c === '\t') n += 2;
    else break;
  }
  return n;
}

function isBlankOrComment(line: string): boolean {
  const t = line.trim();
  return !t || t.startsWith('//');
}

// Intermediate state while collecting a field declaration + its content lines
interface FieldCollector {
  fieldName: string;
  op: FieldOp['op'];
  value: string[];
  declLine: number;
  declIndent: number;
  nameRange: Range;
}

function finishField(fc: FieldCollector, lastLine: number, lastLineLen: number): FieldOp {
  const fo: FieldOp = {
    fieldName: fc.fieldName,
    op: fc.op,
    value: fc.value,
    range: rng(fc.declLine, 0, lastLine, lastLineLen),
    nameRange: fc.nameRange,
  };
  // Detect from() expression on first value line (v2 DSL)
  if (fc.op === ':=' && fc.value.length > 0) {
    const firstVal = fc.value[0].trim();
    if (firstVal.startsWith('from(') || firstVal.startsWith('parent[')) {
      fo.fromExprRaw = firstVal;
    }
  }
  return fo;
}

// ─── Regexes ──────────────────────────────────────────────────────────────────

const PROMPT_RE      = /^prompt\s+([a-zA-Z0-9_-]+)(?:\s+inherits\s+((?:[a-zA-Z0-9_./-]+)(?:\s*,\s*[a-zA-Z0-9_./-]+)*))?\s*\{?/;
const BLOCK_RE       = /^block\s+([a-zA-Z0-9_-]+)\s*\{?/;
const OVERLAY_RE     = /^overlay\s+([a-zA-Z0-9_-]+)\s*\{?/;

const USE_RE         = /^(\s+)use\s+([a-zA-Z0-9_./-]+)/;
const VAR_RE         = /^(\s+)var\s+([a-zA-Z0-9_-]+)\s*=\s*"([^"]*)"/;
const SLOT_RE        = /^(\s+)slot\s+([a-zA-Z0-9_-]+)(?:\s*\{([^}]*)\})?/;
const VARIANT_RE     = /^(\s+)variant\s+([a-zA-Z0-9_-]+)/;
const CONTRACT_RE    = /^(\s+)contract\s*\{?/;
const CAPABILITIES_RE = /^(\s+)capabilities\s*\{?/;
const FIELD_OP_RE    = /^(\s+)(summary|persona|context|objective|notes|kind|instructions|constraints|examples|format|todo|compatible_with|required_sections|forbidden_sections|must_include|must_not_include|allowed|forbidden)\s*(:=|\+=|-=|:)/;

// ─── parseFieldsOnly ─────────────────────────────────────────────────────────
// Used for variant / contract / capabilities bodies.
// Collects field declarations until it sees `}` at exactly closeIndent.
// Returns the line index of the closing `}` (or last line on EOF).

function parseFieldsOnly(
  lines: string[],
  startLine: number,
  closeIndent: number,
  fields: FieldOp[],
  _errors: ParseError[],
): number {
  let i = startLine;
  let fc: FieldCollector | null = null;

  function flush(lastLine: number) {
    if (!fc) return;
    fields.push(finishField(fc, lastLine, lines[lastLine]?.length ?? 0));
    fc = null;
  }

  while (i < lines.length) {
    const line = lines[i];

    if (isBlankOrComment(line)) {
      flush(i > 0 ? i - 1 : 0);
      i++;
      continue;
    }

    const lineIndent = indentOf(line);
    const trimmed = line.trim();

    // Closing brace at the expected indent ends this block
    if (trimmed.startsWith('}') && lineIndent === closeIndent) {
      flush(i - 1);
      return i;
    }

    // Still collecting content for the current field?
    if (fc) {
      if (lineIndent > fc.declIndent) {
        fc.value.push(trimmed);
        i++;
        continue;
      }
      flush(i - 1);
    }

    // Field declaration
    const fm = line.match(FIELD_OP_RE);
    if (fm) {
      const fieldName = fm[2];
      const op = fm[3] as FieldOp['op'];
      const nameIdx = line.indexOf(fieldName);
      fc = {
        fieldName, op,
        value: [],
        declLine: i,
        declIndent: lineIndent,
        nameRange: rng(i, nameIdx, i, nameIdx + fieldName.length),
      };
      i++;
      continue;
    }

    i++; // skip unknown line
  }

  flush(i > 0 ? i - 1 : 0);
  return Math.max(0, i - 1);
}

// ─── parseNodeBody ────────────────────────────────────────────────────────────
// Parses the body of a prompt / block / overlay until `}` at indent 0.
// Returns the line index of the closing `}`.

function parseNodeBody(lines: string[], startLine: number, node: LoomNode, errors: ParseError[]): number {
  let i = startLine;
  let fc: FieldCollector | null = null;

  function flush(lastLine: number) {
    if (!fc) return;
    node.fields.push(finishField(fc, lastLine, lines[lastLine]?.length ?? 0));
    fc = null;
  }

  while (i < lines.length) {
    const line = lines[i];

    if (isBlankOrComment(line)) {
      flush(i > 0 ? i - 1 : 0);
      i++;
      continue;
    }

    const lineIndent = indentOf(line);
    const trimmed = line.trim();

    // Node-level closing brace
    if (trimmed.startsWith('}') && lineIndent === 0) {
      flush(i - 1);
      return i;
    }

    // Still collecting field content?
    if (fc) {
      if (lineIndent > fc.declIndent) {
        fc.value.push(trimmed);
        i++;
        continue;
      }
      flush(i - 1);
    }

    // use statement
    const um = line.match(USE_RE);
    if (um) {
      const refName = um[2];
      const refIdx = line.lastIndexOf(refName);
      node.uses.push({
        name: refName,
        range: rng(i, refIdx, i, refIdx + refName.length),
      });
      i++;
      continue;
    }

    // var declaration
    const vm = line.match(VAR_RE);
    if (vm) {
      const varName = vm[2];
      const varIdx = line.indexOf(varName, line.indexOf('var') + 3);
      node.vars.push({
        name: varName,
        default: vm[3],
        isSlot: false,
        required: false,
        range: rng(i, 0, i, line.length),
        nameRange: rng(i, varIdx, i, varIdx + varName.length),
      });
      i++;
      continue;
    }

    // slot declaration
    const sm = line.match(SLOT_RE);
    if (sm) {
      const slotName = sm[2];
      const slotIdx = line.indexOf(slotName, line.indexOf('slot') + 4);
      const meta = sm[3] ?? '';
      const required = /required\s*:\s*true/.test(meta);
      const defM = meta.match(/default\s*:\s*"([^"]*)"/);
      node.vars.push({
        name: slotName,
        default: defM ? defM[1] : '',
        isSlot: true,
        required,
        range: rng(i, 0, i, line.length),
        nameRange: rng(i, slotIdx, i, slotIdx + slotName.length),
      });
      i++;
      continue;
    }

    // variant block
    const varm = line.match(VARIANT_RE);
    if (varm) {
      const vName = varm[2];
      const vNameIdx = line.indexOf(vName, line.indexOf('variant') + 7);
      const variant: VariantBlock = {
        name: vName,
        nameRange: rng(i, vNameIdx, i, vNameIdx + vName.length),
        fields: [],
        range: rng(i, 0, i, line.length),
      };
      const endLine = parseFieldsOnly(lines, i + 1, 2, variant.fields, errors);
      variant.range = rng(i, 0, endLine, lines[endLine]?.length ?? 0);
      node.variants.push(variant);
      i = endLine + 1;
      continue;
    }

    // contract block
    const cm = line.match(CONTRACT_RE);
    if (cm) {
      const contract: ContractBlock = { fields: [], range: rng(i, 0, i, line.length) };
      const endLine = parseFieldsOnly(lines, i + 1, 2, contract.fields, errors);
      contract.range = rng(i, 0, endLine, lines[endLine]?.length ?? 0);
      node.contract = contract;
      i = endLine + 1;
      continue;
    }

    // capabilities block
    const capm = line.match(CAPABILITIES_RE);
    if (capm) {
      const caps: CapabilitiesBlock = { fields: [], range: rng(i, 0, i, line.length) };
      const endLine = parseFieldsOnly(lines, i + 1, 2, caps.fields, errors);
      caps.range = rng(i, 0, endLine, lines[endLine]?.length ?? 0);
      node.capabilities = caps;
      i = endLine + 1;
      continue;
    }

    // Field declaration
    const fom = line.match(FIELD_OP_RE);
    if (fom) {
      const fieldName = fom[2];
      const op = fom[3] as FieldOp['op'];
      const nameIdx = line.indexOf(fieldName);
      fc = {
        fieldName, op,
        value: [],
        declLine: i,
        declIndent: lineIndent,
        nameRange: rng(i, nameIdx, i, nameIdx + fieldName.length),
      };
      i++;
      continue;
    }

    i++; // skip unrecognised line (error recovery)
  }

  flush(Math.max(0, i - 1));
  return Math.max(0, i - 1);
}

// ─── Public API ───────────────────────────────────────────────────────────────

export function parseLoomDocument(text: string, _uri: string): ParseResult {
  const lines = text.split('\n');
  const nodes: LoomNode[] = [];
  const errors: ParseError[] = [];
  let i = 0;

  while (i < lines.length) {
    if (isBlankOrComment(lines[i])) { i++; continue; }

    const line = lines[i];

    const pm = line.match(PROMPT_RE);
    const bm = !pm && line.match(BLOCK_RE);
    const om = !pm && !bm && line.match(OVERLAY_RE);

    if (!pm && !bm && !om) { i++; continue; }

    const kind: LoomNode['kind'] = pm ? 'prompt' : bm ? 'block' : 'overlay';
    const rawName = pm ? pm[1] : bm ? bm![1] : om![1];
    const rawParentsStr = pm ? pm[2] : undefined;

    const nameIdx = line.indexOf(rawName);

    // Parse multi-parent comma-separated list
    const parents: string[] = [];
    const parentRanges: Range[] = [];
    if (rawParentsStr) {
      const parentNames = rawParentsStr.split(',').map(s => s.trim()).filter(Boolean);
      let searchFrom = nameIdx + rawName.length;
      for (const pName of parentNames) {
        const pIdx = line.indexOf(pName, searchFrom);
        if (pIdx >= 0) {
          parents.push(pName);
          parentRanges.push(rng(i, pIdx, i, pIdx + pName.length));
          searchFrom = pIdx + pName.length;
        }
      }
    }

    // Find the line that contains the opening `{`
    let braceLineIdx = i;
    if (!line.includes('{')) {
      let j = i + 1;
      while (j < lines.length && isBlankOrComment(lines[j])) j++;
      if (j < lines.length && lines[j].includes('{')) braceLineIdx = j;
    }

    const bodyStartLine = braceLineIdx + 1;

    const node: LoomNode = {
      kind,
      name: rawName,
      nameRange: rng(i, nameIdx, i, nameIdx + rawName.length),
      parents,
      parentRanges,
      parent: parents[0],
      parentRange: parentRanges[0],
      uses: [],
      fields: [],
      vars: [],
      variants: [],
      range: rng(i, 0, i, line.length),    // updated below
      bodyRange: rng(bodyStartLine, 0, bodyStartLine, 0),
    };

    const closingLine = parseNodeBody(lines, bodyStartLine, node, errors);
    node.range = rng(i, 0, closingLine, lines[closingLine]?.length ?? 0);
    node.bodyRange = rng(bodyStartLine, 0, closingLine, 0);

    nodes.push(node);
    i = closingLine + 1;
  }

  return { nodes, globalVars: [], errors };
}

// Parses a .vars.loom file — top-level var/slot declarations only, no wrapper body.
export function parseVarsFile(text: string): VarEntry[] {
  const lines = text.split('\n');
  const vars: VarEntry[] = [];

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (isBlankOrComment(line)) continue;

    const trimmed = line.trim();

    const vm = trimmed.match(/^var\s+([a-zA-Z0-9_-]+)\s*=\s*"([^"]*)"/);
    if (vm) {
      const name = vm[1];
      const nameIdx = line.indexOf(name, line.indexOf('var') + 3);
      vars.push({
        name, default: vm[2], isSlot: false, required: false,
        range: rng(i, 0, i, line.length),
        nameRange: rng(i, nameIdx, i, nameIdx + name.length),
      });
      continue;
    }

    const sm = trimmed.match(/^slot\s+([a-zA-Z0-9_-]+)(?:\s*\{([^}]*)\})?/);
    if (sm) {
      const name = sm[1];
      const nameIdx = line.indexOf(name, line.indexOf('slot') + 4);
      const meta = sm[2] ?? '';
      const required = /required\s*:\s*true/.test(meta);
      const defM = meta.match(/default\s*:\s*"([^"]*)"/);
      vars.push({
        name, default: defM ? defM[1] : '', isSlot: true, required,
        range: rng(i, 0, i, line.length),
        nameRange: rng(i, nameIdx, i, nameIdx + name.length),
      });
    }
  }

  return vars;
}
