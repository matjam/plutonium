'use strict';

// The Plutonium language server: runs `pluto lsp` and connects it to VS Code
// with vscode-languageclient, which maps the server's diagnostics, hover,
// definitions, references, symbols, formatting, completion, signature help
// and rename onto the editor's providers.
//
// If the server exits unexpectedly it is restarted after a delay that
// doubles with each crash (RESTART_BASE_MS up to RESTART_MAX_MS); after
// MAX_CRASHES crashes within CRASH_WINDOW_MS it is left stopped, with a
// message offering to restart it.

const vscode = require('vscode');
const { plutoPath } = require('./pluto');

const RESTART_BASE_MS = 500;
const RESTART_MAX_MS = 30000;
const MAX_CRASHES = 5;
const CRASH_WINDOW_MS = 3 * 60 * 1000;

// wanted reports whether the language server should run: the setting is on
// and the workspace is trusted, since pluto runs on the workspace's files.
function wanted() {
  const config = vscode.workspace.getConfiguration('plutonium');
  return config.get('useLanguageServer', true) && vscode.workspace.isTrusted;
}

// serverSettings returns the settings the server reads at startup; later
// changes reach it through workspace/didChangeConfiguration.
function serverSettings() {
  const config = vscode.workspace.getConfiguration('plutonium');
  return {
    target: config.get('target', 'x86_64-linux'),
    mode: config.get('mode', ''),
    tags: config.get('tags', []),
  };
}

class LanguageServer {
  constructor(output, timers = { setTimeout, clearTimeout }) {
    this.output = output;
    this.timers = timers;
    this.client = null;
    this.crashes = []; // times of recent crashes
    this.stopping = false;
  }

  get running() {
    return this.client !== null;
  }

  async start() {
    if (this.client) {
      return;
    }
    // Loaded here so that the extension works without the module when the
    // language server is turned off.
    const lc = require('vscode-languageclient/node');
    this.lc = lc;
    const command = plutoPath();
    const serverOptions = { command, args: ['lsp'], transport: lc.TransportKind.stdio };
    const clientOptions = {
      documentSelector: [{ scheme: 'file', language: 'plutonium' }],
      synchronize: {
        configurationSection: 'plutonium',
        fileEvents: vscode.workspace.createFileSystemWatcher('**/{*.p,pluto.mod}'),
      },
      initializationOptions: serverSettings(),
      outputChannel: this.output,
      errorHandler: {
        error: () => ({ action: lc.ErrorAction.Continue }),
        closed: () => this.closed(),
      },
    };
    this.stopping = false;
    this.client = new lc.LanguageClient('plutonium', 'Plutonium Language Server', serverOptions, clientOptions);
    try {
      await this.client.start();
      this.output.appendLine(`Started ${command} lsp`);
    } catch (err) {
      this.client = null;
      const msg = err && err.code === 'ENOENT'
        ? `${command} was not found. Install pluto or set plutonium.plutoPath.`
        : `${err && err.message ? err.message : err}`;
      this.output.appendLine(`Cannot start the language server: ${msg}`);
      vscode.window.showErrorMessage(`Plutonium: cannot start the language server: ${msg}`, 'Show Output')
        .then((choice) => choice && this.output.show());
    }
  }

  async stop() {
    const client = this.client;
    this.client = null;
    this.stopping = true;
    if (client) {
      try {
        await client.stop();
      } catch (err) {
        this.output.appendLine(`Stopping the language server: ${err && err.message ? err.message : err}`);
      }
    }
  }

  async restart() {
    await this.stop();
    this.crashes = [];
    await this.start();
  }

  // closed decides what happens when the server's connection closes
  // without the client stopping it: the server crashed or exited.
  async closed() {
    const lc = this.lc;
    if (this.stopping) {
      return { action: lc.CloseAction.DoNotRestart, handled: true };
    }
    const now = Date.now();
    this.crashes = this.crashes.filter((t) => now - t < CRASH_WINDOW_MS);
    this.crashes.push(now);
    if (this.crashes.length > MAX_CRASHES) {
      this.client = null;
      const msg = `The Plutonium language server crashed ${this.crashes.length} times in the last few minutes and was not restarted.`;
      this.output.appendLine(msg);
      vscode.window.showErrorMessage(msg, 'Restart', 'Show Output').then((choice) => {
        if (choice === 'Restart') {
          this.restart();
        } else if (choice === 'Show Output') {
          this.output.show();
        }
      });
      return { action: lc.CloseAction.DoNotRestart, handled: true };
    }
    const delay = Math.min(RESTART_BASE_MS * 2 ** (this.crashes.length - 1), RESTART_MAX_MS);
    const msg = `The Plutonium language server stopped unexpectedly; restarting in ${(delay / 1000).toFixed(1)}s.`;
    this.output.appendLine(msg);
    vscode.window.showWarningMessage(msg, 'Show Output').then((choice) => choice && this.output.show());
    await new Promise((resolve) => this.timers.setTimeout(resolve, delay));
    if (this.stopping) {
      return { action: lc.CloseAction.DoNotRestart, handled: true };
    }
    return { action: lc.CloseAction.Restart, handled: true };
  }
}

// register adds the language server and its restart command to context and
// returns it; extension.js starts and stops it as settings change.
function register(context, output) {
  const server = new LanguageServer(output);
  context.subscriptions.push(
    vscode.commands.registerCommand('plutonium.restartLanguageServer', () => {
      if (!wanted()) {
        vscode.window.showInformationMessage('Plutonium: the language server is turned off (plutonium.useLanguageServer).');
        return undefined;
      }
      return server.restart();
    }),
    { dispose: () => server.stop() },
  );
  return server;
}

module.exports = { register, wanted, LanguageServer, RESTART_BASE_MS, RESTART_MAX_MS, MAX_CRASHES };
