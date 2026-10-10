'use strict';

// Locating and running the pluto compiler.

const childProcess = require('child_process');
const os = require('os');
const path = require('path');
const vscode = require('vscode');

// plutoPath returns the pluto command from the plutonium.plutoPath setting,
// with ~ and ${workspaceFolder} expanded. A bare name is left for the
// operating system to find on PATH.
function plutoPath(uri) {
  const config = vscode.workspace.getConfiguration('plutonium', uri);
  let p = (config.get('plutoPath') || 'pluto').trim();
  if (p === '~' || p.startsWith('~/')) {
    p = path.join(os.homedir(), p.slice(1));
  }
  if (p.includes('${workspaceFolder}')) {
    const folder = (uri && vscode.workspace.getWorkspaceFolder(uri)) ||
      (vscode.workspace.workspaceFolders || [])[0];
    if (folder) {
      p = p.split('${workspaceFolder}').join(folder.uri.fsPath);
    }
  }
  return p;
}

// runPluto runs pluto with args, writes input to its standard input, and
// resolves to { code, stdout, stderr } when it exits. It rejects if pluto
// cannot be started (err.code is 'ENOENT' when it is not found), when it runs
// longer than timeoutMs, or when token is cancelled (err.cancelled is true).
function runPluto(args, { uri, cwd, input, timeoutMs, token } = {}) {
  const command = plutoPath(uri);
  return new Promise((resolve, reject) => {
    let settled = false;
    let timer;
    let subscription;
    const finish = (fn, value) => {
      if (!settled) {
        settled = true;
        clearTimeout(timer);
        if (subscription) {
          subscription.dispose();
        }
        fn(value);
      }
    };
    let child;
    try {
      child = childProcess.spawn(command, args, { cwd, stdio: ['pipe', 'pipe', 'pipe'] });
    } catch (err) {
      reject(err);
      return;
    }
    const stdout = [];
    const stderr = [];
    child.stdout.on('data', (b) => stdout.push(b));
    child.stderr.on('data', (b) => stderr.push(b));
    child.on('error', (err) => {
      err.command = command;
      finish(reject, err);
    });
    child.on('close', (code) => finish(resolve, {
      code,
      stdout: Buffer.concat(stdout).toString('utf8'),
      stderr: Buffer.concat(stderr).toString('utf8'),
    }));
    child.stdin.on('error', () => {
      // pluto exited before reading all of its input; 'close' reports why.
    });
    if (timeoutMs) {
      timer = setTimeout(() => {
        child.kill();
        finish(reject, new Error(`${command} ${args.join(' ')} timed out after ${timeoutMs} ms`));
      }, timeoutMs);
    }
    if (token) {
      subscription = token.onCancellationRequested(() => {
        child.kill();
        const err = new Error('cancelled');
        err.cancelled = true;
        finish(reject, err);
      });
    }
    child.stdin.end(input === undefined ? '' : input);
  });
}

module.exports = { plutoPath, runPluto };
