// Bundles every test/*.test.ts with esbuild, then runs them with Node's built-in test runner.
const esbuild = require('esbuild');
const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');

// The end-to-end test talks to the built language server, so make sure it is current.
const built = spawnSync(process.execPath, ['build.js'], { cwd: path.join(__dirname, '..'), stdio: 'inherit' });
if (built.status !== 0) process.exit(built.status ?? 1);

const dir = __dirname;
const out = path.join(__dirname, '..', '.test-out');
fs.rmSync(out, { recursive: true, force: true });

const entries = fs.readdirSync(dir).filter(f => f.endsWith('.test.ts')).map(f => path.join(dir, f));
esbuild.buildSync({
  entryPoints: entries,
  outdir: out,
  bundle: true,
  platform: 'node',
  target: 'node20',
  format: 'cjs',
  external: ['vscode'],
  logLevel: 'error',
});

const files = fs.readdirSync(out).filter(f => f.endsWith('.test.js')).map(f => path.join(out, f));
const r = spawnSync(process.execPath, ['--test', ...files], { stdio: 'inherit' });
process.exit(r.status ?? 1);
