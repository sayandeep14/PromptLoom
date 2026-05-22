const esbuild = require('esbuild');

const watch = process.argv.includes('--watch');
const minify = process.argv.includes('--minify');

const shared = {
  bundle: true,
  external: ['vscode'],
  platform: 'node',
  target: 'node20',
  sourcemap: !minify,
  minify,
};

async function build() {
  const ctxExt = await esbuild.context({
    ...shared,
    entryPoints: ['src/extension.ts'],
    outfile: 'dist/extension.js',
  });

  const ctxSrv = await esbuild.context({
    ...shared,
    entryPoints: ['src/server/server.ts'],
    outfile: 'dist/server.js',
  });

  if (watch) {
    await Promise.all([ctxExt.watch(), ctxSrv.watch()]);
    console.log('[esbuild] watching extension + server...');
  } else {
    await ctxExt.rebuild();
    await ctxSrv.rebuild();
    await ctxExt.dispose();
    await ctxSrv.dispose();
    console.log('[esbuild] build complete (extension + server)');
  }
}

build().catch((err) => {
  console.error(err);
  process.exit(1);
});
