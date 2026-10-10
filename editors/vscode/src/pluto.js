'use strict';

// Locating and running the pluto compiler.
//
// The language server and the formatter both find pluto with resolvePluto,
// which looks, in order, at:
//
//  1. plutonium.plutoPath, if it is set (to anything but "pluto"), with ~ and
//     ${workspaceFolder} expanded. A path is used as it is, with no fallback
//     if it does not exist; another bare name is looked up on PATH only.
//  2. pluto on the extension host's PATH, which an editor started from a
//     desktop launcher often inherits without ~/go/bin.
//  3. Where `go install` puts it: $GOBIN/pluto, bin/pluto under each $GOPATH
//     entry, and ~/go/bin/pluto (Go's default GOPATH).
//
// Only executable regular files count. The environment and home directory
// are read at each call, so a restart picks up a changed setting or a newly
// installed pluto.

const childProcess = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');
const vscode = require('vscode');

const EXE = process.platform === 'win32' ? '.exe' : '';
const PLUTO = `pluto${EXE}`;

// configuredPath returns the plutonium.plutoPath setting with ~ and
// ${workspaceFolder} expanded, or null when it is empty or the default
// bare "pluto", which means search.
function configuredPath(uri) {
  const config = vscode.workspace.getConfiguration('plutonium', uri);
  let p = `${config.get('plutoPath', '') || ''}`.trim();
  if (p === '' || p === 'pluto' || p === PLUTO) {
    return null;
  }
  if (p === '~' || p.startsWith('~/') || p.startsWith(`~${path.sep}`)) {
    p = path.join(os.homedir(), p.slice(1));
  }
  if (p.includes('${workspaceFolder}')) {
    const folder = (uri && vscode.workspace.getWorkspaceFolder && vscode.workspace.getWorkspaceFolder(uri)) ||
      (vscode.workspace.workspaceFolders || [])[0];
    if (folder) {
      p = p.split('${workspaceFolder}').join(folder.uri.fsPath);
    }
  }
  return p;
}

// isExecutable reports whether p is a regular file the user may execute.
function isExecutable(p) {
  try {
    if (!fs.statSync(p).isFile()) {
      return false;
    }
    fs.accessSync(p, fs.constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

// searchPath returns the absolute path of the first executable named name in
// a directory of PATH, or null.
function searchPath(name) {
  for (const dir of (process.env.PATH || '').split(path.delimiter)) {
    if (dir === '') {
      continue;
    }
    const p = path.resolve(dir, name);
    if (isExecutable(p)) {
      return p;
    }
  }
  return null;
}

// goInstallPaths returns where `go install` would have put pluto: $GOBIN,
// each $GOPATH entry's bin, and ~/go/bin, without duplicates.
function goInstallPaths() {
  const dirs = [];
  if (process.env.GOBIN) {
    dirs.push(process.env.GOBIN);
  }
  for (const entry of (process.env.GOPATH || '').split(path.delimiter)) {
    if (entry !== '') {
      dirs.push(path.join(entry, 'bin'));
    }
  }
  const home = os.homedir();
  if (home) {
    dirs.push(path.join(home, 'go', 'bin'));
  }
  const seen = new Set();
  const out = [];
  for (const dir of dirs) {
    const p = path.resolve(dir, PLUTO);
    if (!seen.has(p)) {
      seen.add(p);
      out.push(p);
    }
  }
  return out;
}

// locatePluto finds pluto as the comment at the top of this file says. It
// returns { path, message }: path is the absolute path of pluto, or null
// with message saying what was searched.
function locatePluto(uri) {
  const setting = configuredPath(uri);
  if (setting !== null) {
    if (!setting.includes('/') && !setting.includes(path.sep)) {
      const found = searchPath(setting);
      return found ? { path: found } : { path: null, message: `${setting} (plutonium.plutoPath) was not found on PATH.` };
    }
    const p = path.resolve(setting);
    if (isExecutable(p)) {
      return { path: p };
    }
    const why = fs.existsSync(p) ? 'is not an executable file' : 'was not found';
    return { path: null, message: `${p} (plutonium.plutoPath) ${why}. Install pluto there or change plutonium.plutoPath.` };
  }
  const onPath = searchPath(PLUTO);
  if (onPath) {
    return { path: onPath };
  }
  const fallbacks = goInstallPaths();
  for (const p of fallbacks) {
    if (isExecutable(p)) {
      return { path: p };
    }
  }
  return {
    path: null,
    message: `pluto was not found on PATH or at ${fallbacks.join(', ')}. Install pluto or set plutonium.plutoPath.`,
  };
}

// announced holds the path last logged to each output channel.
const announced = new WeakMap();

// resolvePluto returns the absolute path of pluto, found by locatePluto.
// When output is given it logs "Using pluto at PATH" to it, every time if
// always is set and otherwise when the path differs from the last one it
// logged there. If pluto cannot be found it throws an Error whose code is
// 'ENOENT', with notFound set and the message saying where it looked.
function resolvePluto(uri, output, { always = false } = {}) {
  const found = locatePluto(uri);
  if (!found.path) {
    const err = new Error(found.message);
    err.code = 'ENOENT';
    err.notFound = true;
    throw err;
  }
  if (output && (always || announced.get(output) !== found.path)) {
    announced.set(output, found.path);
    output.info(`Using pluto at ${found.path}`);
  }
  return found.path;
}

// runPluto runs pluto with args, writes input to its standard input, and
// resolves to { code, stdout, stderr } when it exits. It rejects if pluto
// cannot be found or started (err.code is 'ENOENT' when it is not found, and
// err.notFound is set when resolvePluto did not find it), when it runs
// longer than timeoutMs, or when token is cancelled (err.cancelled is true).
// With output, the path of pluto is logged there when it changes.
function runPluto(args, { uri, cwd, input, timeoutMs, token, output } = {}) {
  let command;
  try {
    command = resolvePluto(uri, output);
  } catch (err) {
    return Promise.reject(err);
  }
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

module.exports = { resolvePluto, runPluto };
