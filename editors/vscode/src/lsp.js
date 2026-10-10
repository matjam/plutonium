'use strict';

// The Plutonium language server: runs `pluto lsp` and connects it to VS Code
// with vscode-languageclient, which maps the server's diagnostics, hover,
// definitions, references, symbols, formatting, completion, signature help
// and rename onto the editor's providers. pluto is found by resolvePluto
// (src/pluto.js) at each start, and its path is logged to the output.
//
// If the server exits unexpectedly after it started, it is restarted after
// a delay that doubles with each crash (RESTART_BASE_MS up to
// RESTART_MAX_MS), which is logged to the output; after MAX_CRASHES crashes
// within CRASH_WINDOW_MS it is left stopped, with one message offering to
// restart it or show the output. A server that fails to start is reported
// once and not restarted.
//
// The output channel must be a LogOutputChannel (createOutputChannel with
// { log: true }): vscode-languageclient 10 logs through its info, warn and
// error methods.

const readline = require('readline');
const vscode = require('vscode');
const { resolvePluto, runPluto } = require('./pluto');

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

// SLOG_LINE matches a line of pluto lsp's log (log/slog's text format):
// the time and level, which the output channel shows itself, and the rest.
const SLOG_LINE = /^time=\S+ level=(DEBUG|INFO|WARN|ERROR)([+-]\d+)? (.*)$/;

// logServerLine writes a line the server wrote to standard error to output
// at the line's own level; vscode-languageclient would log every one of
// them as an error.
function logServerLine(output, line) {
  const m = SLOG_LINE.exec(line);
  if (!m) {
    output.error(line);
    return;
  }
  const log = { DEBUG: output.debug, INFO: output.info, WARN: output.warn, ERROR: output.error }[m[1]];
  log.call(output, m[3]);
}

// pipeLines calls fn with each line of the stream input.
function pipeLines(input, fn) {
  readline.createInterface({ input, crlfDelay: Infinity, terminal: false, historySize: 0 }).on('line', fn);
}

const stdioOptions = {
  stdout: (input, output) => pipeLines(input, (line) => output.info(line)),
  stderr: (input, output) => pipeLines(input, (line) => logServerLine(output, line)),
};

function errorText(err) {
  return `${err && err.message ? err.message : err}`;
}

class LanguageServer {
  constructor(output, timers = { setTimeout, clearTimeout }) {
    this.output = output;
    this.timers = timers;
    this.client = null;
    this.started = null; // this.client once it has started
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
    // pluto is looked up again at every start, so a restart or a new
    // plutonium.plutoPath finds the current one.
    let command;
    try {
      command = resolvePluto(undefined, this.output, { always: true });
    } catch (err) {
      this.reportStartFailure(err.message);
      return;
    }
    // No transport: with TransportKind.stdio the client would add --stdio
    // to the arguments, which pluto lsp rejects. Without one it still talks
    // to the server over standard input and output.
    const serverOptions = { command, args: ['lsp'] };
    let client = null;
    const clientOptions = {
      documentSelector: [{ scheme: 'file', language: 'plutonium' }],
      synchronize: {
        configurationSection: 'plutonium',
        fileEvents: vscode.workspace.createFileSystemWatcher('**/{*.p,pluto.mod}'),
      },
      initializationOptions: serverSettings(),
      outputChannel: this.output,
      stdioOptions,
      errorHandler: {
        // The client logs the error; closed decides what happens next.
        error: () => ({ action: lc.ErrorAction.Continue, handled: true }),
        closed: () => this.closed(client),
      },
    };
    this.stopping = false;
    try {
      client = new lc.LanguageClient('plutonium', 'Plutonium Language Server', serverOptions, clientOptions);
      this.client = client;
      await client.start();
      this.started = client;
      this.output.info(`Started ${command} lsp`);
    } catch (err) {
      if (this.client === client) {
        this.client = null;
      }
      // The client leaves a server that failed to initialize running.
      const proc = client && client.serverProcess;
      if (proc && proc.exitCode === null && proc.signalCode === null) {
        proc.kill();
      }
      // A command that cannot be run is reported as a string, "Launching
      // server using command pluto failed. Error: spawn pluto ENOENT".
      const missing = (err && err.code === 'ENOENT') || /\bENOENT\b/.test(errorText(err));
      this.reportStartFailure(missing ? `${command} was not found. Install pluto or set plutonium.plutoPath.` : errorText(err));
    }
  }

