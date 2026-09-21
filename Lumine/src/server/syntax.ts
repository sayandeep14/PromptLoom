// A port of the structural rules of internal/lexer/lexer.go (plus the "keyword only valid in
// prompts" and "unexpected end of file" errors of internal/parser/parser.go). These are the
// errors `loom inspect` reports as load errors: nothing else is checked until they are fixed,
// so, like the CLI, this reports the FIRST one only. Messages are identical to the Go ones.

export interface SyntaxError {
  /** 0-based line */
  line: number;
  message: string;
}

const enum S { Top, Body, FieldContent, NestedBody, NestedFieldContent }

function indentOf(line: string): number {
  for (let i = 0; i < line.length; i++) if (line[i] !== ' ' && line[i] !== '\t') return i;
  return -1;
}

function isIdent(s: string): boolean {
  return s !== '' && /^[\p{L}\p{N}_-]+$/u.test(s);
}

function isNamespacedIdent(s: string): boolean {
  const dot = s.indexOf('.');
  if (dot > 0) return isIdent(s.slice(0, dot)) && isIdent(s.slice(dot + 1));
  const slash = s.indexOf('/');
  if (slash > 0) return isIdent(s.slice(0, slash)) && isIdent(s.slice(slash + 1));
  return isIdent(s);
}

/** A field declaration: `name :=`, `name +=`, `name -=`, or a bare `name:`. */
function parseFieldDecl(trimmed: string): boolean {
  for (const op of [':=', '+=', '-=']) {
    const idx = trimmed.indexOf(op);
    if (idx > 0 && isIdent(trimmed.slice(0, idx).replace(/[ \t]+$/, ''))) return true;
  }
  const bare = trimmed.replace(/[ \t]+$/, '');
  if (bare.endsWith(':') && !bare.slice(0, -1).includes(':') && isIdent(bare.slice(0, -1))) return true;
  return false;
}

function stripInlineComment(s: string): string {
  let out = '';
  let inString = false;
  let escaped = false;
  for (const ch of s) {
    if (escaped) { out += ch; escaped = false; continue; }
    if (ch === '\\') { out += ch; escaped = true; continue; }
    if (ch === '"') { inString = !inString; out += ch; continue; }
    if (ch === '#' && !inString) return out.trim();
    out += ch;
  }
  return out.trim();
}

/** Go's strconv.Unquote for the shapes that matter here: a "..." literal with escapes. */
function validQuoted(raw: string): boolean {
  if (raw.length < 2 || !raw.startsWith('"') || !raw.endsWith('"')) return false;
  for (let i = 1; i < raw.length - 1; i++) {
    if (raw[i] === '\\') { i++; continue; }
    if (raw[i] === '"') return false;
  }
  return !(raw.length >= 2 && raw[raw.length - 2] === '\\' && raw[raw.length - 3] !== '\\');
}

function varLineOk(trimmed: string): string | undefined {
  // returns an error message, or undefined when valid
  const restRaw = trimmed.replace(/^var/, '').trim();
  const rest = stripInlineComment(restRaw);
  const eq = rest.indexOf('=');
  if (eq < 0) return `invalid var declaration: ${JSON.stringify(trimmed)}`;
  const name = rest.slice(0, eq).trim();
  if (!isIdent(name)) return `invalid var declaration: ${JSON.stringify(trimmed)}`;
  const value = rest.slice(eq + 1).trim();
  if (value.startsWith('"') && !validQuoted(value)) return `invalid var declaration: invalid syntax`;
  return undefined;
}

function slotLineOk(trimmed: string): boolean {
  const rest = trimmed.replace(/^slot/, '').trim();
  const open = rest.indexOf('{');
  const close = rest.lastIndexOf('}');
  if (open < 0 && close < 0) return isIdent(rest);      // bare `slot name`
  if (open < 0 || close < 0 || close < open) return false;
  return isIdent(rest.slice(0, open).trim());
}

const PROMPT_ONLY = ['use', 'var', 'slot', 'variant', 'contract', 'capabilities', 'env'];

