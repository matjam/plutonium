#!/usr/bin/env node
'use strict';

// Starts a real `pluto lsp` through the extension's LanguageServer and the
// installed vscode-languageclient, with a stand-in for the vscode module,
// to check that the extension gives the client options it accepts: a
// LogOutputChannel, and a command line pluto lsp runs.
//
//   node test/lsp-client-check.js /path/to/pluto
//
// It needs the extension's npm dependencies (npm ci) and exits with status
// 3 when they are missing.

const assert = require('assert');
const fs = require('fs');
const Module = require('module');
const os = require('os');
const path = require('path');
const { LogLevel, createOutputChannel } = require('./fake-output');

const pluto = process.argv[2];
if (!pluto) {
  console.error('usage: lsp-client-check.js /path/to/pluto');
  process.exit(2);
}
try {
  require.resolve('vscode-languageclient/node');
} catch {
  console.error('vscode-languageclient is not installed; run npm ci');
  process.exit(3);
}

const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'pluto-lsp-client-check-'));
fs.writeFileSync(path.join(dir, 'pluto.mod'), 'module example.com/check\n');
// The embedded standard library is extracted under the cache directory;
// keep that inside the check's directory. The server inherits it.
process.env.XDG_CACHE_HOME = path.join(dir, 'cache');

const settings = { plutoPath: pluto };
const messages = [];
const folderUri = { scheme: 'file', fsPath: dir, path: dir, toString: () => `file://${dir}` };

// stub returns a function standing in for any part of the vscode API the
// check does not model: calling or constructing it, or reading any property
// of it, gives another stub, and disposing it does nothing.
function stub() {
  return new Proxy(function () {}, {
    get(_target, prop) {
      if (prop === 'then') {
        return undefined; // not a promise
      }
      if (prop === Symbol.toPrimitive) {
        return () => 0;
      }
      return prop === 'dispose' ? () => {} : stub();
    },
    apply: () => stub(),
    construct: () => stub(),
  });
}

class EventEmitter {
  constructor() {
    this.listeners = [];
    this.event = (fn) => {
      this.listeners.push(fn);
      return { dispose() {} };
    };
  }
  fire(e) {
    this.listeners.forEach((fn) => fn(e));
  }
  dispose() {}
}

class Disposable {
  constructor(fn) {
    this.fn = fn;
  }
  static from(...all) {
    return new Disposable(() => all.forEach((d) => d.dispose()));
  }
  dispose() {
    if (this.fn) {
      this.fn();
    }
  }
}

const notify = (kind) => (message, ...buttons) => {
  messages.push([kind, message, buttons]);
  return Promise.resolve(undefined);
};

const modelled = {
  version: '1.140.0',
  EventEmitter,
  Disposable,
  LogLevel,
  workspace: {
    isTrusted: true,
    workspaceFolders: [{ uri: folderUri, name: 'check', index: 0 }],
    textDocuments: [],
    notebookDocuments: [],
    getConfiguration: () => ({ get: (key, dflt) => (key in settings ? settings[key] : dflt) }),
  },
  window: {
    createOutputChannel,
    showErrorMessage: notify('error'),
    showWarningMessage: notify('warning'),
    showInformationMessage: notify('info'),
    visibleTextEditors: [],
  },
};
const fallback = stub();
function withFallback(object) {
  return new Proxy(object, {
    get(target, prop) {
      if (prop in target) {
        const v = target[prop];
        return v && typeof v === 'object' && !Array.isArray(v) && Object.getPrototypeOf(v) === Object.prototype ? withFallback(v) : v;
      }
      return prop === 'then' ? undefined : fallback[prop];
    },
  });
}
const vscode = withFallback(modelled);

const resolve = Module._resolveFilename;
Module._resolveFilename = function (request, ...rest) {
  return request === 'vscode' ? request : resolve.call(this, request, ...rest);
};
require.cache.vscode = { id: 'vscode', filename: 'vscode', loaded: true, exports: vscode };

async function main() {
  const lsp = require(path.join(__dirname, '..', 'src', 'lsp.js'));

  // The server starts and logs to the output at the right levels.
  const output = createOutputChannel('Plutonium', { log: true });
  const server = new lsp.LanguageServer(output);
  await server.start();
  assert.ok(server.running, `the server runs; output:\n${output.lines.join('\n')}`);
  assert.deepStrictEqual(messages, [], 'no notifications');
  const proc = server.client.serverProcess;
  assert.ok(proc && proc.pid, 'pluto lsp is running');
  assert.deepStrictEqual(proc.spawnargs.slice(1), ['lsp']);
  // The server logs "initialized" before it answers initialize.
  assert.ok(output.lines.some((l) => l.startsWith('info: msg=initialized ')), `pluto's log is info:\n${output.lines.join('\n')}`);
  assert.ok(output.lines.includes(`info: Started ${pluto} lsp`));
  assert.deepStrictEqual(output.lines.filter((l) => l.startsWith('error: ')), []);

  // A server that dies is restarted after the first delay, with a warning
  // in the output and no notification.
  // The client sends the settings just after it starts; let that finish,
  // so that it is not sent to the server being killed.
  const client = server.client;
  await client.sendRequest('workspace/symbol', { query: 'x' });
  await new Promise((done) => setTimeout(done, 200));
  proc.kill('SIGKILL');
  const deadline = Date.now() + 10000;
  while (!(client.serverProcess && client.serverProcess !== proc && client.isRunning()) && Date.now() < deadline) {
    await new Promise((done) => setTimeout(done, 50));
  }
  assert.ok(client.isRunning(), `the server was restarted; output:\n${output.lines.join('\n')}`);
  assert.strictEqual(server.client, client);
  assert.ok(output.lines.includes('warn: The language server stopped unexpectedly; restarting in 0.5s.'));
  assert.deepStrictEqual(messages, [], 'no notifications');

  // Stopping it shuts the server down.
  const restarted = client.serverProcess;
  const exited = new Promise((done) => restarted.on('exit', (code) => done(code)));
  await server.stop();
  assert.strictEqual(await exited, 0, 'pluto lsp exits with status 0 after shutdown and exit');
  assert.strictEqual(server.running, false);

  // A pluto that does not exist is reported as missing.
  settings.plutoPath = path.join(dir, 'no-such-pluto');
  const missing = new lsp.LanguageServer(createOutputChannel('Plutonium', { log: true }));
  await missing.start();
  assert.strictEqual(missing.running, false);
  const ours = messages.filter(([, m]) => m.startsWith('Plutonium:'));
  assert.strictEqual(ours.length, 1, `one message from the extension: ${JSON.stringify(messages)}`);
  assert.match(ours[0][1], /cannot start the language server: .*no-such-pluto was not found/);

  console.log('ok: vscode-languageclient runs pluto lsp');
}

main().then(() => {
  fs.rmSync(dir, { recursive: true, force: true });
  process.exit(0);
}, (err) => {
  console.error(err && err.stack ? err.stack : err);
  fs.rmSync(dir, { recursive: true, force: true });
  process.exit(1);
});
