import * as vscode from 'vscode';
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
  TransportKind,
} from 'vscode-languageclient/node';

let client: LanguageClient;

export function activate(context: vscode.ExtensionContext) {
  const serverModule = context.asAbsolutePath('dist/server.js');

  const serverOptions: ServerOptions = {
    run: { module: serverModule, transport: TransportKind.ipc },
    debug: {
      module: serverModule,
      transport: TransportKind.ipc,
      options: { execArgv: ['--nolazy', '--inspect=6009'] },
    },
  };

  const clientOptions: LanguageClientOptions = {
    documentSelector: [
      { scheme: 'file', language: 'loom' },
      { scheme: 'file', pattern: '**/loom.toml' },
    ],
    synchronize: {
      fileEvents: vscode.workspace.createFileSystemWatcher(
        '**/*.{prompt.loom,block.loom,overlay.loom,vars.loom,loom,toml}',
      ),
    },
  };

  client = new LanguageClient(
    'lumine',
    'Lumine Language Server',
    serverOptions,
    clientOptions,
  );

  client.start();

  // ── Format on save ────────────────────────────────────────────────────────
  const formatOnSaveDisposable = vscode.workspace.onWillSaveTextDocument(event => {
    const config = vscode.workspace.getConfiguration('loom', event.document.uri);
    if (!config.get<boolean>('formatOnSave', false)) return;
    if (event.document.languageId !== 'loom') return;

    event.waitUntil(
      vscode.commands.executeCommand<vscode.TextEdit[]>(
        'vscode.executeFormatDocumentProvider',
        event.document.uri,
        { insertSpaces: true, tabSize: 2 },
      ).then(edits => edits ?? []),
    );
  });

  context.subscriptions.push(
    formatOnSaveDisposable,
    vscode.commands.registerCommand('loom.weave',     () => runLoomWeave()),
    vscode.commands.registerCommand('loom.inspect',   () => runLoomCommand('inspect')),
    vscode.commands.registerCommand('loom.openGraph', () => runLoomCommand('graph')),
  );
}

export function deactivate(): Thenable<void> | undefined {
  return client?.stop();
}

// ─── CLI helpers ──────────────────────────────────────────────────────────────

function getLoomBin(): string {
  return vscode.workspace.getConfiguration('loom').get<string>('loomExecutable', 'loom');
}

function getOrCreateTerminal(): vscode.Terminal {
  const existing = vscode.window.terminals.find(t => t.name === 'Loom');
  return existing ?? vscode.window.createTerminal('Loom');
}

function runLoomWeave(): void {
  const editor = vscode.window.activeTextEditor;
  const bin = getLoomBin();
  const terminal = getOrCreateTerminal();

  if (editor && editor.document.languageId === 'loom') {
    const filePath = editor.document.uri.fsPath;
    terminal.sendText(`${bin} weave "${filePath}"`);
  } else {
    terminal.sendText(`${bin} weave`);
  }

  terminal.show(true);
}

function runLoomCommand(cmd: string): void {
  const bin = getLoomBin();
  const terminal = getOrCreateTerminal();
  terminal.sendText(`${bin} ${cmd}`);
  terminal.show(true);
}
