#!/usr/bin/env node
'use strict';

// Exercises the language server client outside VS Code: activates
// extension.js against stand-ins for the vscode and vscode-languageclient
// modules, and checks how it starts, stops and restarts the server.
//
//   node test/lsp-check.js

const assert = require('assert');
const fs = require('fs');
const Module = require('module');
const os = require('os');
const path = require('path');
const { createOutputChannel, isLogOutputChannel } = require('./fake-output');

// root holds fake pluto executables, which print the path they were run
// as and their arguments, so that a check does not depend on where (or
// whether) pluto is installed on this machine.
const root = fs.mkdtempSync(path.join(os.tmpdir(), 'pluto-lsp-check-'));
process.on('exit', () => fs.rmSync(root, { recursive: true, force: true }));

// fakePluto writes an executable script at root/rel and returns its path.
function fakePluto(rel, mode = 0o755) {
  const p = path.join(root, rel);
  fs.mkdirSync(path.dirname(p), { recursive: true });
  fs.writeFileSync(p, '#!/bin/sh\necho "$0 $*"\n', { mode });
  fs.chmodSync(p, mode);
  return p;
}

const optPluto = fakePluto('opt/pluto/bin/pluto');
const otherPluto = fakePluto('other/pluto');
const settings = { plutoPath: optPluto, useLanguageServer: true, target: 'x86_64-none', mode: 'kernel', tags: ['x'] };
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
  assert.ok(output.lines.includes(`info: Using pluto at ${optPluto}`));
  assert.ok(output.lines.includes(`info: Started ${optPluto} lsp`));
  assert.deepStrictEqual(c.clientOptions.documentSelector, [{ scheme: 'file', language: 'plutonium' }]);
  assert.deepStrictEqual(c.clientOptions.initializationOptions, { target: 'x86_64-none', mode: 'kernel', tags: ['x'] });
  assert.strictEqual(c.clientOptions.synchronize.configurationSection, 'plutonium');
  assert.strictEqual(c.clientOptions.synchronize.fileEvents.glob, '**/{*.p,pluto.mod}');
  assert.strictEqual(formatter, null, 'no stand-alone formatter while the server runs');
  assert.ok(commands.has('plutonium.restartLanguageServer'));

  // Explain error opens pluto explain's output beside the editor, from the
  // same pluto; one that does not exist is said so in the document.
  await commands.get('plutonium.explainError')('E0024');
  assert.strictEqual(shown.length, 1);
  assert.strictEqual(shown[0].uri.toString(), 'plutonium-explain:/E0024.txt');
  assert.strictEqual(shown[0].text, `${optPluto} explain E0024\n`);
  settings.plutoPath = path.join(root, 'no-such-pluto');
  await commands.get('plutonium.explainError')('E0024');
  assert.match(shown.pop().text, /^Cannot run pluto explain E0024: .*no-such-pluto \(plutonium.plutoPath\) was not found/);
  settings.plutoPath = optPluto;
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
  settings.plutoPath = otherPluto;
  await configListener({ affectsConfiguration: (s) => s === 'plutonium.plutoPath' });
  assert.strictEqual(clients.length, 3);
  assert.ok(clients[1].stopped);
  assert.strictEqual(clients[2].serverOptions.command, otherPluto);
  assert.ok(output.lines.includes(`info: Using pluto at ${otherPluto}`), 'the new path is logged');
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

  await checkSearch(lsp, log);

  console.log('ok: language server client checks pass');
}

