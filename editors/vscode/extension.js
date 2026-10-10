'use strict';

// Entry point of the Plutonium extension. Highlighting and the language
// configuration are declarative (package.json); activate registers the
// features that need code, each in its own module under src/.
//
// With plutonium.useLanguageServer on (the default), `pluto lsp` provides
// diagnostics, navigation, completion and formatting, and the stand-alone
// pluto fmt formatter is turned off; with it off, only that formatter runs.

const vscode = require('vscode');
const format = require('./src/format');
const lsp = require('./src/lsp');

let server = null;

function activate(context) {
  // A LogOutputChannel: vscode-languageclient logs through its info, warn
  // and error methods, which a plain OutputChannel lacks.
  const output = vscode.window.createOutputChannel('Plutonium', { log: true });
  context.subscriptions.push(output);
  const formatter = format.register(context, output);
  server = lsp.register(context, output);

  // apply starts or stops the server and the stand-alone formatter to
  // match the settings; restart is set when the server's command changed.
  const apply = async (restart) => {
    const useServer = lsp.wanted();
    formatter.setEnabled(!useServer);
    if (!useServer) {
      await server.stop();
    } else if (restart && server.running) {
      await server.restart();
    } else {
      await server.start();
    }
  };
  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration('plutonium.useLanguageServer') || e.affectsConfiguration('plutonium.plutoPath')) {
        return apply(e.affectsConfiguration('plutonium.plutoPath'));
      }
      return undefined;
    }),
    vscode.workspace.onDidGrantWorkspaceTrust(() => apply(false)),
  );
  return apply(false);
}

function deactivate() {
  return server ? server.stop() : undefined;
}

module.exports = { activate, deactivate };
