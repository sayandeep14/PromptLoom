// Fails if the extension package would ship the wrong files or has incomplete metadata.
//   node scripts/check-vsix.js               # verify
//   node scripts/check-vsix.js --tag lumine-v0.2.0   # also verify a release tag matches package.json
const { spawnSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const root = path.join(__dirname, '..');
const pkg = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8'));
const problems = [];

// 1. metadata the Marketplace listing needs
for (const key of ['name', 'displayName', 'description', 'version', 'publisher', 'license', 'icon', 'repository', 'bugs', 'homepage']) {
  if (!pkg[key]) problems.push(`package.json is missing "${key}"`);
}
if (pkg.icon && !fs.existsSync(path.join(root, pkg.icon))) problems.push(`icon file ${pkg.icon} does not exist`);
if (pkg.repository && !String(pkg.repository.url || pkg.repository).toLowerCase().includes('promptloom')) {
  problems.push('repository does not point at the PromptLoom monorepo');
}

// 2. the changelog documents this version
const changelog = fs.readFileSync(path.join(root, 'CHANGELOG.md'), 'utf8');
if (!changelog.includes(`## [${pkg.version}]`)) problems.push(`CHANGELOG.md has no entry for ${pkg.version}`);

// 3. a release tag must match the version
const tagIdx = process.argv.indexOf('--tag');
if (tagIdx >= 0) {
  const tag = process.argv[tagIdx + 1] || '';
  if (tag !== `lumine-v${pkg.version}`) problems.push(`tag "${tag}" does not match version ${pkg.version} (expected lumine-v${pkg.version})`);
}

// 4. what would actually be packaged (build first: `vsce ls` does not run prepublish)
const build = spawnSync(process.execPath, ['build.js', '--minify'], { cwd: root, encoding: 'utf8' });
if (build.status !== 0) problems.push('build failed:\n' + build.stderr);
const ls = spawnSync('npx', ['--no-install', 'vsce', 'ls'], { cwd: root, encoding: 'utf8', shell: process.platform === 'win32' });
if (ls.status !== 0) {
  problems.push('vsce ls failed:\n' + ls.stderr);
} else {
  const files = ls.stdout.split('\n').map(l => l.trim()).filter(Boolean);
  const has = (f) => files.includes(f);
  for (const f of ['package.json', 'README.md', 'CHANGELOG.md', 'LICENSE', 'dist/extension.js', 'dist/server.js', 'images/icon.png',
    'syntaxes/loom.tmLanguage.json', 'snippets/loom.json', 'language-configuration.json']) {
    if (!has(f) && !has(f + '.txt')) problems.push(`package is missing ${f}`);
  }
  const forbidden = [/^src\//, /^test\//, /^\.test-out\//, /^node_modules\//, /\.map$/, /^tsconfig/, /^scripts\//, /^plan\.md$/, /\.vsix$/, /^build\.js$/, /^\./];
  for (const f of files) {
    if (forbidden.some(re => re.test(f))) problems.push(`package must not contain ${f}`);
  }
  const bytes = files.reduce((n, f) => n + (fs.existsSync(path.join(root, f)) ? fs.statSync(path.join(root, f)).size : 0), 0);
  if (bytes > 2 * 1024 * 1024) problems.push(`package is unexpectedly large (${(bytes / 1024 / 1024).toFixed(1)} MB)`);
  console.log(`packaging ${files.length} files, ${(bytes / 1024).toFixed(0)} KB`);
}

if (problems.length) {
  console.error('\nPackage check FAILED:\n - ' + problems.join('\n - '));
  process.exit(1);
}
console.log(`Package check passed for ${pkg.publisher}.${pkg.name}@${pkg.version}`);
