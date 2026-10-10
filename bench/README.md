# Cross-language benchmark suite

Sixteen small, classic compiler benchmarks, each written four times (C, Rust,
Go, Plutonium) with the same algorithm, so the Plutonium compiler can be
measured against GCC, LLVM (rustc) and the Go toolchain.

The Plutonium versions are **targets**: they are ordinary Go-style code that a
correct compiler for the language spec must accept and run. They were not
bent to fit the current compiler's limitations, and most of them fail today.

## Running

From the repository root:

```sh
go run ./bench/run.go                         # everything, 5 timed runs each
go run ./bench/run.go -langs c,rust,go        # skip Plutonium
go run ./bench/run.go -only fib,nbody -n 9    # a subset, 9 runs each
go run ./bench/run.go -pluto /tmp/pluto       # use an existing compiler binary
go run ./bench/run.go -json results.json      # also save machine-readable results
```

| Flag | Default | Meaning |
|---|---|---|
| `-n` | `5` | timed runs per binary; the median wall time is reported |
| `-only` | all | comma-separated benchmark names |
| `-langs` | `c,rust,go,pu` | comma-separated languages |
| `-pluto` | build `./cmd/pluto` | path to a `pluto` compiler binary |
| `-json` | none | write per-run timings, failures and toolchain versions to a file |
| `-cc` | `gcc` | C compiler (`clang` works too) |
| `-timeout` | `60s` | limit for a single run of a benchmark binary |
| `-gomaxprocs` | `1` | `GOMAXPROCS` for the Go binaries; `0` leaves Go's default |
| `-keep` | false | keep the temporary build directory and print its path |

For every benchmark and language the harness builds the program into a
temporary directory, runs it once and compares stdout byte-for-byte with
`expected.txt` (this run is also the warm-up), then times `-n` further runs.
A build error, crash, timeout or wrong output is reported in the table as
`FAIL (build|run|verify|timing)` with the details listed under the table, and
the run carries on. The table shows median seconds per language, the time of
each language relative to Rust, and the geometric mean of those ratios over the
benchmarks where both sides succeeded.

`bench/` is its own Go module, so `go build ./...` and `go vet ./...` at the
repository root do not see it. The per-benchmark `main.go` files are built by
the harness one at a time from a private module.

### Build commands

| Language | Command |
|---|---|
| C | `gcc -std=c11 -O2 -ffp-contract=off -march=x86-64 -o bin main.c -lm` |
| Rust | `rustc -C opt-level=3 --edition 2021 -o bin main.rs` |
| Go | `go build -o bin .` with `GOAMD64=v1` |
| Plutonium | `pluto build -o bin ./bench/<name>/pu/` (with `PLUTONIUM_ROOT` set to the repo root unless already set) |

## Fairness rules

- Same algorithm, same problem size, same evaluation order of floating-point
  operations in every language. Single-threaded. No SIMD intrinsics, no
  hand-unrolling, no `unsafe` in Rust (plain `Vec`, slices and arrays, with
  bounds checks), no `-ffast-math`.
- Baseline x86-64 everywhere: no `-march=native` / `target-cpu=native`;
  `GOAMD64=v1`. This is the instruction set Plutonium emits.
- No floating-point contraction: C uses `-ffp-contract=off`; rustc never
  contracts; Go only fuses on amd64 with `GOAMD64=v3` or higher.
- No input. Each problem size is a compile-time constant chosen so the Rust
  build runs for roughly 0.5 to 1.5 seconds on an AMD Ryzen 9 9900X3D.
- Output is integer lines only, so all four languages can be compared exactly.
  Floats are printed as `int64(x * 1e9)` (or `1e6`, `1e3`) with truncation;
  hashes are reduced with `% 1000000007` or printed as unsigned 32-bit values.
  Every C, Rust and Go build was checked to print byte-identical output, which
  is recorded in `expected.txt`.
- Wrap-around arithmetic is explicit: `uint64`/`uint32` in Go, C and
  Plutonium, `wrapping_mul`/`wrapping_add` in Rust.
- Float constants that are products (solar mass, velocities in n-body) are
  computed at run time from `float64` variables. Go evaluates constant
  expressions exactly, so a constant `4 * pi * pi` would round once instead of
  twice and could differ from C and Rust in the last bit.

### Known asymmetries

- **binarytrees memory management.** C calls `malloc`/`free` (glibc), Rust
  drops `Box` values (system allocator), Go relies on its garbage collector,
  and Plutonium allocates with `alloc.New[Node]()` and frees through the
  compiler-inserted ownership scheme in
  `docs/plans/2026-03-15-memory-lifecycle-design.md` (no explicit frees in the
  source). The benchmark measures each language's allocation strategy, not
  just code generation.
- **Go's collector runs on other cores** unless `GOMAXPROCS=1`, which is why the
  harness defaults to `-gomaxprocs 1`. On binarytrees this roughly halves
  Go's wall time when lifted (`-gomaxprocs 0`).
- **Data placement.** Go and Plutonium keep large arrays as package-level
  variables (`.bss`); C uses `static` arrays; Rust allocates them with
  `vec!` in `main`, since mutable statics need `unsafe`.
- Go and Rust check array bounds; C does not.

## The benchmarks

