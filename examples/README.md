# Plutonium Examples

Each directory is a complete program. They compile to static x86-64 Linux
binaries with no libc.

```bash
go build -o pluto ./cmd/pluto
./pluto run ./examples/07-hello
```

Each example's `expected` file holds its exact output (and, on a first line
`exit N`, its exit status); `go test ./tests -run TestExamples` checks
them all.

| Example | Shows |
|---|---|
| `01-exit` | the smallest program |
| `02-arithmetic` | calls and arithmetic; `main` returning an exit status |
| `03-control-flow` | loops and branches |
| `04-pointers` | pointers |
| `05-fibonacci` | an iterative loop |
| `06-generics` | a generic function |
| `07-hello` | `fmt.Println` |
| `08-printf` | `fmt.Printf` verbs, flags, widths, precision |
| `09-packed` | `packed` types: bit fields, flag groups, byte order, signed fields |
| `10-volatile-aligned-unsafe` | `volatile[T]`, `aligned[T, N]`, `unsafe` |
| `11-alloc` | explicit allocation with `pluto/alloc`, `new` and `make` |
| `12-ring` | `ring[T, N]` |
| `13-bits` | `pluto/bits` |
| `14-defer` | `defer`, `Drop` methods |
| `15-interfaces` | interfaces, type switches, errors |
| `16-control-flow` | `break`, `continue`, `switch`, `defer` |
| `17-literals` | literals and bitwise operators |
| `18-strings` | `%s` formatting |

## Memory, tasks and types

These show the features that set Plutonium apart from Go. Each has a comment
that says what the feature is and cites the section of the language
specification (`docs/spec/language/`) that defines it.

| Example | Shows | Spec |
|---|---|---|
| `19-owners` | owner types, `Drop`, moves, `new` returning `owned[T]`, a view that borrows | 7.18, 18.3, 18.5, 18.6 |
| `20-scoped-tasks` | `sync.Scope`: tasks that borrow the caller's variables, per-index writes, a lock the compiler infers | 19.3, 19.4 |
| `21-unions-enums` | `enum` and `union`, exhaustive switches, a generic union | 7.12, 7.13 |
| `22-maps` | maps as counted references: copies share one table, freed with the last copy | 7.10 |
| `23-counted-strings` | counted strings, sharing, `range`, slicing on rune boundaries | 7.3, 18.8 |
| `24-errors` | `errors.New` sentinels, `fmt.Errorf` with `%w`, `errors.Is` | 16.3, 16.5, 16.6 |
| `25-packed-hardware` | `packed` registers and headers, `bits`, big-endian storage | 7.14 |

Two errors are the first a new reader meets, and a program that has them does
not compile, so they are not examples. Their full messages are in
`tests/diag/`: `escape` (E3001, returning a pointer to a local
variable) and `races` (E3502, a package variable written by tasks with no
lock). `pluto explain E3001` and `pluto explain E3502` print the long form.
