#!/usr/bin/env node
'use strict';

// Checks the Plutonium grammar using Node alone.
//
//   node test/grammar-check.js regexps
//       Compiles every regex in the grammar with JavaScript's RegExp and the u
//       flag, and every regex in language-configuration.json the way VS Code
//       compiles them. The grammar keeps to the syntax both Oniguruma and
//       JavaScript accept, so an Oniguruma-only construct (\h, \G, \A, \z,
//       possessive or atomic groups, POSIX classes) fails here on purpose.
//
//   node test/grammar-check.js tokens
//       Compiles every grammar regex with Oniguruma, then tokenizes
//       test/sample.p with vscode-textmate and checks the scope assertions in
//       it. Both modules are taken from an installed VS Code-family editor
//       (or the directory in $PLUTONIUM_TEXTMATE_MODULES), so nothing is
//       downloaded. Exits 3 when no usable copy is found.
//
//   node test/grammar-check.js dump [file]
//       Prints each token of a file (default test/sample.p) with its scopes.
//
// Assertions in sample.p are comment lines under the line they describe:
//
//   func main() {
//   //   ^^^^ entity.name.function
//   // <---- keyword.function
//
// Carets mark columns of the source line; "<" followed by n dashes marks
// columns 0 to n-1. Each scope after the marker must be a prefix (on dot
// boundaries) of a scope of every token in the marked range; a scope written
// with a leading "!" must not be a prefix of any of them.

const fs = require('fs');
const os = require('os');
const path = require('path');

const root = path.join(__dirname, '..');
const grammarPath = path.join(root, 'syntaxes', 'plutonium.tmLanguage.json');
const configPath = path.join(root, 'language-configuration.json');
const samplePath = path.join(__dirname, 'sample.p');

const EXIT_FAIL = 1;
const EXIT_SKIP = 3;

function readJSON(file) {
  return JSON.parse(fs.readFileSync(file, 'utf8'));
}

// collect walks a JSON value and returns every string stored under one of keys.
function collect(node, keys, where, out) {
  if (Array.isArray(node)) {
    node.forEach((n, i) => collect(n, keys, `${where}[${i}]`, out));
  } else if (node && typeof node === 'object') {
    for (const [k, v] of Object.entries(node)) {
      if (keys.includes(k) && typeof v === 'string') {
        out.push({ where: `${where}.${k}`, source: v });
      } else {
        collect(v, keys, `${where}.${k}`, out);
      }
    }
  }
  return out;
}

function grammarRegexps() {
  return collect(readJSON(grammarPath), ['match', 'begin', 'end', 'while'], 'grammar', []);
}

function checkRegexps() {
  let failed = 0;
  const grammar = grammarRegexps();
  for (const { where, source } of grammar) {
    try {
      new RegExp(source, 'u');
    } catch (err) {
      console.error(`${where}: ${err.message}`);
      failed++;
    }
  }
  const config = collect(
    readJSON(configPath),
    ['increaseIndentPattern', 'decreaseIndentPattern', 'indentNextLinePattern', 'unIndentedLinePattern',
      'start', 'end', 'wordPattern', 'beforeText', 'afterText', 'previousLineText'],
    'language-configuration', []);
  for (const { where, source } of config) {
    try {
      new RegExp(source);
    } catch (err) {
      console.error(`${where}: ${err.message}`);
      failed++;
    }
  }
  if (failed > 0) {
    console.error(`${failed} regexps failed to compile`);
    return EXIT_FAIL;
  }
  console.log(`ok: ${grammar.length} grammar and ${config.length} language-configuration regexps compile`);
  return 0;
}

// moduleDirs lists node_modules directories of editors that bundle
// vscode-textmate and vscode-oniguruma outside an asar archive.
function moduleDirs() {
  const dirs = [];
  if (process.env.PLUTONIUM_TEXTMATE_MODULES) {
    dirs.push(process.env.PLUTONIUM_TEXTMATE_MODULES);
  }
  for (const app of [
    '/usr/share/cursor/resources/app',
    '/opt/cursor/resources/app',
    '/usr/share/code/resources/app',
    '/opt/visual-studio-code/resources/app',
    '/usr/lib/code',
    '/usr/share/codium/resources/app',
    '/opt/vscodium-bin/resources/app',
    '/Applications/Visual Studio Code.app/Contents/Resources/app',
    '/Applications/Cursor.app/Contents/Resources/app',
    path.join(os.homedir(), '.local/share/cursor/resources/app'),
  ]) {
    dirs.push(path.join(app, 'node_modules'));
  }
  return dirs;
}

function findModules() {
  for (const dir of moduleDirs()) {
    const tm = path.join(dir, 'vscode-textmate', 'release', 'main.js');
    const onig = path.join(dir, 'vscode-oniguruma', 'release', 'main.js');
    const wasm = path.join(dir, 'vscode-oniguruma', 'release', 'onig.wasm');
    if (fs.existsSync(tm) && fs.existsSync(onig) && fs.existsSync(wasm)) {
      return { dir, tm, onig, wasm };
    }
  }
  return null;
}

async function loadGrammar(mods) {
  const vsctm = require(mods.tm);
  const oniguruma = require(mods.onig);
  const wasm = fs.readFileSync(mods.wasm);
  await oniguruma.loadWASM(wasm.buffer.slice(wasm.byteOffset, wasm.byteOffset + wasm.byteLength));
  const registry = new vsctm.Registry({
    onigLib: Promise.resolve({
      createOnigScanner: (sources) => new oniguruma.OnigScanner(sources),
      createOnigString: (s) => new oniguruma.OnigString(s),
    }),
    loadGrammar: async (scopeName) => {
      if (scopeName !== 'source.plutonium') {
        return null;
      }
      return vsctm.parseRawGrammar(fs.readFileSync(grammarPath, 'utf8'), grammarPath);
    },
  });
  const grammar = await registry.loadGrammar('source.plutonium');
  return { vsctm, oniguruma, grammar };
}

