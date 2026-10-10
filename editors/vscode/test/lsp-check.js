#!/usr/bin/env node
'use strict';

// Exercises the language server client outside VS Code: activates
// extension.js against stand-ins for the vscode and vscode-languageclient
// modules, and checks how it starts, stops and restarts the server.
//
//   node test/lsp-check.js

const assert = require('assert');
const Module = require('module');
const path = require('path');

const settings = { plutoPath: '/opt/pluto/bin/pluto', useLanguageServer: true, target: 'x86_64-none', mode: 'kernel', tags: ['x'] };
const disposable = { dispose() {} };
const commands = new Map();
const messages = [];
let formatter = null;
let configListener = null;
let trustListener = null;

const vscode = {
  workspace: {
    isTrusted: true,
    getConfiguration: () => ({ get: (key, dflt) => (key in settings ? settings[key] : dflt) }),
    onDidChangeTextDocument: () => disposable,
    onDidCloseTextDocument: () => disposable,
    onDidChangeConfiguration: (fn) => {
      configListener = fn;
      return disposable;
    },
    onDidGrantWorkspaceTrust: (fn) => {
      trustListener = fn;
      return disposable;
    },
    createFileSystemWatcher: (glob) => ({ glob, dispose() {} }),
  },
  languages: {
    createDiagnosticCollection: () => ({ set() {}, delete() {}, clear() {}, dispose() {} }),
    registerDocumentFormattingEditProvider: () => {
      const reg = { disposed: false, dispose() { this.disposed = true; formatter = null; } };
      formatter = reg;
      return reg;
    },
  },
  commands: {
    registerCommand: (name, fn) => {
      commands.set(name, fn);
      return disposable;
    },
  },
  window: {
    createOutputChannel: () => ({ appendLine() {}, show() {}, dispose() {} }),
    showErrorMessage: (m) => {
      messages.push(['error', m]);
      return Promise.resolve(undefined);
    },
    showWarningMessage: (m) => {
      messages.push(['warning', m]);
      return Promise.resolve(undefined);
    },
    showInformationMessage: (m) => {
      messages.push(['info', m]);
      return Promise.resolve(undefined);
    },
  },
};

// A stand-in LanguageClient that records what it was given.
const clients = [];
let failStart = null;
class LanguageClient {
  constructor(id, name, serverOptions, clientOptions) {
    Object.assign(this, { id, name, serverOptions, clientOptions, started: false, stopped: false });
    clients.push(this);
  }
  start() {
    if (failStart) {
      return Promise.reject(failStart);
    }
    this.started = true;
    return Promise.resolve();
  }
  stop() {
    this.stopped = true;
    return Promise.resolve();
  }
}
const languageclient = {
  LanguageClient,
  TransportKind: { stdio: 0 },
  ErrorAction: { Continue: 1, Shutdown: 2 },
  CloseAction: { DoNotRestart: 1, Restart: 2 },
};

const stubs = { vscode, 'vscode-languageclient/node': languageclient };
const resolve = Module._resolveFilename;
Module._resolveFilename = function (request, ...rest) {
  return request in stubs ? request : resolve.call(this, request, ...rest);
};
for (const [name, exports] of Object.entries(stubs)) {
  require.cache[name] = { id: name, filename: name, loaded: true, exports };
}

