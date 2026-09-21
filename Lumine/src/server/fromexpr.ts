// A port of internal/parser/fromexpr.go: the `from(...)` expression language of `:=` fields.
// The extension needs the structure (not just the raw text) to check bounds and types the same
// way `loom inspect` does. Error messages are identical to the Go parser's.

export type SubKind = 'all' | 'index' | 'range';

export interface Subscript {
  kind: SubKind;
  n: number;   // index, or range start (inclusive)
  m: number;   // range end (exclusive)
}

export type FromUnit =
  | { kind: 'parentRef'; sub: Subscript }
  | { kind: 'namedRef'; name: string }
  | { kind: 'fieldRef'; source: Subscript; field: string; fieldSub: Subscript }
  | { kind: 'literal'; items: string[] };

export interface FromExpression {
  units: FromUnit[];
}

/** True when the first content line starts a from() expression rather than plain text. */
export function looksLikeFromExpr(s: string): boolean {
  const t = s.trim();
  return t.startsWith('from(') || t.startsWith('parent[');
}

type Tok = { t: 'ident' | 'int' | '(' | ')' | '[' | ']' | '{' | '}' | '.' | '*' | '..' | 'nl' | '-' | 'eof'; text: string };

class FromParser {
  private cur = 0;
  constructor(private text: string) {}

  private next(): Tok {
    while (this.cur < this.text.length && (this.text[this.cur] === ' ' || this.text[this.cur] === '\t')) this.cur++;
    if (this.cur >= this.text.length) return { t: 'eof', text: '' };
    const ch = this.text[this.cur];
    if (ch === '\n') { this.cur++; return { t: 'nl', text: '\n' }; }
    if ('()[]{}*-'.includes(ch)) { this.cur++; return { t: ch as Tok['t'], text: ch }; }
    if (ch === '.') {
      if (this.text[this.cur + 1] === '.') { this.cur += 2; return { t: '..', text: '..' }; }
      this.cur++;
      return { t: '.', text: '.' };
    }
    if (/[0-9]/.test(ch)) {
      const start = this.cur;
      while (this.cur < this.text.length && /[0-9]/.test(this.text[this.cur])) this.cur++;
      return { t: 'int', text: this.text.slice(start, this.cur) };
    }
    if (/[A-Za-z_]/.test(ch)) {
      const start = this.cur;
      while (this.cur < this.text.length && /[A-Za-z0-9_-]/.test(this.text[this.cur])) this.cur++;
      return { t: 'ident', text: this.text.slice(start, this.cur) };
    }
    throw new Error(`unexpected character ${JSON.stringify(ch)} in from() expression`);
  }

  private peek(): Tok {
    const saved = this.cur;
    try { return this.next(); } finally { this.cur = saved; }
  }

  private skipNewlines(): void {
    for (;;) {
      const saved = this.cur;
      let tok: Tok;
      try { tok = this.next(); } catch { this.cur = saved; return; }
      if (tok.t !== 'nl') { this.cur = saved; return; }
    }
  }

  private expect(t: Tok['t'], shown: string): void {
    const tok = this.next();
    if (tok.t !== t) throw new Error(`expected "${shown}", got "${tok.text}"`);
  }

  parse(): FromExpression {
    const units: FromUnit[] = [];
    this.skipNewlines();
    for (;;) {
      units.push(this.unit());
      this.skipNewlines();
      const tok = this.peek();
      if (tok.t === 'eof') break;
      if (tok.t !== 'ident' || tok.text !== 'and') throw new Error(`expected 'and' between from() units, got "${tok.text}"`);
      this.next();
      this.skipNewlines();
    }
    return { units };
  }

  private unit(): FromUnit {
    const tok = this.peek();
    if (tok.t === 'ident' && tok.text === 'from') return this.fromCall();
    if (tok.t === '{') return this.literal();
    if (tok.t === 'ident' && tok.text === 'parent') return this.fieldRef();
    throw new Error(`expected 'from', 'parent[', or '{' in from() expression, got "${tok.text}"`);
  }

  private fromCall(): FromUnit {
    this.next(); // from
    this.expect('(', '(');
    const tok = this.peek();
    let unit: FromUnit;
    if (tok.t === 'ident' && tok.text === 'parent') {
      this.next();
      unit = { kind: 'parentRef', sub: this.subscript() };
    } else if (tok.t === 'ident') {
      unit = { kind: 'namedRef', name: this.namespacedIdent() };
    } else {
      throw new Error(`expected 'parent' or parent name in from(), got "${tok.text}"`);
    }
    this.expect(')', ')');
    return unit;
  }

  private fieldRef(): FromUnit {
    this.next(); // parent
    const source = this.subscript();
    this.expect('.', '.');
    const field = this.next();
    if (field.t !== 'ident') throw new Error(`expected field name after '.', got "${field.text}"`);
    return { kind: 'fieldRef', source, field: field.text, fieldSub: this.subscript() };
  }

  private literal(): FromUnit {
    this.next(); // {
    const items: string[] = [];
    for (;;) {
      this.skipNewlines();
      const tok = this.peek();
      if (tok.t === '}') { this.next(); break; }
      if (tok.t === 'eof') throw new Error('unexpected end of from() literal block');
      if (tok.t !== '-') throw new Error(`expected '- item' in literal block, got "${tok.text}"`);
      this.next();
      const start = this.cur;
      while (this.cur < this.text.length && this.text[this.cur] !== '\n') this.cur++;
      items.push(this.text.slice(start, this.cur).trim());
    }
    return { kind: 'literal', items };
  }

  private subscript(): Subscript {
    this.expect('[', '[');
    const tok = this.next();
    let sub: Subscript;
    if (tok.t === '*') {
      sub = { kind: 'all', n: 0, m: 0 };
    } else if (tok.t === 'int') {
      const n = parseInt(tok.text, 10);
      if (this.peek().t === '..') {
        this.next();
        const mTok = this.next();
        if (mTok.t !== 'int') throw new Error(`expected integer after '..' in subscript, got "${mTok.text}"`);
        sub = { kind: 'range', n, m: parseInt(mTok.text, 10) };
      } else {
        sub = { kind: 'index', n, m: 0 };
      }
    } else {
      throw new Error(`expected '*' or integer in subscript, got "${tok.text}"`);
    }
    this.expect(']', ']');
    return sub;
  }

  private namespacedIdent(): string {
    const tok = this.next();
    if (tok.t !== 'ident') throw new Error(`expected identifier, got "${tok.text}"`);
    let name = tok.text;
    if (this.peek().t === '.') {
      this.next();
      const t2 = this.next();
      if (t2.t !== 'ident') throw new Error(`expected identifier after '.', got "${t2.text}"`);
      name += '.' + t2.text;
    }
    return name;
  }
}

/** Parses the lines of a `:=` field body that starts with a from() expression. Throws Error. */
export function parseFromExpression(lines: string[]): FromExpression {
  return new FromParser(lines.join('\n')).parse();
}
