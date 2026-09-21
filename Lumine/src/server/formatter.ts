/**
 * Conservative formatter for .loom files.
 *
 * It only normalises WHITESPACE, so it can never lose content:
 *   - trailing whitespace is removed and leading tabs become two spaces
 *   - the operator after a field name is spaced canonically:  `persona:=`  ->  `persona :=`
 *   - runs of blank lines collapse to one; blank lines directly after `{` or directly
 *     before a lone `}` are dropped; leading blank lines are removed
 *   - the file ends with exactly one newline; the file's line-ending style is kept
 *
 * Comments, `env` blocks, `tags`, every parent of `inherits A, B`, and the order of
 * body elements are left exactly as written.
 *
 * (An earlier version rebuilt the file from the parsed tree. That silently deleted
 * comments, `env` blocks and all parents after the first, so it was replaced.)
 */

const FIELD_NAMES =
  'summary|persona|context|objective|notes|kind|instructions|constraints|examples|format|todo|compatible_with|' +
  'required_sections|forbidden_sections|must_include|must_not_include|allowed|forbidden';

const FIELD_OP = new RegExp(`^(\\s+)(${FIELD_NAMES})\\s*(:=|\\+=|-=)(.*)$`);

export function formatText(text: string): string {
  const eol = text.includes('\r\n') ? '\r\n' : '\n';
  const raw = text.split(/\r?\n/);

  const lines = raw.map(line => {
    // tabs in the leading whitespace -> two spaces each
    const lead = /^[ \t]*/.exec(line)![0];
    let out = lead.replace(/\t/g, '  ') + line.slice(lead.length);
    out = out.replace(/[ \t]+$/, '');
    const m = FIELD_OP.exec(out);
    if (m) {
      const rest = m[4].trim();
      out = `${m[1]}${m[2]} ${m[3]}${rest ? ' ' + rest : ''}`;
    }
    return out;
  });

  const result: string[] = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (line === '') {
      const prev = result.length > 0 ? result[result.length - 1] : undefined;
      if (prev === undefined) continue;                 // leading blank lines
      if (prev === '') continue;                        // collapse runs
      if (prev.endsWith('{')) continue;                 // blank right after an opening brace
      // blank right before a lone closing brace
      let j = i + 1;
      while (j < lines.length && lines[j] === '') j++;
      if (j < lines.length && lines[j].trim() === '}') continue;
    }
    result.push(line);
  }
  while (result.length > 0 && result[result.length - 1] === '') result.pop();

  return result.length === 0 ? '' : result.join(eol) + eol;
}
