import { LoomNode, FieldOp, VarEntry, VariantBlock, ContractBlock, CapabilitiesBlock } from './parser';

// ─── Indentation constants ────────────────────────────────────────────────────

const I1 = '  ';    // 2 spaces — top-level body elements
const I2 = '    ';  // 4 spaces — field content / nested declarations
const I3 = '      '; // 6 spaces — nested field content

// ─── Field formatting ─────────────────────────────────────────────────────────

function formatFieldLines(field: FieldOp, declIndent: string, contentIndent: string): string[] {
  const lines: string[] = [`${declIndent}${field.fieldName}${field.op}`];
  for (const v of field.value) {
    lines.push(`${contentIndent}${v}`);
  }
  return lines;
}

function formatFields(fields: FieldOp[], declIndent: string, contentIndent: string): string[] {
  const out: string[] = [];
  for (let i = 0; i < fields.length; i++) {
    if (i > 0) out.push('');
    out.push(...formatFieldLines(fields[i], declIndent, contentIndent));
  }
  return out;
}

// ─── Var/slot formatting ──────────────────────────────────────────────────────

function formatVar(v: VarEntry, indent: string): string {
  if (v.isSlot) {
    const parts = [`required: ${v.required}`];
    if (v.default) parts.push(`default: "${v.default}"`);
    return `${indent}slot ${v.name} { ${parts.join(' ')} }`;
  }
  return `${indent}var ${v.name} = "${v.default}"`;
}

// ─── Sub-block formatting ─────────────────────────────────────────────────────

function formatVariant(v: VariantBlock): string[] {
  const lines: string[] = [`${I1}variant ${v.name} {`];
  const body = formatFields(v.fields, I2, I3);
  lines.push(...body);
  lines.push(`${I1}}`);
  return lines;
}

function formatContractOrCaps(keyword: string, fields: FieldOp[]): string[] {
  const lines: string[] = [`${I1}${keyword} {`];
  const body = formatFields(fields, I2, I3);
  lines.push(...body);
  lines.push(`${I1}}`);
  return lines;
}

// ─── Node body formatting ─────────────────────────────────────────────────────

function formatNodeBody(node: LoomNode): string[] {
  // Groups are assembled separately, then joined with a blank line between non-empty groups.
  const groups: string[][] = [];

  // 1. vars / slots
  if (node.vars.length > 0) {
    groups.push(node.vars.map(v => formatVar(v, I1)));
  }

  // 2. use statements
  if (node.uses.length > 0) {
    groups.push(node.uses.map(u => `${I1}use ${u.name}`));
  }

  // 3. field operations
  if (node.fields.length > 0) {
    const fieldLines: string[] = [];
    for (let i = 0; i < node.fields.length; i++) {
      if (i > 0) fieldLines.push('');
      fieldLines.push(...formatFieldLines(node.fields[i], I1, I2));
    }
    groups.push(fieldLines);
  }

  // 4. variant blocks
  for (const v of node.variants) {
    groups.push(formatVariant(v));
  }

  // 5. contract block
  if (node.contract) {
    groups.push(formatContractOrCaps('contract', node.contract.fields));
  }

  // 6. capabilities block
  if (node.capabilities) {
    groups.push(formatContractOrCaps('capabilities', node.capabilities.fields));
  }

  // Join groups with blank lines
  const out: string[] = [];
  for (let i = 0; i < groups.length; i++) {
    if (i > 0) out.push('');
    out.push(...groups[i]);
  }
  return out;
}

// ─── Node formatting ──────────────────────────────────────────────────────────

function formatNode(node: LoomNode): string {
  const header = node.parent
    ? `${node.kind} ${node.name} inherits ${node.parent} {`
    : `${node.kind} ${node.name} {`;

  const bodyLines = formatNodeBody(node);
  const inner = bodyLines.length > 0 ? '\n' + bodyLines.join('\n') + '\n' : '\n';
  return `${header}${inner}}`;
}

// ─── Public entry point ───────────────────────────────────────────────────────

export function formatNodes(nodes: LoomNode[]): string {
  return nodes.map(formatNode).join('\n\n') + '\n';
}
