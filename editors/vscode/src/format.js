'use strict';

// Formatting with pluto fmt, which reads the document on standard input and
// writes the formatted source to standard output.

const fs = require('fs');
const path = require('path');
const vscode = require('vscode');
const { runPluto } = require('./pluto');

const STDIN_NAME = '<standard input>';

// register adds the formatter's diagnostics to context and returns a
// controller whose setEnabled(on) registers or removes the formatter. Syntax
// errors that stop pluto fmt are shown as diagnostics until the document
// changes.
function register(context, output) {
  const diagnostics = vscode.languages.createDiagnosticCollection('pluto fmt');
  let warnedMissing = false;

  async function provideDocumentFormattingEdits(document, _options, token) {
    if (!vscode.workspace.isTrusted) {
      output.info('pluto fmt: not run because the workspace is not trusted');
      return [];
    }
    const text = document.getText();
    const config = vscode.workspace.getConfiguration('plutonium', document.uri);
    let result;
    try {
      result = await runPluto(['fmt'], {
        uri: document.uri,
        cwd: workingDirectory(document),
        input: text,
        timeoutMs: config.get('formatTimeout', 10000),
        token,
        output,
      });
    } catch (err) {
      if (err.cancelled) {
        return [];
      }
      if (err.code === 'ENOENT') {
        // resolvePluto's message says where it looked; a pluto that went
        // away after it was found is reported by its path.
        const msg = err.notFound ? err.message : `${err.command} was not found. Install pluto or set plutonium.plutoPath.`;
        output.error(`pluto fmt: cannot run pluto: ${msg}`);
        if (!warnedMissing) {
          warnedMissing = true;
          vscode.window.showErrorMessage(`Plutonium: cannot format: ${msg}`);
        }
      } else {
        output.error(`pluto fmt: ${err.message}`);
      }
      return [];
    }
    if (token.isCancellationRequested) {
      return [];
    }
    const name = document.uri.scheme === 'file' ? document.uri.fsPath : document.uri.toString();
    if (result.code !== 0) {
      const stderr = result.stderr.split(STDIN_NAME).join(name).trimEnd();
      output.error(`pluto fmt failed for ${name} (exit status ${result.code}):\n${stderr}`);
      diagnostics.set(document.uri, parseErrors(result.stderr));
      vscode.window.setStatusBarMessage('$(error) pluto fmt failed; see the Plutonium output', 5000);
      return [];
    }
    diagnostics.delete(document.uri);
    if (result.stdout === text) {
      return [];
    }
    const all = new vscode.Range(document.positionAt(0), document.positionAt(text.length));
    return [vscode.TextEdit.replace(all, result.stdout)];
  }

  // The provider is registered only while the language server is off: the
  // server formats with the same code, and two formatters for a language
  // make VS Code ask which to use.
  let registration = null;
  function setEnabled(on) {
    if (on && !registration) {
      registration = vscode.languages.registerDocumentFormattingEditProvider(
        { language: 'plutonium' }, { provideDocumentFormattingEdits });
    } else if (!on && registration) {
      registration.dispose();
      registration = null;
      diagnostics.clear();
    }
  }

  context.subscriptions.push(
    diagnostics,
    { dispose: () => setEnabled(false) },
    vscode.workspace.onDidChangeTextDocument((e) => {
      if (e.contentChanges.length > 0) {
        diagnostics.delete(e.document.uri);
      }
    }),
    vscode.workspace.onDidCloseTextDocument((d) => diagnostics.delete(d.uri)),
  );
  return { setEnabled };
}

// workingDirectory returns the document's directory if it exists on disk,
// else the first workspace folder, else undefined (the current directory).
function workingDirectory(document) {
  if (document.uri.scheme === 'file') {
    const dir = path.dirname(document.uri.fsPath);
    if (fs.existsSync(dir)) {
      return dir;
    }
  }
  const folder = (vscode.workspace.workspaceFolders || [])[0];
  return folder && folder.uri.scheme === 'file' ? folder.uri.fsPath : undefined;
}

// parseErrors turns pluto's "<standard input>:line:col: message" lines into
// diagnostics.
function parseErrors(stderr) {
  const out = [];
  const re = /^<standard input>:(\d+):(\d+): (.*)$/;
  for (const line of stderr.split('\n')) {
    const m = re.exec(line.trim());
    if (!m) {
      continue;
    }
    const pos = new vscode.Position(Math.max(0, Number(m[1]) - 1), Math.max(0, Number(m[2]) - 1));
    const d = new vscode.Diagnostic(new vscode.Range(pos, pos), m[3], vscode.DiagnosticSeverity.Error);
    d.source = 'pluto fmt';
    out.push(d);
  }
  return out;
}

module.exports = { register };
