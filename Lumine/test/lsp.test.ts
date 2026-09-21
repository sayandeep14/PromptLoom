import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn, ChildProcessWithoutNullStreams } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { applyEdits } from './helpers';

/**
 * End-to-end: spawn the BUILT language server (dist/server.js, the file that ships in the
 * VSIX) and talk real LSP to it over stdio.
 */

class LspClient {
  private proc: ChildProcessWithoutNullStreams;
  private buf = Buffer.alloc(0);
  private nextId = 1;
  private pending = new Map<number, (v: any) => void>();
  private notifications: { method: string; params: any }[] = [];
  private waiters: (() => void)[] = [];

  constructor(serverJs: string) {
    this.proc = spawn(process.execPath, [serverJs, '--stdio']);
    this.proc.stdout.on('data', (d: Buffer) => { this.buf = Buffer.concat([this.buf, d]); this.drain(); });
    this.proc.stderr.on('data', () => { /* server logs are not part of the protocol */ });
  }

  private drain(): void {
    for (;;) {
      const headerEnd = this.buf.indexOf('\r\n\r\n');
      if (headerEnd < 0) return;
      const m = /Content-Length: (\d+)/i.exec(this.buf.slice(0, headerEnd).toString());
      if (!m) throw new Error('bad LSP header');
      const len = Number(m[1]);
      const start = headerEnd + 4;
      if (this.buf.length < start + len) return;
      const msg = JSON.parse(this.buf.slice(start, start + len).toString());
      this.buf = this.buf.slice(start + len);
      if (msg.id !== undefined && (msg.result !== undefined || msg.error !== undefined) && this.pending.has(msg.id)) {
        this.pending.get(msg.id)!(msg.result ?? null);
        this.pending.delete(msg.id);
      } else if (msg.id !== undefined && msg.method) {
        // server -> client request (e.g. client/registerCapability): acknowledge
        this.send({ jsonrpc: '2.0', id: msg.id, result: null });
      } else if (msg.method) {
        this.notifications.push({ method: msg.method, params: msg.params });
        this.waiters.splice(0).forEach(w => w());
      }
    }
  }

  private send(obj: unknown): void {
    const body = Buffer.from(JSON.stringify(obj));
    this.proc.stdin.write(`Content-Length: ${body.length}\r\n\r\n`);
    this.proc.stdin.write(body);
  }

  request(method: string, params: unknown): Promise<any> {
    const id = this.nextId++;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`timeout waiting for ${method}`)), 10000);
      this.pending.set(id, v => { clearTimeout(timer); resolve(v); });
      this.send({ jsonrpc: '2.0', id, method, params });
    });
  }

  notify(method: string, params: unknown): void { this.send({ jsonrpc: '2.0', method, params }); }

  /** Wait for a notification matching the predicate (already-received ones count). */
  waitFor(method: string, pred: (p: any) => boolean = () => true): Promise<any> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`timeout waiting for ${method}`)), 10000);
      const check = () => {
        const hit = this.notifications.find(n => n.method === method && pred(n.params));
        if (hit) { clearTimeout(timer); resolve(hit.params); return; }
        this.waiters.push(check);
      };
      check();
    });
  }

  async shutdown(): Promise<void> {
    try { await this.request('shutdown', null); this.notify('exit', null); } catch { /* ignore */ }
    this.proc.kill();
  }
}

const SERVER = path.join(__dirname, '..', 'dist', 'server.js');
const V1 = [
  'prompt Base {',
  '  persona:',
  '    You review code.',
  '  instructions :=',
  '    - Read the diff.',
  '}',
  '',
  '// keep this comment',
  'prompt Child extends Base {',
  '  instructions +=',
  '    - Check errors.',
  '',
  '  env prod {',
  '    constraints :=',
  '      - No debug logging.',
  '  }',
  '}',
  '',
].join('\n');