function tokenize(vsctm, grammar, text) {
  let state = vsctm.INITIAL;
  return text.split('\n').map((line) => {
    const r = grammar.tokenizeLine(line, state);
    state = r.ruleStack;
    return { line, tokens: r.tokens };
  });
}

const assertionRE = /^\s*\/\/\s*(?:(<-+)|(\^+))((?:\s+!?[\w.-]+)+)\s*$/;

function hasScope(scopes, want) {
  return scopes.some((s) => s === want || s.startsWith(want + '.'));
}

function checkAssertions(lines) {
  let failed = 0;
  let checked = 0;
  let source = null;
  lines.forEach(({ line, tokens }, i) => {
    const m = assertionRE.exec(line);
    if (!m) {
      source = { line, tokens, number: i + 1 };
      return;
    }
    if (!source) {
      console.error(`sample.p:${i + 1}: assertion before any source line`);
      failed++;
      return;
    }
    let from;
    let to;
    if (m[1]) {
      from = 0;
      to = m[1].length - 1;
    } else {
      from = line.indexOf('^');
      to = from + m[2].length;
    }
    const covered = source.tokens.filter((t) => t.startIndex < to && t.endIndex > from);
    const text = source.line.slice(from, to);
    if (covered.length === 0 || to > source.line.length) {
      console.error(`sample.p:${i + 1}: marker [${from},${to}) is outside line ${source.number}`);
      failed++;
      return;
    }
    for (const want of m[3].trim().split(/\s+/)) {
      checked++;
      const negate = want.startsWith('!');
      const scope = negate ? want.slice(1) : want;
      for (const t of covered) {
        if (hasScope(t.scopes, scope) === negate) {
          const tokText = source.line.slice(t.startIndex, t.endIndex);
          console.error(`sample.p:${i + 1}: ${JSON.stringify(text)} at line ${source.number}: token ${JSON.stringify(tokText)} ${negate ? 'has' : 'lacks'} ${scope}; scopes: ${t.scopes.join(' ')}`);
          failed++;
          break;
        }
      }
    }
  });
  return { failed, checked };
}

async function checkTokens() {
  const mods = findModules();
  if (!mods) {
    console.log('skip: no vscode-textmate and vscode-oniguruma found; set PLUTONIUM_TEXTMATE_MODULES to a node_modules directory holding them');
    return EXIT_SKIP;
  }
  const { vsctm, oniguruma, grammar } = await loadGrammar(mods);
  let failed = 0;
  for (const { where, source } of grammarRegexps()) {
    try {
      new oniguruma.OnigScanner([source]).dispose();
    } catch (err) {
      console.error(`${where}: oniguruma: ${err.message}`);
      failed++;
    }
  }
  const result = checkAssertions(tokenize(vsctm, grammar, fs.readFileSync(samplePath, 'utf8')));
  failed += result.failed;
  const files = repoSources();
  for (const file of files) {
    const depth = endDepth(vsctm, grammar, fs.readFileSync(file, 'utf8'));
    if (depth !== 1) {
      console.error(`${path.relative(repoRoot, file)}: a grammar region is still open at the end of the file (rule stack depth ${depth})`);
      failed++;
    }
  }
  if (failed > 0) {
    console.error(`${failed} failures (modules from ${mods.dir})`);
    return EXIT_FAIL;
  }
  console.log(`ok: ${result.checked} scope assertions hold, ${files.length} repository sources end with no open region (modules from ${mods.dir})`);
  return 0;
}

const repoRoot = path.join(root, '..', '..');

// repoSources returns the .p files under the repository's lib and examples
// directories.
function repoSources() {
  const out = [];
  const walk = (dir) => {
    if (!fs.existsSync(dir)) {
      return;
    }
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) {
        walk(p);
      } else if (e.name.endsWith('.p')) {
        out.push(p);
      }
    }
  };
  walk(path.join(repoRoot, 'lib'));
  walk(path.join(repoRoot, 'examples'));
  return out.sort();
}

// endDepth tokenizes text and returns the depth of the rule stack at its end:
// 1 when every region the grammar opened has been closed.
function endDepth(vsctm, grammar, text) {
  let state = vsctm.INITIAL;
  for (const line of text.split('\n')) {
    state = grammar.tokenizeLine(line, state).ruleStack;
  }
  return state.depth;
}

async function dump(file) {
  const mods = findModules();
  if (!mods) {
    console.log('skip: no vscode-textmate and vscode-oniguruma found');
    return EXIT_SKIP;
  }
  const { vsctm, grammar } = await loadGrammar(mods);
  tokenize(vsctm, grammar, fs.readFileSync(file, 'utf8')).forEach(({ line, tokens }, i) => {
    console.log(`${i + 1}: ${line}`);
    for (const t of tokens) {
      const scopes = t.scopes.slice(1).join(' ');
      console.log(`    ${JSON.stringify(line.slice(t.startIndex, t.endIndex))} ${scopes}`);
    }
  });
  return 0;
}

async function main() {
  switch (process.argv[2]) {
    case 'regexps':
      return checkRegexps();
    case 'tokens':
      return checkTokens();
    case 'dump':
      return dump(process.argv[3] || samplePath);
    default:
      console.error('usage: grammar-check.js regexps|tokens|dump [file]');
      return 2;
  }
}

main().then(
  (code) => process.exit(code),
  (err) => {
    console.error(err && err.stack ? err.stack : err);
    process.exit(EXIT_FAIL);
  });