| Name | What it does | What it stresses |
|---|---|---|
| `fib` | naive recursive `fib(43)` | call/return overhead, recursion |
| `sieve` | sieve of Eratosthenes over 20 M `bool`s, 50 times | strided byte stores, branchy scans, memory bandwidth |
| `nbody` | Benchmarks Game n-body, 35 M steps | float64 arithmetic, `sqrt`, fields through pointers into an array of structs |
| `spectralnorm` | Benchmarks Game spectral-norm, N = 5500 | float division and multiply-add in nested loops, slice parameters |
| `mandelbrot` | Benchmarks Game mandelbrot, 5000x5000, packed bits hashed instead of written as PBM | scalar float loop with a data-dependent exit, bit packing |
| `fannkuch` | Benchmarks Game fannkuch-redux, n = 11 (checksum, max flips) | small array permutation and reversal |
| `binarytrees` | Benchmarks Game binary-trees, max depth 18 | allocation and deallocation throughput, pointer chasing |
| `matmul` | naive i-j-k float64 matrix multiply, 1200x1200 | strided loads, float add latency, bounds-check hoisting |
| `collatz` | longest Collatz chain below 10 M | signed `n%2`, `n/2` strength reduction, unpredictable branches |
| `heapsort` | heapsort of 5 M LCG-generated `int64`s | data-dependent branches, random access |
| `quicksort` | recursive Hoare quicksort of 10 M LCG-generated `int64`s | recursion, branch misprediction, sequential scans |
| `crc32` | table-driven CRC-32 (IEEE) over 16 MB, 36 passes | `uint32` shift/xor chains, table lookups |
| `sha256` | hand-written SHA-256 with padding over 1 MB, 350 chained passes | `uint32` rotates and adds under register pressure |
| `fasta-lcg` | LCG and cumulative-probability lookup from Benchmarks Game fasta, 80 M output bytes hashed | int mod, int-to-float, float compares, short search loop |
| `structs` | 1000 particles (spring plus magnetic field) for 500 k steps, using `Vec3` value methods | struct pass/return by value, inlining, scalar replacement |
| `strings` | wc-style scan of a 16 MB generated text, 35 passes: lines, words, longest word, words equal to `"go"`, per-word FNV-1a | byte indexing, compare chains, string indexing, struct return |

The LCG used by several benchmarks is
`seed = seed*6364136223846793005 + 1442695040888963407` (uint64, wrapping), with
`int64(seed >> 33)` as the output.

## Layout

```
bench/
  go.mod              separate module; keeps the suite out of the root module
  run.go              the harness
  README.md
  <name>/
    main.c
    main.rs
    main.go
    pu/main.p         Plutonium package
    expected.txt      the agreed output
```

## Plutonium language features the suite relies on

Every program uses `import "pluto/fmt"`, `fmt.Printf("%d\n", x)` with `int64`
arguments (including call results and conversions passed straight into the
variadic `...any`), `func main() int64` with `return 0`, untyped integer
constants, and `int64` arithmetic. Beyond that:

| Benchmark | Features |
|---|---|
| `fib` | plain recursion |
| `sieve` | 20 MB package-level `[N]bool`; an index expression as an `if` condition (`if flags[i] {`, not a composite literal) |
| `nbody` | `pluto/math.Sqrt` (not in the stdlib yet; expected to lower to `sqrtsd`); struct with grouped field names; keyed multi-line composite literals; `&bodies[i]` and field updates through the pointer; float `+=`/`-=`, unary minus; package `float64` vars with initialisers; `int64(float64)` truncation |
| `spectralnorm` | slicing package arrays (`u[:]`), `[]float64` parameters, `len`; int-to-float conversion; `pluto/math.Sqrt` |
| `mandelbrot` | `&&` with float compares in a `for` condition; `<<=`, `\|=`; shift by a signed variable count; `uint64` wrap and `%` |
| `fannkuch` | zero-initialised local arrays; `*int64` out-parameter; nested `for {}` with `break` and `return`; `count[r]--` |
| `binarytrees` | `alloc.New[Node]()`; pointer fields and `nil` compares; ownership moving through returns and into fields; per-iteration scope-exit frees; recursive release of child pointers |
| `matmul` | three 11.5 MB package arrays sized by a constant expression (`[N * N]float64`) |
| `collatz` | assigning to a parameter; signed `%`/`/` by 2 |
| `heapsort` | `var seed uint64 = 42`; large unsigned constants; `>>` on `uint64`; `int64(uint64)` |
| `quicksort` | deep recursion with two calls per frame |
| `crc32` | `[256]uint32`; untyped constants above `MaxInt32` into `uint32`; `byte(int64)` truncation; slicing a 16 MB array; `^=` |
| `sha256` | package array composite literal (`var k = [64]uint32{...}`) in initialised data; unary `^x`; shifts by a `uint32` variable; `data[:full]` and slicing a local array; zeroed local `var pad [128]byte` per call. Uses `bitLen`, not `bits`: `bits`, `be`, `le`, `packed`, `volatile`, `aligned` and `ring` are keywords |
| `fasta-lcg` | `[15]byte{'a', ...}` and `[15]float64{...}` package literals; untyped rune constant `'\n'` passed as a `byte` argument; `break` out of an inner loop |
| `structs` | value-receiver methods; method calls chained on call results; unkeyed `Vec3{a, b, c}`; 24- and 56-byte structs passed and returned by value; whole-struct array element assignment; nested `particles[i].Pos.X` |
| `strings` | `const Pattern = "go"`; `len` and indexing on a `string` parameter; `bool` locals and `!`; struct return by value with a zero-valued `var s Stats` |