async function main() {
  const extension = require(path.join(__dirname, '..', 'extension.js'));
  const lsp = require(path.join(__dirname, '..', 'src', 'lsp.js'));
  const context = { subscriptions: [] };
  await extension.activate(context);

  // The server runs `pluto lsp` with the settings, and replaces the
  // stand-alone formatter.
  assert.strictEqual(clients.length, 1);
  const c = clients[0];
  assert.ok(c.started);
  assert.strictEqual(c.serverOptions.command, settings.plutoPath);
  assert.deepStrictEqual(c.serverOptions.args, ['lsp']);
  assert.deepStrictEqual(c.clientOptions.documentSelector, [{ scheme: 'file', language: 'plutonium' }]);
  assert.deepStrictEqual(c.clientOptions.initializationOptions, { target: 'x86_64-none', mode: 'kernel', tags: ['x'] });
  assert.strictEqual(c.clientOptions.synchronize.configurationSection, 'plutonium');
  assert.strictEqual(c.clientOptions.synchronize.fileEvents.glob, '**/{*.p,pluto.mod}');
  assert.strictEqual(formatter, null, 'no stand-alone formatter while the server runs');
  assert.ok(commands.has('plutonium.restartLanguageServer'));

  // Errors on the connection do not stop the server.
  assert.deepStrictEqual(c.clientOptions.errorHandler.error(new Error('x')), { action: languageclient.ErrorAction.Continue });

  // Turning the server off stops it and brings the formatter back.
  settings.useLanguageServer = false;
  await configListener({ affectsConfiguration: (s) => s === 'plutonium.useLanguageServer' });
  assert.ok(c.stopped);
  assert.ok(formatter, 'the stand-alone formatter is registered');
  await commands.get('plutonium.restartLanguageServer')();
  assert.strictEqual(clients.length, 1, 'restart does nothing while the server is off');
  assert.strictEqual(messages.pop()[0], 'info');

  // Turning it on again starts a new client; changing plutoPath restarts it.
  settings.useLanguageServer = true;
  await configListener({ affectsConfiguration: (s) => s === 'plutonium.useLanguageServer' });
  assert.strictEqual(clients.length, 2);
  assert.strictEqual(formatter, null);
  settings.plutoPath = '/other/pluto';
  await configListener({ affectsConfiguration: (s) => s === 'plutonium.plutoPath' });
  assert.strictEqual(clients.length, 3);
  assert.ok(clients[1].stopped);
  assert.strictEqual(clients[2].serverOptions.command, '/other/pluto');
  await configListener({ affectsConfiguration: () => false });
  assert.strictEqual(clients.length, 3, 'unrelated settings change nothing');
  await commands.get('plutonium.restartLanguageServer')();
  assert.strictEqual(clients.length, 4);

  // An untrusted workspace runs no server until it is trusted.
  vscode.workspace.isTrusted = false;
  await configListener({ affectsConfiguration: (s) => s === 'plutonium.useLanguageServer' });
  assert.ok(clients[3].stopped);
  vscode.workspace.isTrusted = true;
  await trustListener();
  assert.strictEqual(clients.length, 5);

  // A server that cannot start is reported.
  await extension.deactivate();
  failStart = Object.assign(new Error('spawn pluto ENOENT'), { code: 'ENOENT' });
  const failing = new lsp.LanguageServer({ appendLine() {}, show() {} });
  await failing.start();
  assert.strictEqual(failing.running, false);
  assert.match(messages.pop()[1], /cannot start the language server: .* was not found/);
  failStart = null;

  // Crashes restart the server after doubling delays, until too many.
  const delays = [];
  const server = new lsp.LanguageServer({ appendLine() {}, show() {} }, {
    setTimeout: (fn, ms) => {
      delays.push(ms);
      fn();
    },
    clearTimeout() {},
  });
  await server.start();
  const closed = () => clients[clients.length - 1].clientOptions.errorHandler.closed();
  for (let i = 0; i < lsp.MAX_CRASHES; i++) {
    const res = await closed();
    assert.strictEqual(res.action, languageclient.CloseAction.Restart);
    assert.ok(res.handled);
    assert.strictEqual(messages.pop()[0], 'warning');
  }
  assert.deepStrictEqual(delays, [500, 1000, 2000, 4000, 8000]);
  const res = await closed();
  assert.strictEqual(res.action, languageclient.CloseAction.DoNotRestart);
  assert.strictEqual(messages.pop()[0], 'error');
  assert.strictEqual(server.running, false);

  // A connection closed by stopping is not a crash.
  await server.start();
  await server.stop();
  assert.strictEqual((await closed()).action, languageclient.CloseAction.DoNotRestart);

  console.log('ok: language server client checks pass');
}

main().catch((err) => {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