test('built language server: diagnostics, quick fixes, fix-all and formatting over real LSP', async (t) => {
  if (!fs.existsSync(SERVER)) { t.skip('dist/server.js not built'); return; }

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'lumine-lsp-'));
  const file = path.join(dir, 'prompts', 'a.prompt.loom');
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const uri = 'file://' + file;

  const client = new LspClient(SERVER);
  try {
    const init = await client.request('initialize', {
      processId: process.pid, rootUri: 'file://' + dir,
      workspaceFolders: [{ uri: 'file://' + dir, name: 'p' }],
      capabilities: {},
    });
    // the server advertises what the extension relies on
    assert.ok(init.capabilities.codeActionProvider, 'codeActionProvider missing');
    assert.ok(init.capabilities.documentFormattingProvider);
    assert.ok(init.capabilities.completionProvider);
    client.notify('initialized', {});

    client.notify('textDocument/didOpen', { textDocument: { uri, languageId: 'loom', version: 1, text: V1 } });
    const published = await client.waitFor('textDocument/publishDiagnostics', p => p.uri === uri && p.diagnostics.length > 0);
    const byCode = (c: string) => published.diagnostics.filter((d: any) => d.code === c);
    assert.equal(byCode('legacy-colon').length, 1, JSON.stringify(published.diagnostics.map((d: any) => d.message)));
    assert.equal(byCode('legacy-append').length, 1);
    assert.equal(byCode('legacy-extends').length, 1);
    assert.match(byCode('legacy-append')[0].message, /from\(parent\[0\]\) and \{/);

    // quick fixes for the whole document
    const fullRange = { start: { line: 0, character: 0 }, end: { line: 20, character: 0 } };
    const actions: any[] = await client.request('textDocument/codeAction', {
      textDocument: { uri }, range: fullRange, context: { diagnostics: published.diagnostics },
    });
    const titles = actions.map(a => a.title);
    assert.ok(titles.some(x => x.includes('persona :=')), titles.join(' | '));
    assert.ok(titles.some(x => x.includes('inherits')));
    assert.ok(titles.some(x => x.startsWith('Fix all v1 syntax')));

    // applying fix-all yields v2 that keeps the comment and the env block
    const fixAll = actions.find(a => a.kind === 'source.fixAll')!;
    const fixed = applyEdits(V1, fixAll.edit.changes[uri]);
    assert.match(fixed, /persona :=/);
    assert.match(fixed, /prompt Child inherits Base \{/);
    assert.match(fixed, /instructions :=\n\s+from\(parent\[0\]\) and \{\n\s+- Check errors\.\n\s+\}/);
    assert.match(fixed, /\/\/ keep this comment/);
    assert.match(fixed, /env prod \{/);

    // and after the edit the server reports nothing left
    client.notify('textDocument/didChange', {
      textDocument: { uri, version: 2 }, contentChanges: [{ text: fixed }],
    });
    const clean = await client.waitFor('textDocument/publishDiagnostics',
      p => p.uri === uri && !p.diagnostics.some((d: any) => String(d.code).startsWith('legacy-')));
    // (objective/format warnings from the default config may remain; nothing v1 does)
    assert.deepEqual(clean.diagnostics.filter((d: any) => d.severity === 1), []);

    // formatting is lossless over the wire
    const messy = fixed.replace('persona :=', 'persona:=  ').replace('\n\n  env prod', '\n\n\n\n  env prod');
    client.notify('textDocument/didChange', { textDocument: { uri, version: 3 }, contentChanges: [{ text: messy }] });
    const edits: any[] = await client.request('textDocument/formatting', {
      textDocument: { uri }, options: { tabSize: 2, insertSpaces: true },
    });
    const formatted = applyEdits(messy, edits);
    assert.equal(formatted.replace(/\s+/g, ''), messy.replace(/\s+/g, ''), 'formatting lost content');
    assert.match(formatted, /persona :=\n/);
    assert.match(formatted, /\/\/ keep this comment/);
  } finally {
    await client.shutdown();
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
