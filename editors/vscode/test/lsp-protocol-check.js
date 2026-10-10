#!/usr/bin/env node
'use strict';

// Talks to a real `pluto lsp` through vscode-jsonrpc, the JSON-RPC library
// under vscode-languageclient, to check that the two agree on the wire:
// the handshake, diagnostics, hover, definition, formatting and shutdown.
// It prints how long diagnostics took after an edit.
//
//   node test/lsp-protocol-check.js /path/to/pluto
//
// It needs the extension's npm dependencies (npm ci) and exits with status
// 3 when they are missing.

const assert = require('assert');
const childProcess = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { pathToFileURL } = require('url');

let rpc;
try {
  rpc = require('vscode-jsonrpc/node');
} catch {
  console.error('vscode-jsonrpc is not installed; run npm ci');
  process.exit(3);
}

const pluto = process.argv[2];
if (!pluto) {
  console.error('usage: lsp-protocol-check.js /path/to/pluto');
  process.exit(2);
}

const main = `package main

import "pluto/strings"

func main() {
	ok := strings.HasPrefix("abc", "a")
	_ = ok
}
`;

async function run() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'pluto-lsp-check-'));
  try {
    fs.writeFileSync(path.join(dir, 'pluto.mod'), 'module example.com/check\n');
    const file = path.join(dir, 'main.p');
    fs.writeFileSync(file, main);
    const uri = pathToFileURL(file).toString();

    // The embedded standard library is extracted under the cache directory;
    // keep that inside the test's directory.
    const env = { ...process.env, XDG_CACHE_HOME: path.join(dir, 'cache') };
    const server = childProcess.spawn(pluto, ['lsp'], { stdio: ['pipe', 'pipe', 'inherit'], env });
    const exited = new Promise((resolve) => server.on('exit', (code) => resolve(code)));
    const conn = rpc.createMessageConnection(
      new rpc.StreamMessageReader(server.stdout), new rpc.StreamMessageWriter(server.stdin));
    const waiters = [];
    conn.onNotification('textDocument/publishDiagnostics', (p) => {
      for (const w of waiters.splice(0)) {
        w(p);
      }
    });
    const nextDiagnostics = () => new Promise((resolve) => waiters.push(resolve));
    conn.listen();

    const init = await conn.sendRequest('initialize', {
      processId: process.pid,
      rootUri: pathToFileURL(dir).toString(),
      capabilities: {},
      workspaceFolders: [{ uri: pathToFileURL(dir).toString(), name: 'check' }],
    });
    assert.strictEqual(init.capabilities.textDocumentSync.change, 1);
    assert.strictEqual(init.capabilities.hoverProvider, true);
    conn.sendNotification('initialized', {});

    let diags = nextDiagnostics();
    conn.sendNotification('textDocument/didOpen', { textDocument: { uri, languageId: 'plutonium', version: 1, text: main } });
    assert.deepStrictEqual((await diags).diagnostics, []);

    // An edit that breaks the file, timed until its diagnostics arrive.
    diags = nextDiagnostics();
    const start = process.hrtime.bigint();
    conn.sendNotification('textDocument/didChange', {
      textDocument: { uri, version: 2 },
      contentChanges: [{ text: main.replace('_ = ok', '_ = missing') }],
    });
    const broken = await diags;
    const ms = Number(process.hrtime.bigint() - start) / 1e6;
    assert.ok(broken.diagnostics.some((d) => d.message.includes('missing')), JSON.stringify(broken));
    console.log(`diagnostics ${ms.toFixed(1)} ms after the edit (including the debounce)`);

    // The error has a code, and a code action explains it.
    const missing = broken.diagnostics.find((d) => d.message.includes('missing'));
    assert.match(missing.code, /^E\d{4}$/);
    const actions = await conn.sendRequest('textDocument/codeAction', {
      textDocument: { uri }, range: missing.range, context: { diagnostics: [missing] },
    });
    assert.strictEqual(actions.length, 1);
    assert.deepStrictEqual(actions[0].command.arguments, [missing.code]);

    const hover = await conn.sendRequest('textDocument/hover', { textDocument: { uri }, position: { line: 5, character: 16 } });
    assert.match(hover.contents.value, /func strings\.HasPrefix/);

    const defs = await conn.sendRequest('textDocument/definition', { textDocument: { uri }, position: { line: 5, character: 16 } });
    assert.strictEqual(defs.length, 1);
    assert.match(defs[0].uri, /pluto\/strings\/strings\.p$/);

    const edits = await conn.sendRequest('textDocument/formatting', { textDocument: { uri }, options: { tabSize: 4, insertSpaces: false } });
    assert.deepStrictEqual(edits, []);

    await conn.sendRequest('shutdown');
    conn.sendNotification('exit');
    assert.strictEqual(await exited, 0);
    conn.dispose();
    console.log('ok: pluto lsp speaks to vscode-jsonrpc');
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

run().catch((err) => {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
