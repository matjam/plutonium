'use strict';

// A stand-in for vscode.window.createOutputChannel, shared by the checks
// that run the extension outside VS Code. Like VS Code, it returns a
// LogOutputChannel (with trace, debug, info, warn and error, a logLevel and
// onDidChangeLogLevel) only when it is asked for one with { log: true }, and
// a plain OutputChannel otherwise, so a check fails if the extension hands
// vscode-languageclient a channel it cannot log to.

// LogLevel matches vscode.LogLevel.
const LogLevel = { Off: 0, Trace: 1, Debug: 2, Info: 3, Warning: 4, Error: 5 };

// createOutputChannel returns a channel that records what is written to it
// in lines, as "level: message" for a LogOutputChannel.
function createOutputChannel(name, options, lines = []) {
  const channel = {
    name,
    lines,
    shown: 0,
    append: (s) => lines.push(s),
    appendLine: (s) => lines.push(s),
    replace() {},
    clear() {},
    show() {
      channel.shown++;
    },
    hide() {},
    dispose() {},
  };
  if (options && typeof options === 'object' && options.log === true) {
    for (const level of ['trace', 'debug', 'info', 'warn', 'error']) {
      channel[level] = (message) => lines.push(`${level}: ${message}`);
    }
    channel.logLevel = LogLevel.Info;
    channel.onDidChangeLogLevel = () => ({ dispose() {} });
  }
  return channel;
}

// isLogOutputChannel reports whether channel has what vscode-languageclient
// 10 uses of a LogOutputChannel.
function isLogOutputChannel(channel) {
  return Boolean(channel) &&
    ['trace', 'debug', 'info', 'warn', 'error', 'onDidChangeLogLevel'].every((m) => typeof channel[m] === 'function') &&
    typeof channel.logLevel === 'number';
}

module.exports = { LogLevel, createOutputChannel, isLogOutputChannel };