/** The first load error of `text`, or undefined when it lexes and nests correctly. */
export function findSyntaxError(text: string): SyntaxError | undefined {
  const lines = text.split('\n');
  let state = S.Top;
  let fieldIndent = 0;
  let nestedFieldIndent = 0;
  let fromDepth = 0;
  let inFrom = false;
  let awaitFirst = false;
  let declKind = '';
  let declName = '';
  let declLine = 0;

  const fail = (i: number, message: string): SyntaxError => ({ line: i, message });

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i].replace(/\r$/, '');
    const indent = indentOf(raw);
    const trimmed = raw.trim();

    if (trimmed === '') {
      if (state === S.FieldContent) state = S.Body;
      else if (state === S.NestedFieldContent) state = S.NestedBody;
      continue;
    }
    if (trimmed.startsWith('//')) continue;

    if (state === S.Top) {
      const parts = trimmed.split(/\s+/);
      switch (parts[0]) {
        case 'prompt': {
          if (parts[parts.length - 1] !== '{') return fail(i, `expected '{' at end of prompt declaration, got ${JSON.stringify(parts[parts.length - 1])}`);
          if (parts.length < 3) return fail(i, `invalid prompt declaration: ${JSON.stringify(trimmed)}`);
          if (!isIdent(parts[1])) return fail(i, `expected prompt name, got ${JSON.stringify(parts[1])}`);
          if (parts.length === 3) {
            // no inheritance
          } else if (parts[2] === 'inherits' || parts[2] === 'extends') {
            // `extends` is v1: the legacy check reports it (and offers the fix) instead of a load error
            const names = parts.slice(3, -1).join(' ').split(',');
            for (const raw of names) {
              const pn = raw.trim();
              if (pn === '') return fail(i, 'empty parent name in inherits list');
              if (/\s/.test(pn)) return fail(i, `parent names must be separated by commas, got ${JSON.stringify(pn)} (write ${JSON.stringify(pn.split(/\s+/).join(', '))})`);
              if (!isNamespacedIdent(pn)) return fail(i, `expected parent prompt name, got ${JSON.stringify(pn)}`);
            }
          } else {
            return fail(i, `invalid prompt declaration: ${JSON.stringify(trimmed)}`);
          }
          declKind = 'prompt'; declName = parts[1]; declLine = i;
          state = S.Body;
          break;
        }
        case 'block':
        case 'overlay': {
          const kw = parts[0];
          if (parts.length !== 3 || parts[2] !== '{') return fail(i, `expected '${kw} Name {', got ${JSON.stringify(trimmed)}`);
          if (!isIdent(parts[1])) return fail(i, `expected ${kw} name, got ${JSON.stringify(parts[1])}`);
          declKind = kw; declName = parts[1]; declLine = i;
          state = S.Body;
          break;
        }
        default:
          return fail(i, `unexpected token at top level: ${JSON.stringify(trimmed)}`);
      }
      continue;
    }

    if (state === S.FieldContent) {
      if (trimmed === '}' && fromDepth > 0) { fromDepth--; continue; }
      if (indent <= fieldIndent) {
        state = S.Body;               // fall through to body handling below
      } else {
        if (awaitFirst) { inFrom = /^(from\(|parent\[)/.test(trimmed); awaitFirst = false; }
        if (inFrom && trimmed.endsWith('{')) fromDepth++;
        continue;
      }
    }

    if (state === S.NestedFieldContent) {
      if (indent <= nestedFieldIndent) state = S.NestedBody;
      else continue;
    }

    if (state === S.Body) {
      if (trimmed === '}') { state = S.Top; continue; }
      const parts = trimmed.split(/\s+/);

      // keywords that only make sense in a prompt
      if (declKind !== 'prompt') {
        const kw = parts[0];
        const isKw =
          (kw === 'use' && parts.length === 2) || (kw === 'var' && trimmed.startsWith('var ')) ||
          (kw === 'slot' && trimmed.startsWith('slot ')) ||
          (kw === 'variant' && parts.length === 3 && parts[2] === '{') ||
          (kw === 'env' && parts.length === 3 && parts[2] === '{') ||
          trimmed === 'contract {' || trimmed === 'capabilities {';
        if (isKw && PROMPT_ONLY.includes(kw)) return fail(i, `'${kw}' is only valid inside prompts`);
      }

      if (parts.length === 2 && parts[0] === 'use') {
        if (!isNamespacedIdent(parts[1])) return fail(i, `expected block name after 'use', got ${JSON.stringify(parts[1])}`);
        continue;
      }
      if (trimmed.startsWith('var ')) {
        const err = varLineOk(trimmed);
        if (err) return fail(i, err);
        continue;
      }
      if (trimmed.startsWith('slot ')) {
        if (!slotLineOk(trimmed)) return fail(i, `invalid slot declaration: ${JSON.stringify(trimmed)}`);
        continue;
      }
      if (parts.length === 3 && (parts[0] === 'variant' || parts[0] === 'env') && parts[2] === '{') {
        if (!isIdent(parts[1])) return fail(i, `expected ${parts[0]} name, got ${JSON.stringify(parts[1])}`);
        state = S.NestedBody;
        continue;
      }
      if (trimmed === 'contract {' || trimmed === 'capabilities {') { state = S.NestedBody; continue; }
      if (trimmed.startsWith('tags:')) continue;

      if (parseFieldDecl(trimmed)) {
        // inline content after the operator (`name := from(...)`) decides whether braces are structural
        let inline = '';
        for (const op of [':=', '+=', '-=']) {
          const idx = trimmed.indexOf(op);
          if (idx > 0 && isIdent(trimmed.slice(0, idx).replace(/[ \t]+$/, ''))) { inline = trimmed.slice(idx + op.length).trim(); break; }
        }
        inFrom = /^(from\(|parent\[)/.test(inline);
        awaitFirst = inline === '';
        fromDepth = 0;
        if (inFrom && inline.endsWith('{')) fromDepth++;
        state = S.FieldContent;
        fieldIndent = indent;
        continue;
      }
      return fail(i, `unexpected token in body: ${JSON.stringify(trimmed)}`);
    }

    if (state === S.NestedBody) {
      if (trimmed === '}') { state = S.Body; continue; }
      if (parseFieldDecl(trimmed)) { state = S.NestedFieldContent; nestedFieldIndent = indent; continue; }
      return fail(i, `unexpected token in nested body: ${JSON.stringify(trimmed)}`);
    }
  }

  if (state !== S.Top) {
    return fail(lines.length - 1,
      `unexpected end of file inside body of ${JSON.stringify(declName)} (declared at line ${declLine + 1}) — add the missing closing '}'`);
  }
  return undefined;
}
