# Plutonium for Visual Studio Code

Language support for [Plutonium](https://github.com/matjam/plutonium) source files (`.p`):
highlighting, and through the `pluto lsp` language server, diagnostics, hover, navigation,
completion and formatting. Works in VS Code and its forks (Cursor, VSCodium).

## Features

**The language server**, `pluto lsp`, runs while `plutonium.useLanguageServer` is on (the
default). It type-checks open files as you type (without compiling them), and provides:

- diagnostics: syntax and type errors, including Plutonium's own rules, for open files and their
  packages, with the error code (`E0024`) and its explanation;
- hover: the declaration and doc comment of the name under the cursor, its kind under the
  ownership rules (value, view, `owner type (moves)`, shared), an enum's members, and what a
  `//pu:` directive means; on an error, its full explanation;
- go to definition, including into the standard library, and find references in the workspace;
- the outline (document symbols) and workspace symbols;
- completion of names in scope, package members, fields, methods and enum members, and keywords;
- signature help, rename, and *Explain this error* (a code action that shows `pluto explain`);
- formatting, the same as `pluto fmt`.

It checks code for the target and mode in the settings (default `x86_64-linux`, hosted). A file
that this build leaves out, such as one with `//pu:build kernel`, is checked in the first mode that
includes it. The standard library comes from `$PLUTOROOT` if it is set, otherwise from the copy
inside `pluto`, extracted read-only to `~/.cache/pluto/` so that go to definition can open it.

If the server stops unexpectedly it is restarted after a short delay that doubles with each crash
(up to 30 seconds), which the *Plutonium* output records; after five crashes in three minutes it
stays stopped, and one message offers to show the output or restart it. A server that cannot start
is reported once and not restarted. *Plutonium: Restart Language Server* restarts it by hand. Its
log goes to the *Plutonium* output, at the level of each line; set that output's log level to
*Trace* (the gear in the Output panel) to add every message (`plutonium.trace.server` chooses
`messages` or `verbose`).

**Highlighting** of the whole language as `docs/spec/language.md` describes it:

- Go's keywords, operators (including `<-`, `&^=` and `...`), comments, labels, and package and
  import clauses. `map` is shown as a removed construct, since Plutonium rejects it; the complex
  types and `complex`, `real`, `imag`, `delete` and `recover` are not highlighted as builtins.
- Plutonium's additions: `packed` types with `le`/`be` byte order, `bits[T, N]` fields and
  `bits { A B C }` flag groups, `enum` types and their members, `union` types and their variants,
  `owner` in type definitions and constraints, default parameter values (`a Allocator =
  alloc.Default()`), and methods with type parameters.
- `//pu:` directives (`//pu:build`, `//pu:inline`, `//pu:extern name`, `//pu:interrupt` and the
  rest), highlighted as directives the way Go highlights `//go:`. Any `//pu:name` is accepted, so
  new directives need no grammar change. `//pu:build` expressions get their operators and tags
  highlighted.
- Predeclared types, the type constructors `volatile[T]`, `aligned[T, N]`, `ring[T, N]`,
  `owned[T]`, `shared[T]`, `synced[T]` and `weak[P]` (only where they take type arguments, so a
  variable named `shared` is not mistaken for one), the builtins `len`, `cap`, `new`, `make`,
  `append`, `take`, `min`, `max`, `clear`, `close`, `copy`, `print`, `println` and `panic` (only
  where called), and `unsafe`'s functions.
- Interpreted strings with escapes and `fmt` verbs, raw strings, runes, and numeric literals in
  every base with `_` separators, including hexadecimal floats.
- Function and method declarations (receivers, type parameters, parameter names, result types),
  function literals and types, struct, interface, packed, enum and union bodies, generic calls such
  as `v.Map[int](f)`.

**Formatting** with `pluto fmt` when the language server is off: *Format Document* (and format on
save, on by default for Plutonium) sends the buffer to `pluto fmt` on standard input and replaces
it with the output. If `pluto fmt` fails, the buffer is left as it was, the error goes to the
*Plutonium* output channel, and a syntax error is shown as a diagnostic until the document is
edited. While the language server runs, it formats instead, and this formatter is not registered.

**Editing**: comment toggling, bracket matching, auto-closing of brackets, quotes and backticks,
Go-style indentation, and `// region` / `// endregion` folding markers.

## Settings

| Setting | Default | Meaning |
|---|---|---|
| `plutonium.plutoPath` | (search) | The compiler, which runs the language server and formats. See [Finding pluto](#finding-pluto). Changing it restarts the server. |
| `plutonium.useLanguageServer` | `true` | Run `pluto lsp`. When off, only `pluto fmt` formatting is available. |
| `plutonium.target` | `x86_64-linux` | The target the server checks code for: `x86_64-linux` or `x86_64-none`. |
| `plutonium.mode` | (from the target) | The build mode: `hosted`, `freestanding` or `kernel`, as `pluto build --mode`. |
| `plutonium.tags` | `[]` | More build tags, as `pluto build --tags`. |
| `plutonium.trace.server` | `off` | `messages` or `verbose`: how the protocol is logged to the *Plutonium* output while its log level is *Trace*. |
| `plutonium.formatTimeout` | `10000` | Milliseconds to wait for `pluto fmt` (with the server off). |

The extension sets `editor.insertSpaces: false` and `editor.formatOnSave: true` for Plutonium;
override them under `"[plutonium]"` in your settings. In an untrusted workspace `pluto` is never
run, and `plutonium.plutoPath` is taken only from user settings.

### Finding pluto

The language server, the formatter and *Explain this error* all run the same `pluto`, the first
of:

1. `plutonium.plutoPath`, if it is set (to anything but `pluto`), with `~` and
   `${workspaceFolder}` expanded. A path is used as it is: if nothing executable is there, the
   error names that path and nothing else is tried. Another bare name is looked up on `PATH`.
2. `pluto` on the editor's `PATH`.
3. Where `go install` puts it: `$GOBIN/pluto`, `bin/pluto` under each `$GOPATH` entry, and
   `~/go/bin/pluto` (Go's default `GOPATH`). An editor started from a desktop launcher often has
   a `PATH` without `~/go/bin`, so a `pluto` copied there is found too. Build `pluto` as
   `AGENTS.md` says ("Self-hosted compiler").

Only executable regular files count. The *Plutonium* output logs the one in use (`Using pluto at
/home/you/go/bin/pluto`); if none is found, the error lists the places searched. The search is
done again when the server restarts and when `plutonium.plutoPath` changes, so a newly installed
`pluto` is picked up by *Plutonium: Restart Language Server*.

## Install

The extension is plain JavaScript with no build step. It depends on `vscode-languageclient`, and
`@vscode/vsce` packages it; both come from npm, pinned by `package-lock.json`. `node_modules/` is
not committed. To package and install it:

```sh
# build pluto first (AGENTS.md); the language server is pluto lsp
cd editors/vscode
npm ci                                     # install the locked dependencies
npm run package                            # writes plutonium-<version>.vsix
code --install-extension plutonium-0.2.2.vsix
cursor --install-extension plutonium-0.2.2.vsix   # or codium, code-insiders
```

`vsce` puts the extension's files and its production dependencies (`vscode-languageclient` and
what it needs) in the `.vsix`, leaving out what `.vscodeignore` lists and the development
dependencies. Reload the editor window (*Developer: Reload Window*) after installing.

For development, run `npm ci`, then the editor with
`code --extensionDevelopmentPath=$PWD/editors/vscode`.

## Tests

`go test ./editors/vscode/` (part of the repository's `go test ./...`) checks that:

- `package.json`, the language configuration and the grammar parse, and refer to files that exist;
- every keyword of `compiler/internal/syntax/token` is highlighted, and every predeclared name of
  `compiler/internal/types2` (less the removed complex and map builtins) is highlighted as a builtin type,
  function, constant or type constructor, so a new keyword or builtin cannot be forgotten;
- every regex compiles (`test/grammar-check.js regexps`). TextMate grammars use Oniguruma, which
  Go's `regexp` cannot parse (no lookaround), so the check uses JavaScript's `RegExp` with the `u`
  flag. The grammar keeps to the syntax both engines accept: no `\h`, `\G`, `\A`, `\z`, possessive
  or atomic groups, or POSIX classes. The `u` flag makes JavaScript reject those rather than
  silently read them as literals.
- the scope assertions in `test/sample.p` hold, and every `.p` file under `lib/` and `examples/`
  ends with no grammar region left open (`test/grammar-check.js tokens`). This uses the
  `vscode-textmate` and `vscode-oniguruma` modules bundled with an installed VS Code-family editor,
  so it also compiles every regex with real Oniguruma; set `PLUTONIUM_TEXTMATE_MODULES` to a
  `node_modules` directory holding both if yours is elsewhere. The test is skipped when none is
  found.
- the formatter, run through `extension.js` against a stand-in for the `vscode` module and the pluto
  named by `PLUTO_TEST_COMPILER` (else `pluto` on `PATH`), formats, leaves formatted text alone, reports syntax errors as diagnostics
  without touching the buffer, copes with a missing `pluto`, and finds `pluto` in `~/go/bin` (a
  fake `HOME`) when the setting is unset and `PATH` lacks it (`test/format-check.js`).
- the language server client, run through `extension.js` against stand-ins for `vscode` and
  `vscode-languageclient`, starts `pluto lsp` with the settings, swaps the stand-alone formatter
  out while it runs, follows `useLanguageServer`, `plutoPath` and workspace trust, reports a
  server that cannot start once, and restarts a crashed server with doubling delays until it
  gives up with one message (`test/lsp-check.js`). The stand-ins hold the extension to what
  `vscode-languageclient` 10 needs: a `LogOutputChannel` (`test/fake-output.js` gives one only
  for `{ log: true }`, as VS Code does) and no `--stdio` argument, which `pluto lsp` rejects.
  With fake `pluto` scripts in temporary directories, it checks each step of
  [Finding pluto](#finding-pluto) for both the server and the formatter: the setting wins (and
  a missing one is not replaced), `PATH` wins over the Go locations, then `$GOBIN`, `$GOPATH`,
  `~/go/bin` (a fake `HOME`), directories and non-executable files are passed over, the not
  found message lists the places searched, and a restart searches again.
- that `pluto lsp` and `vscode-jsonrpc` (the library under `vscode-languageclient`)
  agree on the protocol: handshake, diagnostics after an edit, hover, definition into the standard
  library, formatting and shutdown (`test/lsp-protocol-check.js`); and the extension starts and
  stops that `pluto lsp` through the installed `vscode-languageclient`, with the server's log at
  its own levels and a missing `pluto` reported once, and starts it from a fake `HOME`'s
  `go/bin` with the setting unset and nothing on `PATH` (`test/lsp-client-check.js`). Both are
  skipped without `npm ci`. `node test/lsp-client-check.js --search` leaves the setting unset and
  uses the real environment; run it with a `PATH` lacking `~/go/bin` (`env -u GOBIN -u GOPATH
  PATH=/usr/bin node test/lsp-client-check.js --search`) to check that the installed `pluto` is
  found the way an editor started from a desktop launcher would find it.

The node-based checks are skipped when `node` is not installed, and the checks that run `pluto`
are skipped when there is none. The language server's own unit tests went with the Go
implementation; `test/lsp-protocol-check.js` is what tests it now.
`node test/grammar-check.js dump file.p` prints the scopes of every token of a file.

Assertions in `test/sample.p` are comment lines under the line they check: carets mark columns,
`<---` marks columns from 0, and each scope listed must be a prefix of a scope of every token
marked (`!scope` must not be).

```go
func main() {
// <---- keyword.function
//   ^^^^ entity.name.function
```

## Origin

The grammar was written for Plutonium from scratch; it shares no code with the Go grammar that VS
Code bundles, so it carries no third-party licence notice.
