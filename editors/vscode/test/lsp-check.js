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
const { createOutputChannel, isLogOutputChannel } = require('./fake-output');

const settings = { plutoPath: '/opt/pluto/bin/pluto', useLanguageServer: true, target: 'x86_64-none', mode: 'kernel', tags: ['x'] };
const disposable = { dispose() {} };
const commands = new Map();
const messages = [];
const contentProviders = new Map();
const shown = [];
const channels = [];
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
    registerTextDocumentContentProvider: (scheme, provider) => {
      contentProviders.set(scheme, provider);
      return disposable;
    },
    openTextDocument: async (uri) => ({ uri, text: await contentProviders.get(uri.scheme).provideTextDocumentContent(uri) }),
  },
  Uri: {
    parse: (s) => {
      const [scheme, rest] = s.split(/:(.*)/s);
      return { scheme, path: rest, toString: () => s };
    },
  },
  ViewColumn: { Beside: -2 },
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
    createOutputChannel: (name, options) => {
      const channel = createOutputChannel(name, options);
      channels.push(channel);
      return channel;
    },
    showErrorMessage: (m, ...buttons) => {
      messages.push(['error', m, buttons]);
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
    showTextDocument: async (doc) => {
      shown.push(doc);
    },
  },
};

// A stand-in LanguageClient that records what it was given. Like
// vscode-languageclient 10, it logs to the output channel with the
// LogOutputChannel methods, and adds --stdio to the command's arguments
// when the transport is TransportKind.stdio.
const clients = [];
let failStart = null;
class LanguageClient {
  constructor(id, name, serverOptions, clientOptions) {
    Object.assign(this, { id, name, serverOptions, clientOptions, started: false, stopped: false });
    clients.push(this);
    const channel = clientOptions.outputChannel;
    if (!isLogOutputChannel(channel)) {
      throw new TypeError('this.outputChannel.error is not a function');
    }
    this.traceLogLevel = channel.logLevel;
    this.spawnArgs = [...(serverOptions.args || [])];
    if (serverOptions.transport === languageclient.TransportKind.stdio) {
      this.spawnArgs.push('--stdio');
    }
  }
  start() {
    if (failStart) {
      this.clientOptions.outputChannel.error(`${this.name} client: couldn't create connection to server.`);
      return Promise.reject(failStart);
    }
    this.clientOptions.outputChannel.onDidChangeLogLevel(() => {});
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
  TransportKind: { stdio: 0, ipc: 1, pipe: 2, socket: 3 },
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
  assert.deepStrictEqual(c.spawnArgs, ['lsp'], 'pluto lsp is run with no other arguments, such as --stdio');
  assert.strictEqual(channels.length, 1);
  const output = channels[0];
  assert.ok(isLogOutputChannel(output), 'the output is a LogOutputChannel');
  assert.strictEqual(c.clientOptions.outputChannel, output);
  assert.ok(output.lines.includes('info: Started /opt/pluto/bin/pluto lsp'));
  assert.deepStrictEqual(c.clientOptions.documentSelector, [{ scheme: 'file', language: 'plutonium' }]);
  assert.deepStrictEqual(c.clientOptions.initializationOptions, { target: 'x86_64-none', mode: 'kernel', tags: ['x'] });
  assert.strictEqual(c.clientOptions.synchronize.configurationSection, 'plutonium');
  assert.strictEqual(c.clientOptions.synchronize.fileEvents.glob, '**/{*.p,pluto.mod}');
  assert.strictEqual(formatter, null, 'no stand-alone formatter while the server runs');
  assert.ok(commands.has('plutonium.restartLanguageServer'));

  // Explain error opens pluto explain's output beside the editor; here pluto
  // does not exist, which the document says.
  await commands.get('plutonium.explainError')('E0024');
  assert.strictEqual(shown.length, 1);
  assert.strictEqual(shown[0].uri.toString(), 'plutonium-explain:/E0024.txt');
  assert.match(shown[0].text, /Cannot run pluto explain E0024/);
  await commands.get('plutonium.explainError')('not a code');
  assert.strictEqual(shown.length, 1, 'only error codes are explained');

  // Errors on the connection do not stop the server, and are logged
  // without a notification.
  assert.deepStrictEqual(c.clientOptions.errorHandler.error(new Error('x')),
    { action: languageclient.ErrorAction.Continue, handled: true });

  // The server's log reaches the output at its own level.
  const logged = createOutputChannel('log', { log: true });
  lsp.logServerLine(logged, 'time=2026-10-10T00:35:22.601-07:00 level=INFO msg=initialized mode=hosted');
  lsp.logServerLine(logged, 'time=2026-10-10T00:35:22.601-07:00 level=WARN msg="bad notification parameters"');
  lsp.logServerLine(logged, 'time=2026-10-10T00:35:22.601-07:00 level=ERROR msg="handler panicked"');
  lsp.logServerLine(logged, 'time=2026-10-10T00:35:22.601-07:00 level=DEBUG msg=request');
  lsp.logServerLine(logged, 'panic: runtime error');
  assert.deepStrictEqual(logged.lines, [
    'info: msg=initialized mode=hosted',
    'warn: msg="bad notification parameters"',
    'error: msg="handler panicked"',
    'debug: msg=request',
    'error: panic: runtime error',
  ]);
  assert.strictEqual(typeof c.clientOptions.stdioOptions.stdout, 'function');
  assert.strictEqual(typeof c.clientOptions.stdioOptions.stderr, 'function');

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

  // A plain OutputChannel cannot be handed to vscode-languageclient.
  await extension.deactivate();
  assert.throws(() => new LanguageClient('x', 'x', { command: 'pluto' }, { outputChannel: createOutputChannel('plain') }),
    /outputChannel.error is not a function/);

  // A server that cannot start is reported once, and the closed
  // connection of a client that never started is not restarted.
  // vscode-languageclient reports a command it cannot run as a string.
  const log = () => createOutputChannel('Plutonium', { log: true });
  failStart = 'Launching server using command pluto failed. Error: spawn pluto ENOENT';
  const failingOutput = log();
  const failing = new lsp.LanguageServer(failingOutput);
  await failing.start();
  assert.strictEqual(failing.running, false);
  assert.strictEqual(messages.length, 1);
  assert.match(messages.pop()[1], /cannot start the language server: .* was not found/);
  assert.ok(failingOutput.lines.some((l) => /^error: Cannot start the language server: .* was not found/.test(l)));
  const res0 = await clients[clients.length - 1].clientOptions.errorHandler.closed();
  assert.strictEqual(res0.action, languageclient.CloseAction.DoNotRestart);
  assert.ok(res0.handled);
  assert.strictEqual(messages.length, 0, 'no further message');
  failStart = new Error('Server initialization failed.');
  await failing.start();
  assert.deepStrictEqual(messages.pop().slice(1), ['Plutonium: cannot start the language server: Server initialization failed.', ['Show Output']]);
  failStart = null;

  // Crashes restart the server after doubling delays, logged to the output
  // without a notification, until too many: then one message says so.
  const delays = [];
  const crashOutput = log();
  const server = new lsp.LanguageServer(crashOutput, {
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
  }
  assert.strictEqual(messages.length, 0, 'restarts are not notified');
  assert.deepStrictEqual(delays, [500, 1000, 2000, 4000, 8000]);
  assert.deepStrictEqual(crashOutput.lines.filter((l) => l.startsWith('warn: ')), [
    'warn: The language server stopped unexpectedly; restarting in 0.5s.',
    'warn: The language server stopped unexpectedly; restarting in 1.0s.',
    'warn: The language server stopped unexpectedly; restarting in 2.0s.',
    'warn: The language server stopped unexpectedly; restarting in 4.0s.',
    'warn: The language server stopped unexpectedly; restarting in 8.0s.',
  ]);
  const res = await closed();
  assert.strictEqual(res.action, languageclient.CloseAction.DoNotRestart);
  assert.ok(res.handled);
  assert.match(res.message, /crashed 6 times/);
  assert.strictEqual(messages.length, 1, 'giving up is notified once');
  const [kind, text, buttons] = messages.pop();
  assert.strictEqual(kind, 'error');
  assert.match(text, /crashed 6 times .* not restarted/);
  assert.deepStrictEqual(buttons, ['Show Output', 'Restart']);
  assert.strictEqual(server.running, false);
  assert.strictEqual((await closed()).action, languageclient.CloseAction.DoNotRestart, 'a client given up on stays stopped');
  assert.strictEqual(messages.length, 0);

  // Restarting by hand starts afresh; a replaced client is not restarted.
  await server.restart();
  const replaced = clients[clients.length - 1];
  await server.restart();
  assert.strictEqual((await replaced.clientOptions.errorHandler.closed()).action, languageclient.CloseAction.DoNotRestart);
  assert.strictEqual((await closed()).action, languageclient.CloseAction.Restart);

  // A connection closed by stopping is not a crash, even while the restart
  // waits.
  await server.stop();
  assert.strictEqual((await closed()).action, languageclient.CloseAction.DoNotRestart);
  let wake = null;
  const waiting = new lsp.LanguageServer(log(), { setTimeout: (fn) => { wake = fn; }, clearTimeout() {} });
  await waiting.start();
  const pending = closed();
  await waiting.stop();
  wake();
  assert.strictEqual((await pending).action, languageclient.CloseAction.DoNotRestart);
  assert.strictEqual(messages.length, 0);

  console.log('ok: language server client checks pass');
}

main().catch((err) => {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
