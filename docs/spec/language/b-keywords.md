## B. Keywords and contextual keywords

**Reserved keywords.** These 27 words are reserved and may not be used as identifiers (section 4.5,
Keywords).

| Keyword | Used for | Section |
|---|---|---|
| `break` | leaving a `for`, `switch` or `select` | 12.15 Break statements |
| `case` | a clause of `switch` and `select`; a variant of a packed union | 12.9 Switch statements; 12.13 Select statements; 7.13 Unions |
| `chan` | channel types | 7.11 Channel types |
| `const` | constant declarations | 10.6 Constant declarations |
| `continue` | starting the next iteration of a `for` | 12.16 Continue statements |
| `default` | the default clause of `switch` and `select` | 12.9; 12.13 |
| `defer` | deferred calls | 12.19 Defer statements |
| `else` | the alternative of an `if` | 12.8 If statements |
| `enum` | enum types | 7.12 Enums |
| `fallthrough` | continuing into the next `switch` clause | 12.18 Fallthrough statements |
| `for` | loops | 12.10 For statements |
| `func` | function types, function literals, function and method declarations | 7.8 Function types; 11.4 Function literals; 10.12 Function declarations |
| `go` | nothing; reserved so that `go f()` is an error that points to `task` | 12.12 Task statements |
| `goto` | jumps to a label | 12.17 Goto statements |
| `if` | conditionals | 12.8 If statements |
| `import` | import declarations | 14.3 Import declarations |
| `interface` | interface types | 7.9 Interface types |
| `map` | map types | 7.10 Map types |
| `package` | the package clause | 14.2 Package clause |
| `packed` | packed types and packed unions | 7.14 `packed` types; 7.13 Unions |
| `range` | iteration in `for` | 12.10 For statements; 12.11 Range over functions |
| `return` | leaving a function | 12.14 Return statements |
| `select` | waiting on several channel operations | 12.13 Select statements |
| `struct` | struct types | 7.6 Struct types |
| `switch` | multi-way branches | 12.9 Switch statements |
| `type` | type declarations | 10.8 Type declarations |
| `var` | variable declarations | 10.10 Variable declarations |

**Contextual keywords.** These words are identifiers, and have the meaning of a keyword only in the
position given (section 4.6, Contextual keywords). Everywhere else they name variables, fields, types,
functions and packages like any other identifier.

| Word | A keyword | Section |
|---|---|---|
| `owner` | before a type in a type definition, and before the constraint of a type parameter | 7.18 Owner types |
| `union` | as the type of a type definition when `{` follows it; after `packed` when something other than `{` follows it | 7.13 Unions |
| `task` | at the start of a statement when the next token is an identifier or `func` | 12.12 Task statements |
| `bits` | as the type of a field of a packed type, when `[` or `{` follows it | 7.14 `packed` types |
| `le`, `be` | directly after `packed` or after `packed union` | 7.14; 7.13 |

**Words that look like keywords and are not.** `true`, `false`, `iota` and `nil` are predeclared
identifiers (appendix D). `owned`, `shared`, `synced`, `weak`, `volatile`, `aligned` and `ring` are
predeclared type constructors (section 10.3, Predeclared identifiers). A `//pu:` comment is a directive,
not a word of the language (section 20.3, Directives).

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