// checkSearch checks how pluto is found, by the language server and by
// runPluto (the formatter and pluto explain), with fake executables in
// every place searched and an environment pointing at them.
async function checkSearch(lsp, log) {
  const { resolvePluto, runPluto } = require(path.join(__dirname, '..', 'src', 'pluto.js'));
  const saved = Object.fromEntries(['PATH', 'HOME', 'GOBIN', 'GOPATH'].map((k) => [k, process.env[k]]));
  const env = (vars) => {
    for (const [k, v] of Object.entries(vars)) {
      if (v === undefined) {
        delete process.env[k];
      } else {
        process.env[k] = v;
      }
    }
  };
  const dir = (rel) => path.join(root, rel);

  // Only executable regular files count: a directory named pluto and a
  // pluto that cannot be executed, early on PATH, are passed over.
  fs.mkdirSync(dir('path1/pluto'), { recursive: true });
  fakePluto('path2/pluto', 0o644);
  const onPath = fakePluto('path3/pluto');
  const settingPluto = fakePluto('setting/pluto');
  const homePluto = fakePluto('home/go/bin/pluto');
  const tildePluto = fakePluto('home/tools/pluto');
  const gobinPluto = fakePluto('gobin/pluto');
  const gopathPluto = fakePluto('gopath2/bin/pluto');
  fakePluto('path3/pluto-dev');
  fs.mkdirSync(dir('gopath1/bin'), { recursive: true });
  fs.mkdirSync(dir('emptyhome'), { recursive: true });
  const fullPath = [dir('path1'), dir('path2'), dir('path3')].join(path.delimiter);
  const goPath = [dir('gopath1'), dir('gopath2')].join(path.delimiter);
  env({ PATH: fullPath, HOME: dir('home'), GOBIN: dir('gobin'), GOPATH: goPath });

  // found returns the pluto the language server starts and checks that it
  // logged it, and that runPluto runs the same one.
  const found = async () => {
    const out = log();
    const before = clients.length;
    const server = new lsp.LanguageServer(out);
    await server.start();
    assert.strictEqual(clients.length, before + 1, `a client was started; output:\n${out.lines.join('\n')}`);
    const command = clients[clients.length - 1].serverOptions.command;
    assert.ok(out.lines.includes(`info: Using pluto at ${command}`), `the path is logged:\n${out.lines.join('\n')}`);
    const res = await runPluto(['fmt']);
    assert.strictEqual(res.stdout, `${command} fmt\n`, 'the formatter runs the same pluto');
    await server.stop();
    return command;
  };
  // notFound returns the message the language server shows when it finds
  // no pluto, and checks that runPluto reports the same.
  const notFound = async () => {
    const before = clients.length;
    const server = new lsp.LanguageServer(log());
    await server.start();
    assert.strictEqual(clients.length, before, 'no client is started');
    assert.strictEqual(server.running, false);
    assert.strictEqual(messages.length, 1, `one message: ${JSON.stringify(messages)}`);
    const msg = messages.pop()[1];
    await assert.rejects(runPluto(['fmt']), (err) => err.code === 'ENOENT' && err.notFound && msg.endsWith(err.message));
    return msg;
  };

  // 1. The setting wins over everything else, with ~ expanded; "pluto"
  // means search, and another bare name is looked up on PATH.
  settings.plutoPath = settingPluto;
  assert.strictEqual(await found(), settingPluto);
  settings.plutoPath = '~/tools/pluto';
  assert.strictEqual(await found(), tildePluto);
  settings.plutoPath = 'pluto-dev';
  assert.strictEqual(await found(), dir('path3/pluto-dev'));
  settings.plutoPath = 'pluto';
  assert.strictEqual(await found(), onPath);

  // A setting that names no pluto is reported by its path, with no
  // fallback to the others.
  settings.plutoPath = dir('setting/missing');
  assert.match(await notFound(), new RegExp(`cannot start the language server: ${dir('setting/missing')} \\(plutonium.plutoPath\\) was not found\\.`));
  settings.plutoPath = dir('path2/pluto');
  assert.match(await notFound(), /\(plutonium.plutoPath\) is not an executable file/);
  settings.plutoPath = 'no-such-pluto';
  assert.match(await notFound(), /no-such-pluto \(plutonium.plutoPath\) was not found on PATH/);

  // 2. Unset, PATH wins over the Go install locations, and the result is
  // an absolute path.
  delete settings.plutoPath;
  assert.strictEqual(await found(), onPath);
  env({ PATH: ['path1', 'path3'].map((d) => path.relative(process.cwd(), dir(d))).join(path.delimiter) });
  assert.strictEqual(await found(), onPath, 'a relative PATH entry gives an absolute path');

  // 3. $GOBIN, then each $GOPATH entry's bin, then ~/go/bin.
  env({ PATH: [dir('path1'), dir('path2')].join(path.delimiter) });
  assert.strictEqual(await found(), gobinPluto);
  env({ GOBIN: dir('gopath1/bin') });
  assert.strictEqual(await found(), gopathPluto);
  env({ GOBIN: undefined });
  assert.strictEqual(await found(), gopathPluto);
  env({ GOPATH: undefined });
  assert.strictEqual(await found(), homePluto, '~/go/bin/pluto, from a fake HOME');

  // 4. Nothing found: the message lists the places searched.
  fs.rmSync(gopathPluto);
  env({ HOME: dir('emptyhome'), GOBIN: dir('gobin2'), GOPATH: goPath });
  const searched = [dir('gobin2/pluto'), dir('gopath1/bin/pluto'), dir('gopath2/bin/pluto'), dir('emptyhome/go/bin/pluto')];
  const msg = await notFound();
  assert.ok(msg.endsWith(`pluto was not found on PATH or at ${searched.join(', ')}. Install pluto or set plutonium.plutoPath.`), msg);

  // The search is done again at each start: a pluto installed later is
  // found by a restart, and logged when it changes.
  const out = log();
  const server = new lsp.LanguageServer(out);
  fakePluto('emptyhome/go/bin/pluto');
  await server.start();
  await server.restart();
  assert.deepStrictEqual(out.lines.filter((l) => l.startsWith('info: Using')),
    [`info: Using pluto at ${dir('emptyhome/go/bin/pluto')}`, `info: Using pluto at ${dir('emptyhome/go/bin/pluto')}`]);
  fakePluto('gobin2/pluto');
  await server.restart();
  assert.strictEqual(clients[clients.length - 1].serverOptions.command, dir('gobin2/pluto'));
  await server.stop();
  assert.strictEqual(resolvePluto(), dir('gobin2/pluto'));
  const quiet = log();
  resolvePluto(undefined, quiet);
  resolvePluto(undefined, quiet);
  assert.deepStrictEqual(quiet.lines, [`info: Using pluto at ${dir('gobin2/pluto')}`], 'logged once while it stays the same');
  assert.strictEqual(messages.length, 0);

  env(saved);
}

main().catch((err) => {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
