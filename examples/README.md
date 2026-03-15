# Plutonium Examples

These examples compile to standalone x86_64 Linux binaries with no libc dependency.

## Building

```bash
# Build the compiler first
go build -o pluto ./cmd/pluto/

# Build any example
./pluto build -o <output> examples/<name>/main.pu

# Run it
./<output>
```

## Examples

| Example | Description | Output |
|---|---|---|
| `01-exit` | Empty main, clean exit | exit 0 |
| `02-arithmetic` | `add(20,1) * 2` — function calls + arithmetic | exit 42 |
| `03-control-flow` | `for` loop sum + `if/else` branch | exit 55 |
| `04-pointers` | `inc(&x)` — pointer dereference + store | exit 11 |
| `05-fibonacci` | Iterative Fibonacci(10) | exit 55 |
| `06-generics` | `Max[int64](42, 17)` — monomorphized generic | exit 42 |
| `07-hello` | Fibonacci sequence printed to stdout | stdout: 1 1 2 3 5 8 13 21 34 55 |

## How It Works

Each binary is:
- Statically linked, no shared libraries
- No libc, no POSIX — direct Linux syscalls via inline assembly stubs
- Entry point `_start` calls `main()`, passes return value to `SYS_EXIT_GROUP`
- `pluto/fmt.PrintInt` writes decimal digits to stdout via `SYS_WRITE`
