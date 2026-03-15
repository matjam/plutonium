# Plutonium Examples

These examples compile to standalone x86_64 Linux binaries with no libc dependency.

## Building

```bash
# Build the compiler first
go build -o pluto ./cmd/pluto/

# Build any example
./pluto build -o <output> examples/<name>/main.pu

# Run it — exit code is the result
./<output>; echo $?
```

## Examples

| Example | Description | Expected exit code |
|---|---|---|
| `01-exit` | Empty main, clean exit | 0 |
| `02-arithmetic` | Integer arithmetic with function calls | 42 |
| `03-control-flow` | If/else, for loops, sum computation | 55 |
| `04-pointers` | Pass-by-reference via pointer | 11 |
| `05-fibonacci` | Iterative Fibonacci(10) | 55 |
| `06-generics` | Generic-style max function | 42 |

## How It Works

Each binary is:
- Statically linked (no shared libraries)
- No libc, no POSIX — raw syscalls only
- Entry point `_start` calls `main()`, then calls `SYS_EXIT_GROUP` with main's return value
- The exit code IS the program's output (0-255 range due to Linux convention)

## Known Limitations

- Output is limited to exit codes (stdout writes via `pluto/os` are in progress)
- Negative number literals require workarounds (`int64(0) - int64(5)` has a codegen issue)
- Generic function instantiation via `Func[Type](args)` syntax has a name resolution bug
- No string output yet (string literals are interned but write() integration is pending)