  // reportStartFailure logs why the server could not start and says so
  // once in a message.
  reportStartFailure(msg) {
    this.output.error(`Cannot start the language server: ${msg}`);
    vscode.window.showErrorMessage(`Plutonium: cannot start the language server: ${msg}`, 'Show Output')
      .then((choice) => choice && this.output.show());
  }

  async stop() {
    const client = this.client;
    this.client = null;
    this.started = null;
    this.stopping = true;
    if (client) {
      try {
        await client.stop();
      } catch (err) {
        this.output.warn(`Stopping the language server: ${errorText(err)}`);
      }
    }
  }

  async restart() {
    await this.stop();
    this.crashes = [];
    await this.start();
  }

  // closed decides what happens when client's connection closes without
  // the extension stopping it: the server crashed or exited. A client that
  // never started, or has been replaced, is not restarted; start reports
  // why it failed.
  async closed(client) {
    const lc = this.lc;
    const stay = (message) => ({ action: lc.CloseAction.DoNotRestart, handled: true, message });
    if (this.stopping || client !== this.client || client !== this.started) {
      return stay('The language server stopped.');
    }
    const now = Date.now();
    this.crashes = this.crashes.filter((t) => now - t < CRASH_WINDOW_MS);
    this.crashes.push(now);
    if (this.crashes.length > MAX_CRASHES) {
      this.client = null;
      this.started = null;
      const msg = `The Plutonium language server crashed ${this.crashes.length} times in the last few minutes and was not restarted.`;
      vscode.window.showErrorMessage(msg, 'Show Output', 'Restart').then((choice) => {
        if (choice === 'Restart') {
          this.restart();
        } else if (choice === 'Show Output') {
          this.output.show();
        }
      });
      return stay(msg);
    }
    const delay = Math.min(RESTART_BASE_MS * 2 ** (this.crashes.length - 1), RESTART_MAX_MS);
    this.output.warn(`The language server stopped unexpectedly; restarting in ${(delay / 1000).toFixed(1)}s.`);
    await new Promise((resolve) => this.timers.setTimeout(resolve, delay));
    if (this.stopping || client !== this.client) {
      return stay('The language server stopped.');
    }
    return { action: lc.CloseAction.Restart, handled: true, message: 'Restarting the language server.' };
  }
}

// EXPLAIN_SCHEME is the scheme of the read-only documents that show
// `pluto explain CODE`, which the server's "Explain error" code actions
// open through the plutonium.explainError command.
const EXPLAIN_SCHEME = 'plutonium-explain';

const explainProvider = {
  async provideTextDocumentContent(uri) {
    const code = uri.path.replace(/^\//, '').replace(/\.txt$/, '');
    try {
      const res = await runPluto(['explain', code], { timeoutMs: 10000 });
      return res.code === 0 ? res.stdout : `pluto explain ${code} failed:\n\n${res.stderr}`;
    } catch (err) {
      return `Cannot run pluto explain ${code}: ${errorText(err)}`;
    }
  },
};

async function explainError(code) {
  if (typeof code !== 'string' || !/^E\d{4}$/.test(code)) {
    return;
  }
  const doc = await vscode.workspace.openTextDocument(vscode.Uri.parse(`${EXPLAIN_SCHEME}:/${code}.txt`));
  await vscode.window.showTextDocument(doc, { preview: true, viewColumn: vscode.ViewColumn.Beside });
}

// register adds the language server, its restart command and the error
// explanations to context and returns the server; extension.js starts and
// stops it as settings change.
function register(context, output) {
  const server = new LanguageServer(output);
  context.subscriptions.push(
    vscode.workspace.registerTextDocumentContentProvider(EXPLAIN_SCHEME, explainProvider),
    vscode.commands.registerCommand('plutonium.explainError', explainError),
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

module.exports = {
  register, wanted, LanguageServer, explainProvider, logServerLine, RESTART_BASE_MS, RESTART_MAX_MS, MAX_CRASHES,
};
