# Plutonium for Visual Studio Code

Syntax highlighting and `pluto fmt` formatting for [Plutonium](https://github.com/matjam/plutonium)
source files (`.p`). Works in VS Code and its forks (Cursor, VSCodium).

## Features

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

**Formatting** with `pluto fmt`: *Format Document* (and format on save, on by default for
Plutonium) sends the buffer to `pluto fmt` on standard input and replaces it with the output. If
`pluto fmt` fails, the buffer is left as it was, the error goes to the *Plutonium* output channel,
and a syntax error is shown as a diagnostic until the document is edited.

**Editing**: comment toggling, bracket matching, auto-closing of brackets, quotes and backticks,
Go-style indentation, and `// region` / `// endregion` folding markers.

## Settings

| Setting | Default | Meaning |
|---|---|---|
| `plutonium.plutoPath` | `pluto` | The compiler used for formatting. A bare name is looked up on `PATH`; `~` and `${workspaceFolder}` are expanded. |
| `plutonium.formatTimeout` | `10000` | Milliseconds to wait for `pluto fmt`. |

The extension sets `editor.insertSpaces: false` and `editor.formatOnSave: true` for Plutonium;
override them under `"[plutonium]"` in your settings. In an untrusted workspace `pluto` is never
run, and `plutonium.plutoPath` is taken only from user settings.

To get `pluto` on `PATH`, run `go install ./cmd/pluto` in the repository (it lands in
`$(go env GOPATH)/bin`), or point `plutonium.plutoPath` at a built binary.

## Install

The extension has no npm dependencies and no build step. To package and install it:

```sh
cd editors/vscode
scripts/package.sh                         # writes plutonium-<version>.vsix using zip
code --install-extension plutonium-0.1.0.vsix
cursor --install-extension plutonium-0.1.0.vsix   # or codium, code-insiders
```

`scripts/package.sh` builds the `.vsix` with `zip` alone; `npx @vscode/vsce package` makes an
equivalent one (honouring `.vscodeignore`). Reload the editor window (*Developer: Reload Window*)
after installing.

For development, run the editor with `code --extensionDevelopmentPath=$PWD/editors/vscode`, or
symlink this directory to `~/.vscode/extensions/matjam.plutonium-<version>`.

## Tests

`go test ./editors/vscode/` (part of the repository's `go test ./...`) checks that:

- `package.json`, the language configuration and the grammar parse, and refer to files that exist;
- every keyword of `internal/syntax/token` is highlighted, and every predeclared name of
  `internal/types2` (less the removed complex and map builtins) is highlighted as a builtin type,
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
- the formatter, run through `extension.js` against a stand-in for the `vscode` module and a pluto
  built for the test, formats, leaves formatted text alone, reports syntax errors as diagnostics
  without touching the buffer, and copes with a missing `pluto` (`test/format-check.js`).

The node-based checks are skipped when `node` is not installed.
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
