'use strict';

// Entry point of the Plutonium extension. Highlighting and the language
// configuration are declarative (package.json); activate registers the
// features that need code. Each feature lives in its own module under src/
// and exports register(context, output); add new ones to the list below.

const vscode = require('vscode');
const format = require('./src/format');

const features = [format];

function activate(context) {
  const output = vscode.window.createOutputChannel('Plutonium');
  context.subscriptions.push(output);
  for (const feature of features) {
    feature.register(context, output);
  }
}

function deactivate() {}

module.exports = { activate, deactivate };
