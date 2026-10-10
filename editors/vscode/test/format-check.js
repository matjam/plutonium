#!/usr/bin/env node
'use strict';

// Exercises the formatter outside VS Code: activates extension.js against a
// minimal stand-in for the vscode module and runs the registered formatting
// provider with a real pluto binary.
//
//   node test/format-check.js /path/to/pluto

const assert = require('assert');
const fs = require('fs');
const Module = require('module');
const os = require('os');
const path = require('path');
const { createOutputChannel } = require('./fake-output');

const pluto = process.argv[2];
if (!pluto) {
  console.error('usage: format-check.js /path/to/pluto');
  process.exit(2);
}

// A stand-in for the parts of the vscode API the extension uses.
// The language server is off, so that the stand-alone formatter runs.
const settings = { plutoPath: pluto, formatTimeout: 10000, useLanguageServer: false };
const outputLines = [];
const errorMessages = [];
const diagnostics = new Map();
let provider = null;
let changeListener = null;

class Position {
  constructor(line, character) {
    this.line = line;
    this.character = character;
  }
}
class Range {
  constructor(start, end) {
    this.start = start;
    this.end = end;
  }
}
class Diagnostic {
  constructor(range, message, severity) {
    this.range = range;
    this.message = message;
    this.severity = severity;
  }
}
const disposable = { dispose() {} };

const vscode = {
  Position,
  Range,
  Diagnostic,
  DiagnosticSeverity: { Error: 0 },
  TextEdit: { replace: (range, newText) => ({ range, newText }) },
  workspace: {
    isTrusted: true,
    workspaceFolders: undefined,
    getConfiguration: () => ({ get: (key, dflt) => (key in settings ? settings[key] : dflt) }),
    getWorkspaceFolder: () => undefined,
    onDidChangeTextDocument: (fn) => {
      changeListener = fn;
      return disposable;
    },
    onDidCloseTextDocument: () => disposable,
    onDidChangeConfiguration: () => disposable,
    onDidGrantWorkspaceTrust: () => disposable,
    registerTextDocumentContentProvider: () => disposable,
  },
  commands: {
    registerCommand: () => disposable,
  },
  languages: {
    createDiagnosticCollection: () => ({
      set: (uri, list) => diagnostics.set(uri.toString(), list),
      delete: (uri) => diagnostics.delete(uri.toString()),
      clear: () => diagnostics.clear(),
      dispose() {},
    }),
    registerDocumentFormattingEditProvider: (selector, p) => {
      assert.deepStrictEqual(selector, { language: 'plutonium' });
      provider = p;
      return disposable;
    },
  },
  window: {
    createOutputChannel: (name, options) => createOutputChannel(name, options, outputLines),
    showErrorMessage: (m) => errorMessages.push(m),
    setStatusBarMessage: () => disposable,
  },
};

const resolve = Module._resolveFilename;
Module._resolveFilename = function (request, ...rest) {
  return request === 'vscode' ? 'vscode' : resolve.call(this, request, ...rest);
};
require.cache.vscode = { id: 'vscode', filename: 'vscode', loaded: true, exports: vscode };

function documentOf(text) {
  const uri = { scheme: 'untitled', fsPath: '', toString: () => 'untitled:sample.p' };
  return {
    uri,
    getText: () => text,
    positionAt: (offset) => {
      const before = text.slice(0, offset).split('\n');
      return new Position(before.length - 1, before[before.length - 1].length);
    },
  };
}

const token = { isCancellationRequested: false, onCancellationRequested: () => disposable };

function format(text) {
  return provider.provideDocumentFormattingEdits(documentOf(text), {}, token);
}

async function main() {
  const context = { subscriptions: [] };
  await require(path.join(__dirname, '..', 'extension.js')).activate(context);
  assert.ok(provider, 'activate registers a formatting provider');

  // Unformatted source is replaced by pluto fmt's output.
  const messy = 'package main\nfunc main( ) {\nx:=1\n_ = x}\n';
  const tidy = 'package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n';
  let edits = await format(messy);
  assert.strictEqual(edits.length, 1);
  assert.strictEqual(edits[0].newText, tidy);
  assert.deepStrictEqual(edits[0].range.end, new Position(4, 0));

  // Formatted source needs no edit.
  edits = await format(tidy);
  assert.deepStrictEqual(edits, []);

  // A syntax error leaves the document alone and becomes a diagnostic.
  edits = await format('package main\nfunc main( ) {\nx:=1\n');
  assert.deepStrictEqual(edits, []);
  const list = diagnostics.get('untitled:sample.p');
  assert.ok(list && list.length === 1, 'one diagnostic for the syntax error');
  assert.strictEqual(list[0].range.start.line, 2);
  assert.match(list[0].message, /expected '}'/);
  assert.ok(outputLines.some((l) => l.startsWith('error: ') && l.includes('untitled:sample.p:3:')),
    'the error is logged, with the document name');

  // Editing the document clears the diagnostic.
  changeListener({ document: documentOf(''), contentChanges: [{}] });
  assert.strictEqual(diagnostics.has('untitled:sample.p'), false);

  // A missing pluto is reported once, and the document is left alone.
  settings.plutoPath = path.join(__dirname, 'no-such-pluto');
  assert.deepStrictEqual(await format(messy), []);
  assert.deepStrictEqual(await format(messy), []);
  assert.strictEqual(errorMessages.length, 1);
  assert.match(errorMessages[0], /^Plutonium: cannot format: .*no-such-pluto \(plutonium.plutoPath\) was not found/);

  // With no setting, and PATH without pluto, the formatter finds it in
  // ~/go/bin (here a fake HOME) and logs where.
  const home = fs.mkdtempSync(path.join(os.tmpdir(), 'pluto-format-check-'));
  const saved = { PATH: process.env.PATH, HOME: process.env.HOME, GOBIN: process.env.GOBIN, GOPATH: process.env.GOPATH };
  try {
    fs.mkdirSync(path.join(home, 'go', 'bin'), { recursive: true });
    const installed = path.join(home, 'go', 'bin', 'pluto');
    fs.symlinkSync(path.resolve(pluto), installed);
    delete settings.plutoPath;
    process.env.PATH = path.join(home, 'empty');
    process.env.HOME = home;
    delete process.env.GOBIN;
    delete process.env.GOPATH;
    edits = await format(messy);
    assert.strictEqual(edits.length, 1);
    assert.strictEqual(edits[0].newText, tidy);
    assert.ok(outputLines.includes(`info: Using pluto at ${installed}`), outputLines.join('\n'));
  } finally {
    for (const [k, v] of Object.entries(saved)) {
      if (v === undefined) {
        delete process.env[k];
      } else {
        process.env[k] = v;
      }
    }
    fs.rmSync(home, { recursive: true, force: true });
    settings.plutoPath = pluto;
  }

  // Nothing runs in an untrusted workspace.
  vscode.workspace.isTrusted = false;
  assert.deepStrictEqual(await format(messy), []);
  vscode.workspace.isTrusted = true;

  console.log('ok: formatter checks pass');
}

main().catch((err) => {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
