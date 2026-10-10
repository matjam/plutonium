# Plutonium Examples

Each directory is a complete program. They compile to static x86-64 Linux
binaries with no libc.

```bash
go build -o pluto ./cmd/pluto
./pluto run ./examples/07-hello
```

Each example's `expected` file holds its exact output (and, on a first line
`exit N`, its exit status); `go test ./internal/build -run TestExamples` checks
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
