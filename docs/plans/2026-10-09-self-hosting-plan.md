# Self-hosting the Plutonium compiler (milestone M3)

**Date:** 2026-10-09
**Status:** plan and analysis, with a working prototype (`tools/go2pu`, `selfhost/`). Section 10 lists the
decisions for the user. Section 11 amends the plan for the ownership model (language spec section 6):
the Go sources migrate to a `pu` package that mirrors the Plutonium library, so translation is one to one
(see the Status block below for what the language specification now says).

> **Status (2026-10-10): partly implemented.** The prototype and the first phases of section 8 are done
> (the translated back end, growable arenas, the OS layer, the constant package and the `pu` mirror
> package). The front half of the compiler, the driver, the bootstrap check and the cutover are not
> started. The plan stays as the reasoning for the port. The language it assumes is now specified in full,
> and some of its premises changed (below); what the compiler and library still lack is in
> docs/roadmap.md.
>
> **Defined now by:** section 7.10 (Map types), section 13.1 (Built-ins Plutonium does not have),
> section 13.7 (Making channels and maps), section 13.9 (Allocation), section 11.24 (Conversions),
> section 7.3 (String types), section 18.8 (Strings), section 18.10 (No allocation without an owner or an
> arena), section 11.4 (Function literals), section 11.5 (Closures and their regions), section 18.2
> (Where variables live), section 18.3 (Views), section 16.2 (Where an error lives), section 16.3
> (Sentinels and `errors.New`), section 16.7 (Allocated errors and ownership), section 7.13 (Unions),
> section 7.18 (Owner types), section 12.12 (Task statements), section 19.1 (Tasks), appendix F.3
> (Iterators and containers).
>
> **The specification supersedes these statements in this document:**
> - "language spec section 6" (the ownership model), "section 6.3", "section 4.5", "section 4.2",
>   "section 14" and "section 14.6" (this document's header and sections 3.1, 3.3, 3.5, 7, 8 and 11):
>   these are the numbers of the earlier language.md. The ownership model is chapter 18 with sections 7.18
>   and 8.7; where literals and locals live is sections 18.2 and 18.3; closures are sections 11.4 and
>   11.5; `new` is section 13.9; errors are chapter 16, and counted errors are section 16.7.
> - "maps are `*hash.Map`", "no built-in map syntax" and the idea that map syntax over an allocator "hides
>   an allocator and a pointer" (sections 1, 3.1 and 7, gap 10): the language has the built-in `map[K]V`,
>   a counted reference with Go's semantics (reads from a nil map, `v, ok := m[k]`, `delete`, `clear`,
>   Go's iteration guarantees). `make(map[K]V, n, a)` takes an allocator. Neither keys nor values may be
>   owners; `hash.Map` is the library table for owners. Sections 7.10, 12.10, 13.3, 13.5, 13.7 and F.3.
>   The `compat.NewMap`, `MapAt` and `MapGet` rows of section 3.1 worked around a language that had no map
>   (go2pu now translates Go's maps unchanged, and the table has one row for them), and the single-result
>   lookup and nil-safe reads that gap 10 asks of `hash.Map` are properties of the built-in map.
> - "`pu.Map[K, V]` | `hash.Map[K, V]`" and "Map types become `pu.Map` values held by their owner"
>   (sections 11.1 and 11.2, step 1): a Go `map[K]V` has the Plutonium counterpart `map[K]V` (section
>   7.10). `hash.Map` remains for owners and for code that must avoid counting (appendix F.3). Whether the
>   mirror package keeps `pu.Map` is not decided by the specification.
> - "`append(s, x...)` | `compat.Append(s, x...)`", the 492 `append` calls, and gap 11 ("add
>   `alloc.Append(a, s, xs...)`", "an allocator argument to the built-in `append` would be ambiguous";
>   sections 2.3, 3.1, 7 and 10, decision 2): `append` is not a function of the language, and a slice never
>   grows (E2013). A sequence that grows is a `vec.Vec[T]` (`Push`, `Append`), and an arena hands out
>   slices with `a.Slice[T](n)`. A function that returns a slice grown from a freeing allocator would return
>   an array that no owner has, which section 18.10 rules out. Sections 13.1, 7.5, 13.7 and 18.10.
> - "`string(b)`, `[]byte(s)`, `string(r)` | `compat.String`, `compat.Bytes`, `compat.RuneString` |
>   copies" and the conversion counts of section 2.3: `string([]byte)`, `[]byte(s)` and `[]rune(s)` are
>   compile errors, because a Plutonium string always holds valid UTF-8 where a Go string holds any bytes.
>   The replacements are `strings.FromUTF8`, `strings.FromUTF8Lossy`, `strings.Bytes`, `strings.Runes` and
>   `copy(buf[:], s)`. `string(r)` stays. A string slice `s[i:j]` panics off a rune boundary, which Go
>   code may do. Sections 11.24, 7.3, 11.12, F.6 and A.5.
> - "`fmt.Sprintf` returns a `strings.Buf` and `Bprintf` formats into a buffer" (section 10, decision 12;
>   the introduction and table of section 11), `pu.Buf` | `strings.Buf` (section 11.1), "the library has
>   `Bprintf`" (gap 9): `fmt.Sprintf` returns a counted `string` (section 18.8). The growing text owner is
>   `strings.Builder` (section 18.10), and the specification names no `strings.Buf`. `Bprintf` and `Bprint`
>   are replaced by `fmt.Appendf`, `Append` and `Appendln`, which write into a `vec.Vec[byte]` or a
>   `vec.Fixed[byte]` (sections 13.1 and F.3).
> - "`errors.New` takes one of 512 static slots, meant for package-level sentinels", and the three run-time
>   calls that "become `compat.NewError`" (sections 2.4, 3.1 and 3.5): `errors.New` works anywhere. Each
>   call site is one static error, so there are no slots to exhaust. The text must be a constant (E2008);
>   for any other text the error points to `fmt.Errorf`. Section 16.3.
> - "`pluto/fmt.Errorf` allocates the error and its message from the default allocator", "the proposal of
>   section 14.6 if it is signed off" (sections 3.5 and 11.3): the proposal is adopted. An allocated error
>   is a counted object that `error` values count and release; there is no `errors.Free`; in an arena the
>   free does nothing. Sections 16.7 and 18.9.
> - "Returning `&T{...}` today compiles and returns a dangling pointer", "nothing reports it yet (roadmap
>   item 5)" and "implement roadmap item 5 ... before any hand-written Plutonium is merged" (sections 2.5,
>   7 gap 1, 8 and 9.4, decision 6): the language rejects every view of a frame that outlives it
>   (E3001 to E3006), and it never moves a variable to the heap because its address is taken (sections 18.2
>   and 18.3). Roadmap item 5 is done.
> - "The idiomatic allocating form is `new(T{...})`" (gap 1) and "under the proposal ... `new` returns an
>   `owned[T]`" (section 11.2): `new(T{...})` returns an `owned[T]`, not a `*T`, and does not store in a
>   `*T` field. Views of arena memory come from `a.New[T]()`. Section 13.9.
> - "keep Plutonium's rule (closures do not outlive their frame)", "Plutonium's rule only forbids returning
>   or storing", the returned closures and iterators listed in section 3.6, and "iterator constructors that
>   return closures capturing their parameters are expanded in the caller (language spec section 4.5)"
>   (sections 3.6 and 8, gap 3): a function may return a closure. It is built in a region of the caller's
>   frame, lives as long as that frame and costs no allocation. Storing a closure in a package variable or
>   through a pointer is E3002, and what a region cannot hold is E3100 to E3102. A closure record is not
>   "expanded in the caller". Conversion to a struct and an interface (section 5.4) is still needed for
>   closures that the program stores, such as the `check.later` queue. Section 11.5.
> - "Sum types (language, proposed)", "accept option A", "Today, without sum types" and "with the proposed
>   union" (gap 13, section 5.3, decision 7): unions are decided and specified, section 7.13.
> - "The compiler does not use puroutines", "puroutines have fixed stacks" and "Items 14 to 17
>   (puroutines, channels, ...)" (sections 6.2, 7 gap 12 and 8): the word is task, started by
>   `task f(x)`. A task has a fixed stack; a hosted task whose size cannot be computed gets at least 64 MiB
>   of address space, committed as it is touched. The main thread's stack is set with `//pu:stack`.
>   Sections 12.12, 19.1 and 20.3.

The compiler is 56,548 lines of Go. The goal is a compiler written in Plutonium that compiles itself, can
be built and maintained on PlutoniumOS, and produces byte-identical output when it rebuilds itself. This
document measures what stands in the way, proposes how to port, and reports how far a prototype got.

## 1. Summary

- **Translate, do not rewrite.** A mechanical Go-to-Plutonium translator (`tools/go2pu`) plus a small
  compatibility package (`selfhost/compat`, 380 lines) that gives translated code Go's semantics on top
  of an arena: maps are `*hash.Map`, `append` grows from the current allocator, string concatenation and
  conversions copy, `Sprintf` returns a new string. Nothing is freed individually;
  arenas reclaim whole phases, which is what Go's collector does for this program.
- **Go's own escape analysis is the oracle** for the one semantic difference a translator cannot see
  syntactically: in Plutonium `&T{...}`, slice literals and address-taken locals live in the frame. The
  translator allocates exactly the ones `go build -gcflags='-m -l'` moves to the heap.
- **The prototype works on the whole back half of the compiler.** `internal/ssa`, `internal/opt`,
  `internal/arch/amd64` and `internal/objfile` (9,356 lines, 17.6% of the compile path) translate with one
  hand-written file (`opt/magic.go`, which used `math/big`) and produce byte-identical SSA dumps, machine
  code, listings and ELF objects to the Go build at `-O0`, `-O1` and `-O2` (`TestDifferential` checks
  `-O0` and `-O2` on every test run).
- **The hard part is the front half:** `types2` (26,772 lines) needs a new constant-arithmetic package in
  place of `go/constant` and `math/big`, closure conversion for its 14 deferred actions, and stubs for
  `sync`; the driver needs an OS layer (files, directories, processes, paths) the standard library lacks.
- **Ranked prerequisites:** the compiler must reject frame addresses that escape (roadmap item 5) before
  anyone writes Plutonium by hand; then a constant package, the OS layer, growable arenas, determinism
  fixes, and an itab lookup that is not a linear scan.
- **Bootstrap:** stage 0 (Go) builds stage 1, stage 1 builds stage 2, stage 2 builds stage 3, and stage 2
  and stage 3 must be byte-identical. The Go implementation stays the source of truth until cutover;
  after it, release N is built by release N-1, with no binaries checked into the repository.
- The prototype found three wrong-code bugs in the current compiler (one fixed here, one fixed on `main`
  while this plan was written) and two places where the Go-hosted compiler's output depends on map
  iteration order.

## 2. Inventory

### 2.1 Method

Every number below is measured, by tooling kept out of the build except where noted. The inventory
(sections 2.2 to 2.7) is a snapshot of `main` at 636c276; `main` has since grown to 57,624 lines (`lower`
+773 for System V classification of `//pu:extern`, `amd64` +142 for interrupt handlers, `types2` +93), and
go2pu translated the grown back end with no new hand work (section 9).

- an inventory program (scratch, `go/packages` and `go/types`) that walks the typed ASTs of `internal/...`
  and `cmd/...` and counts the constructs Plutonium lacks or changes, and every standard library name used;
- Go's escape analysis, `go build -a -gcflags='-m -l' ./internal/... ./cmd/...`, for which composite
  literals, closures and variables outlive their frame;
- `tools/go2pu` itself, run over every package, for what it cannot translate (section 9.3);
- a phase-by-phase allocation measurement compiled into `internal/build` through `go test -overlay`,
  over every example and every `testdata/run` and `testdata/lib` program (section 2.6);
- `golang.org/x/tools/cmd/deadcode` from `cmd/pluto` (46 unreachable functions in `types2`, 34 in
  `syntax/ast`: little to delete before porting).

### 2.2 Lines per package

| Package | Role | Lines | Test lines | Functions | Needed to compile |
|---|---|---:|---:|---:|---|
| `internal/syntax/token` | tokens, positions, file sets | 1,389 | 0 | 69 | yes |
| `internal/syntax/scanner` | scanner | 1,121 | 0 | 41 | yes |
| `internal/syntax/ast` | AST, walk, comment maps | 2,627 | 0 | 221 | yes |
| `internal/syntax/parser` | parser | 3,240 | 205 | 101 | yes |
| `internal/types2` | type checker (forked go/types) | 25,090 | 323 | 848 | yes |
| `internal/types2/errors` | error codes (mostly generated) | 1,682 | 0 | 2 | yes |
| `internal/load` | package loading, build tags | 504 | 0 | 26 | yes |
| `internal/lower` | typed AST to SSA | 5,766 | 121 | 236 | yes |
| `internal/ssa` | SSA IR, verifier, basic passes | 1,911 | 0 | 93 | yes |
| `internal/opt` | optimiser | 3,241 | 81 | 87 | yes |
| `internal/arch/amd64` | isel, regalloc, encoder | 3,756 | 55 | 149 | yes |
| `internal/objfile` | ELF writer | 297 | 0 | 8 | yes |
| `internal/build` | driver | 502 | 806 | 15 | yes |
| `cmd/pluto` | command line | 888 | 397 | 30 | yes |
| `internal/syntax/printer` | formatter | 3,935 | 1,017 | 102 | for `pluto fmt` |
| `internal/syntax/format` | formatter | 336 | 517 | 7 | for `pluto fmt` |
| `internal/diff` | diffs for `pluto fmt -d` | 263 | 39 | 3 | for `pluto fmt` |
| **Total** | | **56,548** | **3,561** | **2,038** | 52,014 on the compile path |

### 2.3 Go features Plutonium lacks or changes, per package

Columns: map types and map operations; map keys by kind (pointer or integer / string / interface /
struct or array); `range` over a map; `append` calls (of them, into a struct field); runtime string `+`
and `+=`; `string(b)` and `[]byte(s)`; `fmt.Sprintf` and friends / `fmt.Errorf` and `errors.New`;
function literals (capturing / escaping per Go); `&T{...}` that escape per Go; slice literals that escape;
variables Go moves to the heap; conversions of values larger than a word to interfaces (escaping);
functions on a recursive call cycle within the package; `panic` / `recover`; `sync` uses.

| Package | maps | keys p/s/i/st | range | append (field) | str + | conv b→s/s→b | Sprintf / Errorf | closures (capt / esc) | `&T{}` esc | `[]T{}` esc | heap vars | iface boxes (esc) | recursive | panic / recover | sync |
|---|---|---|---:|---|---:|---|---|---|---:|---:|---:|---|---:|---|---:|
| token | 2 / 5 | 0/2/0/0 | 0 | 3 (2) | 5 | 0/0 | 11 / 0 | 3 (2 / 2) | 3 | 1 | 0 | 20 (0) | 1 | 17 / 0 | 36 |
| scanner | 0 / 0 | - | 0 | 1 (0) | 7 | 11/1 | 4 / 0 | 0 | 1 | 0 | 0 | 4 (1) | 0 | 1 / 0 | 0 |
| ast | 4 / 21 | 1/0/3/0 | 2 | 16 (1) | 0 | 1/1 | 2 / 2 | 10 (6 / 7) | 1 | 1 | 10 | 18 (0) | 3 | 4 / 1 | 0 |
| parser | 5 / 4 | 5/0/0/0 | 0 | 34 (2) | 24 | 0/2 | 3 / 1 | 6 (6 / 1) | 121 | 8 | 3 | 21 (0) | 59 | 9 / 2 | 0 |
| types2 | 114 / 313 | 53/23/34/4 | 32 | 89 (19) | 46 | 1/0 | 28 / 9 | 123 (109 / 42) | 138 | 23 | 100 | 428 (7) | 194 | 116 / 3 | 21 |
| load | 14 / 23 | 4/7/3/0 | 1 | 11 (6) | 4 | 2/0 | 0 / 20 | 4 (3 / 3) | 5 | 0 | 2 | 35 (3) | 1 | 0 / 0 | 0 |
| lower | 32 / 131 | 19/13/0/0 | 4 | 91 (49) | 28 | 0/2 | 9 / 2 | 49 (43 / 21) | 37 | 29 | 4 | 25 (1) | 53 | 4 / 0 | 0 |
| ssa | 12 / 46 | 10/2/0/0 | 3 | 34 (13) | 3 | 0/0 | 8 / 1 | 11 (10 / 0) | 10 | 0 | 2 | 22 (0) | 2 | 4 / 0 | 0 |
| opt | 39 / 129 | 38/1/0/0 | 4 | 44 (17) | 12 | 0/0 | 1 / 2 | 16 (15 / 0) | 1 | 2 | 4 | 7 (0) | 7 | 4 / 0 | 0 |
| amd64 | 60 / 192 | 56/0/0/4 | 7 | 83 (20) | 3 | 0/0 | 25 / 6 | 14 (12 / 0) | 128 | 44 | 2 | 30 (0) | 3 | 4 / 0 | 0 |
| objfile | 3 / 11 | 0/3/0/0 | 0 | 13 (6) | 1 | 0/0 | 0 / 1 | 3 (3 / 0) | 3 | 0 | 3 | 6 (0) | 0 | 0 / 0 | 0 |
| build | 2 / 10 | 0/2/0/0 | 0 | 15 (4) | 1 | 0/0 | 1 / 7 | 4 (4 / 0) | 6 | 0 | 3 | 24 (0) | 0 | 0 / 0 | 0 |
| cmd/pluto | 2 / 3 | 2/0/0/0 | 0 | 10 (6) | 7 | 0/0 | 2 / 6 | 12 (7 / 1) | 3 | 0 | 1 | 29 (0) | 2 | 0 / 0 | 0 |
| printer, format, diff | 5 / 12 | 0/1/4/0 | 0 | 48 (17) | 25 | 4/3 | 2 / 3 | 9 (6 / 1) | 14 | 2 | 8 | 33 (0) | 38 | 7 / 0 | 2 |
| **Total** | **294 / 900** | **188/54/44/8** | **53** | **492 (162)** | **168** | **19/9** | **96 / 60** | **264 (226 / 78)** | **471** | **110** | **142** | **702 (12)** | **363** | **170 / 6** | **59** |

Not in the table because the count is zero or trivial: goroutines and channels (none), `reflect` (one
file, `syntax/ast/print.go`, a debugging printer), `defer` in a loop (none; 137 `defer`s in all),
complex numbers (one `complex` call in `types2`), range over a function (12 loops: 10 in `types2`, five
of them in `hash.go`, and 2 in `syntax/ast`, over iterators the packages define), `[]rune` conversions
(none), string `min`/`max` (none), comparison of arrays (none).

Of the 702 conversions of large values to interfaces, 176 are arguments to `fmt` functions and most of
the rest are arguments to the type checker's variadic error helpers (`errorf`, `addf`), which format them
before returning; only 12 store the box somewhere that outlives the frame (section 7, gap 4).

Map operations break down as 297 reads, 247 writes, 62 comma-ok reads, 24 deletes, 53 ranges, 26 `len`,
72 `make` and 118 literals.

### 2.4 Standard library use, and what Plutonium has today

"Plutonium today" counts the distinct names used that exist under the same path in `lib/pluto`; the
inventory checks names, not semantics, so the column also says where the semantics differ.

| Go package | Used by | Names used (most frequent) | Plutonium today | Plan (section 4) |
|---|---|---|---|---|
| `fmt` | 16 packages, 259 uses | `Sprintf` 96, `Fprintf` 65, `Errorf` 55, `Println` 17 | `pluto/fmt`: 7 of 8; `Errorf` with `%w` (spec section 14); no `Sprintf` (it has `Bprintf` into a buffer); `%v` of structs prints the type name | compat `Sprintf` over `Fprintf`; `Errorf` as is |
| `strings` | 13 packages, 236 uses | `Builder` and its methods 100, `HasPrefix` 31, `CutPrefix` 10, `Join` 8 | `pluto/strings`: 28 of 37; no `Join`, `Repeat`, `ReplaceAll`, `Split`, `Fields`, `ToLower` (it has `Append*` and `*Into` forms); `Builder` needs an allocator | add allocator forms; compat `Builder` |
| `bytes` | 9 packages, 211 uses | `Buffer` methods (`WriteByte` 59, `WriteString` 58) | `pluto/bytes.Buffer` exists but has fixed capacity | compat `Buffer` (grows) |
| `go/constant` | types2 198, lower 31 | `Value`, `Kind`, `MakeInt64`, `Int64Val`, `ToInt`, `BinaryOp`, `Compare`, `Shift`, `MakeFromLiteral` (34 names) | none | new `constant` package (section 5.3) |
| `math/big` | opt 20 | `Int` (`Lsh`, `Div`, `SetBit`) in `magic.go` only | none | done by hand in the prototype (192-bit integer) |
| `debug/elf` | build 35, objfile 47 | 39 constants and the `*64` structs | none | done: `selfhost/elf`, 140 lines |
| `encoding/binary` | amd64 6, lower 12, objfile 8 | `LittleEndian.PutUint64/32`, `Write` (reflection) | none | done: compat `PutUint*`; go2pu generates `Write` encoders |
| `sort` | 9 packages, 19 uses | `Slice` 6, `Strings` 4, `SliceStable` 3, `Sort` 3 | `pluto/sort`: element-based `SortFunc` (same pdqsort) | done: go2pu rewrites `sort.Slice` |
| `slices`, `cmp` | 5 packages, 35 uses | `SortFunc` 9, `Contains` 5, `Index` 5, `cmp.Compare` 10 | none (`pluto/sort` covers sorting) | small library package |
| `strconv` | 7 packages, 23 uses | `Quote` 8, `Unquote` 5, `Itoa` 4, `FormatInt` 4, `QuotedPrefix` | `pluto/strconv`: `ParseUint` only of these (it has `AppendInt`, `AppendFloat`) | port `quote.go`; allocator forms |
| `unicode` | 6 packages, 22 uses | `IsSpace` 7, `IsDigit` 3, `IsLetter` 3, `IsGraphic`, `IsUpper` | none (`pluto/unicode/utf8` only) | port the predicates with generated tables |
| `unicode/utf8` | 6 packages | `DecodeRuneInString`, `RuneSelf` | complete | none |
| `os` | 8 packages, 60 uses | `Stderr`, `ReadFile`, `Stat`, `MkdirTemp`, `RemoveAll`, `DirFS`, `WriteFile`, `ReadDir`, `Pipe` | `pluto/os`: 9 of 22 (files, `Exit`, `Getenv`, `Arg`) | OS layer (section 4.2) |
| `os/exec` | cmd/pluto 12, build 4 | `Command`, `Run`, `Start`, `Wait`, `ExitError` (runs `as`, `ld` and test binaries) | none | OS layer |
| `io/fs`, `path`, `path/filepath` | cmd, build, load, scanner | `DirEntry`, `FS`, `ReadDir`, `WalkDir`, `Join`, `Dir`, `Base`, `Rel`, `Abs` | none | OS layer |
| `io` | 10 packages | `Writer` 22, `ReadAll` 2 | `pluto/io`: `Writer`, `Reader` | add `ReadAll` |
| `errors` | 5 packages | `New` 5, `Join` 3, `Is` 1 | `pluto/errors`: `New` (static slots, for sentinels), `Is`, `As`, `Unwrap`, `Join` | as is; `errors.New` at run time (3 sites) becomes `compat.NewError` |
| `sync`, `sync/atomic` | token 39, types2 25, printer 3 | `Mutex`, `RWMutex`, `Once`, `Pool`; atomics | `pluto/sync/atomic` only (`pluto/sync` is roadmap item 16) | no-op stubs (the compiler is single-threaded) |
| `hash/maphash`, `hash/fnv` | types2 58, lower 2 | `maphash.Hash` (`WriteByte`, `WriteString`, `Sum64`), `fnv.New32a` | `pluto/hash` has the functions, no streaming hasher | add a streaming hasher |
| `iter` | ast, token, types2 | `Seq` (13) | `pluto/iter` (roadmap item 22, done) | import `pluto/iter` |
| `go/build/constraint` | load, parser, printer | `Parse`, `Expr.Eval` | none | translate with go2pu (696 lines) |
| `container/heap` | types2 3 | `Init`, `Pop`, `Fix` | none | 100-line generic heap |
| `go/token`, `go/version` | types2 | `Token` (operators for `go/constant`), `Compare`, `Lang` | none | disappears with the constant package; `version` by hand |
| `flag`, `time`, `bufio`, `regexp` | cmd/pluto, build, load | flag set parsing; `Now`/`Since`; `Scanner`; `-run` patterns | none (`time` is roadmap item 45) | hand-written driver pieces |
| `text/tabwriter`, `go/doc/comment` | printer | alignment, doc comment reformatting | none | only for `pluto fmt`; translate later |
| `reflect` | ast `print.go` | `ValueOf` and friends (43 uses) | none, by design | drop the debug printer |
| `runtime` | types2 1 | `Caller` in `assert` | none | drop |
| `github.com/spf13/cobra` | cmd/pluto | command tree | none | hand-written argument parser |

The compiler also embeds the standard library with `//go:embed` (`stdlib.go`). Plutonium has no embedding;
the self-hosted compiler reads the library from `PLUTOROOT` (already supported) and an installation
places `lib/` next to the binary.

### 2.5 What Go's escape analysis says

Escape analysis (with inlining off, so allocations are reported where they are written) decides what
Plutonium must allocate explicitly. Totals over the compiler:

| Construct | Escapes (must allocate) | Stays in frame |
|---|---:|---:|
| `&T{...}` | 471 | 9 |
| slice literal `[]T{...}` | 110 | (not counted) |
| variable whose address outlives its frame ("moved to heap") | 142 | |
| function literal | 78 | 186 |

In Plutonium all of these live in the frame, and nothing reports it yet (roadmap item 5). Returning
`&T{...}` today compiles and returns a dangling pointer; it happens to work at `-O2` when the callee is
inlined and fails at `-O0` (section 9.4). This is the largest semantic gap between the two languages for
this program, and it is invisible in the source.

### 2.6 Allocation volume

Measured with Go over the 52 programs of `examples/`, `testdata/run` and `testdata/lib` (103,750 lines
of input counting the standard library each one loads), `-O2`:

| Phase | Allocated | Share | Per input line |
|---|---:|---:|---:|
| load (parse and type-check) | 160 MiB | 9.5% | 1.6 KiB |
| lower | 124 MiB | 7.4% | 1.2 KiB |
| optimise | 482 MiB | 28.8% | 4.8 KiB |
| code generation | 892 MiB | 53.3% | 8.8 KiB |
| write the object | 15 MiB | 0.9% | 0.2 KiB |
| **total** | **1.63 GiB** | | **16.5 KiB** |

The largest single function's code generation allocated 37 MiB; the live heap after a whole compilation
of a 4,300-line program was at most 8.6 MiB. Allocation is two orders of magnitude above what is live, so
an arena per compilation that frees nothing would need about 1.1 GiB for the compiler's own 70,000 lines
(compiler plus the library it imports). Section 3.4 brings the high-water mark down by freeing code
generation per function and optimiser scratch per pass.

### 2.7 Determinism today

Byte-identical stage 2 and stage 3 need a deterministic compiler. Of the 53 `range` loops over maps, the
ones on the compile path were read; two make the Go-hosted compiler's output depend on Go's randomised map
order (a task has been filed for both):

- `internal/arch/amd64/regalloc.go`, in the spill rewrite: the spill stores of an instruction that defines
  several spilled registers are emitted in map order;
- `internal/opt/mem2reg.go`: zero constants for unreachable phi inputs are created (new value IDs,
  prepended to the entry block) in map order.

The others (`isel.go`, the rest of `regalloc.go`, `lower/iface.go`, `opt/slots.go`, `opt/tailrec.go`,
`ssa/program.go`) build sets, copy maps or sort first. `ssa.Verify` lists errors in map order, which only
affects diagnostics. Object files carry no timestamps, symbol tables are emitted in definition order with
data sorted by name. Positions reach the output through the trace table and DWARF (roadmap items 41 and
42), which name files relative to the module root (`./cmd/tool/main.p`) and the standard library
(`pluto/fmt/print.p`), with `.` as DWARF's compilation directory ([debugging.md section
2.1](../spec/debugging.md#21-file-names)). (Amended 2026-10-09: the two map-order bugs above are fixed,
and the same source now links to a byte-identical executable from any directory, checked by
`TestDeterministic` and `TestReproducible` in `internal/build/determinism_test.go`.)

## 3. Translation strategy

### 3.1 Go semantics on arenas first, idiomatic Plutonium later

The port has two phases with different goals. While Go is the source of truth, the translation must be
mechanical, repeatable after every change, and behave exactly like the Go code; style does not matter.
After cutover, the Plutonium source is edited by hand and should become idiomatic.

Go's semantics need a garbage collector only because objects are never freed explicitly. A compiler is
the textbook case where that is unnecessary: almost everything lives until the end of a phase. So the
translation keeps Go's semantics and replaces the collector with arenas: every allocation comes from
`alloc.Default()`, which the driver points at the arena of the current phase with `alloc.Use`, and nothing
is ever freed individually. `selfhost/compat` holds the few functions that need this:

| Go | Translation | Notes |
|---|---|---|
| `map[K]V` and every map operation | unchanged | Plutonium's `map[K]V` is a counted reference with Go's semantics (language spec section 7.10); until 2026-10-10 maps went to `*hash.Map` through `compat.NewMap`, `MapAt` and friends |
| `append(s, x...)` | `compat.Append(s, x...)` | grows from `alloc.Default()` beyond capacity; never frees the old array (it may be shared) |
| `append(b, s...)` (string) | `compat.AppendString(b, s)` | |
| `s + t`, `s += t` | `compat.Concat(s, t)` | chains flattened into one call |
| `string(b)`, `[]byte(s)`, `string(r)` | `compat.String`, `compat.Bytes`, `compat.RuneString` | copies |
| `fmt.Sprintf` | `compat.Sprintf` | a new string from `alloc.Default()` |
| `fmt.Errorf`, `errors.Is`, `As`, `Unwrap`, `Join` | unchanged | `pluto/fmt` and `pluto/errors` follow Go (spec section 14) |
| `errors.New` in a function body | `compat.NewError` | `errors.New` takes a static slot, meant for package-level sentinels |
| `bytes.Buffer`, `strings.Builder` | `compat.Buffer` | zero value grows from `alloc.Default()` |
| `strings.Join` | `compat.Join` | |
| `sort.Slice(x, func(i, j int) bool { ... x[i] ... x[j] ... })` | `sort.SortFunc(x, func(a, b T) bool { ... a ... b ... })` | same pdqsort as Go, so ties come out in the same order; verified |
| `binary.Write(w, binary.LittleEndian, &x)` | `goWriteLE<T>(w, &x)` | generated field-by-field encoder |
| `binary.LittleEndian.PutUint32(b, v)` | `compat.PutUint32(b, v)` | |
| `&T{...}` that escapes | `pu.New(T{...})` | per Go's escape analysis (3.3); `selfhost/pu.New` copies the value into the phase arena (the current allocator). go2pu emitted `new(T{...})` until `new` was to return `owned[T]` (section 11) |
| `[]T{...}` that escapes | `compat.Clone([]T{...})` | per Go's escape analysis |
| `x` moved to heap | `x := pu.New(e)` (or `pu.Alloc[T]()` without a value); uses `(*x)`; `&x` is `x` | parameters copied in at the top of the body |
| `debug/elf`, `internal/...` imports | `selfhost/elf`, `selfhost/internal/...` | |

An example, from `internal/objfile` (the Go source, then go2pu's output, unedited):

```go
func (o *Object) Define(sym *Symbol) {
	if old, ok := o.syms[sym.Name]; ok {
		*old = *sym
		return
	}
	o.syms[sym.Name] = sym
	o.order = append(o.order, sym.Name)
}

	names := append([]string(nil), o.order...)
	sort.SliceStable(names, func(i, j int) bool {
		return o.syms[names[i]].Bind == elf.STB_LOCAL && o.syms[names[j]].Bind != elf.STB_LOCAL
	})
```

```go
func (o *Object) Define(sym *Symbol) {
	if old, ok := compat.MapGet(o.syms, sym.Name); ok {
		*old = *sym
		return
	}
	o.syms.Set(sym.Name, sym)
	o.order = compat.Append(o.order, sym.Name)
}

	names := compat.Append([]string(nil), o.order...)
	sort.SortStableFunc(names, func(a, b string) bool {
		return compat.MapAt(o.syms, a).Bind == elf.STB_LOCAL && compat.MapAt(o.syms, b).Bind != elf.STB_LOCAL
	})
```

### 3.2 How go2pu works

`tools/go2pu` (2,100 lines of Go including tests) type-checks a package with `go/types`, runs Go's escape
analysis on it, and rewrites the source text: each node that needs a change is replaced by text built from
its translated children, and everything else (comments, layout, names) is copied, so the output reads like
the input and diffs against it line by line. The result is formatted with `go/format` (Plutonium source is
Go syntax here) and gets a `// Code generated by go2pu from <file>. DO NOT EDIT.` header.

Constructs it cannot translate are reported with a position and a reason ("closure escapes its frame
(passed to a function that keeps it)", "recover: Plutonium's panic does not unwind", "pluto/strconv has no
Quote", ...). Section 9.3 counts them per package.

A `.p` file in the output **without** the generated header is hand-written: go2pu does not overwrite it and
does not translate the Go file of the same name. This is how a file that needs real porting work
(`opt/magic.go` in the prototype) coexists with a package that is otherwise regenerated on every change.

### 3.3 Go's escape analysis as the oracle

Plutonium puts composite literals, their addresses and every local in the frame (language spec section 6.3);
Go moves to the heap whatever escape analysis cannot prove stays in the frame. go2pu runs
`go build -gcflags='-m -l'` on each package and allocates exactly what Go moves:

- `&T{...}` reported as escaping becomes `pu.New(T{...})`, which allocates from the current allocator
  (the phase arena);
- a slice literal reported as escaping becomes `compat.Clone(...)`;
- a variable reported as "moved to heap" becomes a pointer to an allocation;
- a capturing function literal reported as escaping is flagged for hand-fixing (section 3.6).

Inlining must be off (`-l`): with inlining, an allocation inside an inlined function or closure is
reported at each call site, and the literal itself would stay in the frame. The prototype hit exactly this
in `isel.go` before the flag was added. Go's analysis is conservative (an address passed through an
interface counts as escaping), so the translation allocates a little more than it must; that is safe, and
the extra allocations are arena bumps.

### 3.4 Arena structure

Memory is the main cost of the arena model (section 2.6). The compiler's phases have clear lifetimes, so
the plan uses three arena scopes, each a chunked bump allocator (a gap: `pluto/arena` today is one fixed
buffer):

1. **Program arena**, for the whole compilation: source text, ASTs, `types2` objects and `Info`, the SSA
   program (the inliner reads other functions' bodies, so every function's SSA lives until optimisation
   ends), symbols, and the object being written.
2. **Pass scratch**, marked before and released after each function pass of the optimiser. The values and
   blocks a pass creates belong to the function, so `ssa.Func` gets an explicit allocator field and
   `newValue`, `NewBlock`, `NewSlot` and the appends to `Values`, `Args`, `Preds` and `Succs` use it
   (about a dozen sites in `internal/ssa`, hand-fixed once); the pass's own maps, worklists and sets go to
   scratch through `alloc.Default()`.
3. **Function code generation arena**, reset after each function: `amd64.Prepare` and `Compile` allocate
   the machine IR, the interference graph and the encoder's buffers there; the driver copies the code
   bytes, relocations and constant pool entries into the object (program arena) before the reset. Once a
   function is compiled its SSA is dead too, so with per-function SSA allocators (item 2) the function's
   memory goes back as well.

Code generation is 53% of all allocation, and most of the optimiser's 29% is pass scratch (an estimate:
passes keep only the values they create), so the high-water mark drops from about 1.1 GiB to the front
end's retained data plus one function's back end: an estimated 200 to 350 MiB for compiling the compiler.
Objects that cross from a short scope into a longer one (a constant-pool symbol made during code
generation, a name built by `Sprintf` and stored in the program) must be allocated from the longer-lived
arena explicitly. The debug allocator of roadmap item 43 (poisoning released memory) turns every such
mistake the tests reach into a crash, so the differential tests (section 6.3) run with it.

### 3.5 Errors

The errors design (`docs/plans/2026-10-09-errors-design.md`, language spec section 14) landed on `main`
while this plan was written, and the port follows it: an error that leaves a function is a word-sized
value or a pointer to static, owner or allocator storage. For the compiler that means:

- `fmt.Errorf` (55 calls, 10 with `%w`) is used as it is: `pluto/fmt.Errorf` allocates the error and its
  message from the default allocator, which during compilation is the phase's arena, and wraps with `%w`
  like Go. `errors.Is`, `As`, `Unwrap` and `Join` are used as they are.
- `errors.New` takes one of 512 static slots, meant for package-level sentinels; the compiler's two
  sentinels (`build.ErrNoTestFiles`, `errFormattingDiffers`) stay, and the three calls made at run time
  (`types2/instantiate.go`, `parser/interface.go`, `cmd/pluto/fmt.go`) become `compat.NewError`, which
  allocates. The `types2` one runs once per failed instantiation and would exhaust the slots.
- Three error values are not pointers and are returned or stored as interfaces, which spec section 14
  forbids: `lower.errorList` and `scanner.ErrorList` (slices) and `types2.Error` (a struct passed to a
  callback that keeps it). They become pointers by hand.

Errors live in the arena of the phase that made them, so the driver reports them before releasing that
arena; it already prints them as soon as a phase fails.

### 3.6 What must be fixed by hand

From go2pu's report over every package (section 9.3), the hand work is:

- **Escaping closures**, 78 per Go. About half are false alarms in Plutonium (a closure converted to an
  interface and passed to `ast.Inspect`, which Go cannot prove does not keep it; Plutonium's rule only
  forbids returning or storing). The real ones: 14 `check.later(func() {...})` actions in `types2`, 7
  `funcJob.build` closures queued in `lower`, 2 closure factories in `lower/intrinsic.go`, 12 iterators in
  `types2/iter.go` and `token`, the `Config.Error` and `Importer` callbacks in `load`, and a few returned
  closures (`types2/typestring.go`, `under.go`, `ast/walk.go`). Section 5.4 shows the conversion; go2pu can
  automate the `later` pattern.
- **`go/constant`** call sites (127 in `types2`, 29 in `lower`), which change package but mostly keep their
  shape if the new constant package keeps `go/constant`'s function names.
- **`recover`** in the parser's and the type checker's bailout (5 sites, plus one in the AST debug printer,
  which is dropped): the parser stops by pretending the input ended; the type checker's recover only adds
  context to internal panics and is dropped.
- **OS, process and command-line code** in `load`, `build` and `cmd/pluto` (about 120 notes), written
  against the new OS layer.
- Small rewrites: 12 range-over-function loops become index loops, `sync` becomes no-op stubs, `maphash`
  becomes the streaming hasher, `reflect` (the AST debug printer) is dropped, `runtime.Caller` is dropped,
  three variables declared together with one moved to heap are split.

### 3.7 Testing a translation

Every translated package is checked by running the same program built both ways and comparing output
(section 6.3). The prototype's demos (`tools/go2pu/testdata/*demo`) do this for the back end; the plan
extends it to every phase of the real compiler.

## 4. Dependencies

### 4.1 Per package

| Dependency | Decision | Size | Notes |
|---|---|---:|---|
| `go/constant` + `math/big` | **rewrite** as a constant package | 2,500-3,500 lines | section 5.3; `math/big` (10,219 lines, assembly, `sync.Pool`, `fmt.Formatter`) is too large and too Go-shaped to port for one client |
| `math/big` in `opt/magic.go` | **rewritten** (prototype) | 100 lines | a 192-bit integer and long division |
| `strconv` (`Quote`, `Unquote`, `QuotedPrefix`, `Itoa`, `FormatInt`, `ParseFloat` for constants) | **port** with go2pu, then add allocator forms | 600 lines + `ParseFloat` 900 | float parsing must round exactly like Go's (Go 1.27 uses fast unrounded scaling, not Eisel-Lemire, with the multiprecision slow-path fallback; ported and differential-tested) |
| `sort` | **replace** with `pluto/sort` | 0 | done by go2pu |
| `slices`, `cmp` | **replace** with a small `pluto/slices` | 200 lines | `SortFunc` with a three-way comparison, `Contains`, `Index`, `BinarySearchFunc`, `EqualFunc` |
| `encoding/binary` | **replace** | 0 | done: generated encoders and `compat.PutUint*` |
| `debug/elf` | **replace** with a hand-written subset | 140 lines | done (`selfhost/elf`) |
| `debug/dwarf` | not used yet | | DWARF output (roadmap item 42) is written natively when it comes |
| `unicode` | **port** the predicates used, with generated range tables | 1,200 lines | Go's package is 10,816 lines, nearly all tables; only `IsLetter`, `IsDigit`, `IsSpace`, `IsUpper`, `IsLower`, `IsGraphic` are used |
| `hash/maphash`, `hash/fnv` | **replace** with a streaming hasher in `pluto/hash` | 80 lines | |
| `container/heap` | **replace** with a generic heap | 100 lines | |
| `go/build/constraint` | **port** with go2pu | 700 lines | `//pu:build` expressions |
| `go/token`, `go/version` | **replace** | 50 lines | `go/token` only names operators for `go/constant` |
| `sync`, `sync/atomic` | **stub** (no-op `Mutex`, `Once`) | 50 lines | the compiler is single-threaded; `pluto/sync` (roadmap item 16) replaces the stubs later |
| `os`, `os/exec`, `io/fs`, `path`, `path/filepath`, `bufio`, `time`, `flag` | **write** an OS layer | 2,000-2,500 lines | section 4.2 |
| `regexp` | **avoid** | 0-500 lines | used only for `pluto test -run`; anchored literal patterns and `*` cover the uses, or a small backtracking matcher |
| `text/tabwriter`, `go/doc/comment` | **port** with go2pu, after the compile path | 600 + 1,500 lines | formatter only |
| `reflect`, `runtime` | **drop** | 0 | |
| `cobra`, `pflag` | **replace** with a hand-written parser | 300 lines | |

About 10,000 lines of library work in all, half of it mechanical translation.

### 4.2 The OS layer

The driver and loader need: read a file, write a file, list a directory, stat, create and remove a
temporary directory, rename, chmod, run a process with redirected standard streams and wait for its exit
status, environment variables, arguments, a monotonic clock for test timings, and path manipulation. On
Linux these are system calls `pluto/syscall` can already make; the library work is the API above them
(`pluto/os` additions, `pluto/os/exec`, `pluto/path/filepath`, `pluto/time` from roadmap item 45). On
PlutoniumOS the same API sits on the kernel's system calls, which is part of the platform work in phase 9.

The compiler also runs two external programs, `as` (for the standard library's five `.s` files, 413
lines) and `ld`. They exist on Linux; PlutoniumOS has neither, so self-hosting **on** PlutoniumOS also
needs an assembler for the subset of GNU syntax those files use (reusing the backend's x86-64 encoder,
about 1,500 lines) and a static ELF linker (about 2,000 lines), or the five assembly files rewritten with
intrinsics. That is phase 9, not a prerequisite for self-hosting on Linux.

## 5. The type checker

### 5.1 Shape

`internal/types2` is 25,090 lines (plus 1,682 of error codes) in 848 functions, 194 of which lie on
recursive call cycles. It is upstream `go/types` with Plutonium's additions (`plutonium.go`,
`plutonium_types.go`: `volatile`, `aligned`, `ring`, `packed`, `owned`/`shared`/`synced`, allocator
arguments). Everything Go-specific about it shows up at once:

- **Maps everywhere**: 114 map types, 313 operations, 32 ranges. 34 maps are keyed by interfaces
  (`Object`, `Type`, `ast.Expr`, `ast.Node`: the `Info` maps, `objMap`, the many `seen map[Type]bool`
  sets), 53 by pointers, 23 by strings, 4 by structs (`importKey`, `dotImportKey`, ...).
- **Closures**: 123 literals, 109 capturing, 42 escaping per Go; the deferred-action queue
  (`check.later`) is the design that matters.
- **Interfaces**: `Type`, `Object` and `ast.Node` implementations are all pointers, so they are direct in
  Plutonium; `constant.Value` is the exception (its implementations are small structs).
- **`go/constant`** (198 references) for every untyped constant computation.
- **`sync`** (21 uses): locks for lazy `Named` type expansion and the instantiation `Context`.

### 5.2 Approach

Translate it with go2pu like everything else, plus three pieces of real work:

1. a constant package to replace `go/constant` (5.3);
2. closure conversion for `check.later` (5.4);
3. about 300 to 500 lines of hand edits: drop `iter.go` and rewrite the ten loops that range over its
   iterators (five in `hash.go`) as index loops, adapt `hash.go` to the streaming hasher, make `posSpan`
   (a struct stored as a `positioner` interface) a pointer, remove `handleBailout`'s `recover`, stub
   `sync`, replace `go/token` operator names and `go/version`, split three declarations.

Interface-keyed maps need nothing: `compat.NewMap` sees that the key type is an interface (its zero value
converts to a nil `any`) and hashes keys by their dynamic value, which for every `Type`, `Object` and
`Node` is a pointer. The one map keyed by constant values (`valueMap map[any][]valueType`, duplicate
`case` detection) changes with the constant representation.

Two alternatives were considered and rejected. Rewriting the type checker in idiomatic Plutonium would
throw away the property that makes the fork maintainable, that it can take upstream `go/types` fixes, and
would re-derive inference and unification from scratch. Keeping `types2` in Go behind an interface (a Go
type checker feeding a Plutonium back end) defeats the goal: the system could not build its own compiler.

### 5.3 A constant package

`go/constant` cannot be ported mechanically. Its `Value` is an interface whose implementations are
structs (`intVal{*big.Int}`, `ratVal`, `floatVal`), so in Plutonium every `Value` that is not an `int64`
would be boxed in the frame that made it, and `BinaryOp` returns one. It also drags in `math/big`.

The replacement keeps `go/constant`'s function names (so most call sites change only their import) and
changes the representation to a value type:

```go
// Today, without sum types: a kind and a payload.
type Value struct {
	kind Kind   // Unknown, Bool, String, Int, Float
	i    int64  // Bool and small Int
	s    string // String
	big  *Big   // large Int or Float, allocated from the current arena
}

// With the proposed union (docs/plans/2026-10-09-sum-types-proposal.md):
type Value union {
	Unknown
	Bool   bool
	String string
	Int    Int   // int64 fast path, else arena-allocated digits
	Float  Float // 512-bit mantissa, as go/constant's big.Float precision
}
```

Integers are sign and magnitude over `[]uint64` from the arena (add, subtract, multiply, quotient and
remainder, shifts, bitwise operations, comparison: about 900 lines). Floats are a binary mantissa of 512
bits and an exponent, correctly rounded, with exact conversion to `float64`/`float32` and decimal parsing
for `MakeFromLiteral` (about 1,500 lines). Complex constants are dropped (Plutonium has no complex types).
Its tests are go/constant's own test tables, run differentially against Go.

### 5.4 Closure conversion for deferred actions

```go
// internal/types2/decl.go
var rhs Type
check.later(func() {
	if t := asNamed(obj.typ); t != nil { // type may be invalid
		check.validType(t)
	}
	_ = !versionErr && check.isImportedConstraint(rhs) && check.verifyVersionf(tdecl.Type, go1_18, "using type constraint %s", rhs)
}).describef(obj, "validType(%s)", obj.Name())
```

The closure runs after `typeDecl` returns and reads `rhs`, which `typeDecl` assigns afterwards. Go's escape
analysis already moves `rhs` and `versionErr` to the heap, so after go2pu's heap-variable rewrite they are
pointers. Closure conversion turns the literal into a struct holding the captured variables (pointers for
those moved to the heap, values for the rest) and a method:

```go
type goLater1 struct {
	check      *Checker
	obj        *TypeName
	tdecl      *ast.TypeSpec
	versionErr *bool
	rhs        *Type
}

func (c *goLater1) Call() {
	if t := asNamed(c.obj.typ); t != nil {
		c.check.validType(t)
	}
	_ = !*c.versionErr && c.check.isImportedConstraint(*c.rhs) && c.check.verifyVersionf(c.tdecl.Type, go1_18, "using type constraint %s", *c.rhs)
}

// in typeDecl, where the heap-variable rewrite has made versionErr and rhs pointers:
versionErr := new(false)
rhs := new(Type)
check.later(new(goLater1{check, obj, tdecl, versionErr, rhs})).describef(...)
```

with `action.f func()` becoming `action.f compat.Func0` (`interface{ Call() }`), and the call `a.f()`
becoming `a.f.Call()`. A pointer in an interface is direct, so the action can be stored and run later.
go2pu can do this mechanically when every value flowing into the parameter is a function literal or a
non-capturing function (which `compat.Fn0(f)` adapts); that holds for `later`, `funcJob.build` in `lower`
and the intrinsic table. It is the next translator feature after the prototype.

## 6. Bootstrap plan

### 6.1 Stages

| Stage | Built by | From | Runs on |
|---|---|---|---|
| 0 | the Go toolchain | the Go sources | host |
| 1 | stage 0 | the Plutonium sources (go2pu's output plus hand-written files) | host |
| 2 | stage 1 | the Plutonium sources | host |
| 3 | stage 2 | the Plutonium sources | host |

Stage 1 and stage 2 are different binaries (built by different compilers) that must behave identically;
stage 2 and stage 3 are built by compilers with identical behaviour from identical source, so they must be
byte-identical. `make bootstrap` (or a `pluto` subcommand) runs all four and compares stage 2 with stage 3;
CI runs it on every change to the compiler once stage 1 exists.

### 6.2 Determinism requirements

- **Map iteration.** `hash.Map` iterates in table order, fixed for a given sequence of insertions and
  hashes. Pointer keys hash their addresses, and addresses differ between runs (ASLR places the arena at a
  different base), so any output that depends on iterating a pointer-keyed map differs between runs.
  Every such loop is a bug in the Go compiler too, where map order is random; the two found (section 2.7)
  must be fixed first, and new ones are caught by building twice in CI. As a second line of defence the
  compiler can reserve its arenas at a fixed virtual address so that pointer hashes repeat.
- **Hash seeds** stay fixed (`hash.DefaultSeed`); the compiler never randomises.
- **Sorting.** `pluto/sort` uses the same pdqsort and stable merge as Go, so ties resolve identically
  (verified by the `rewrites` demo); sorts should still use total orders.
- **Floating-point text.** Constant parsing and printing must round exactly as Go's `strconv` does, or
  stage 0 and stage 1 disagree on constant values; this is why `ParseFloat` is ported, not rewritten.
- **No timestamps, no absolute paths** in output. Object files have no timestamps. The trace table and
  DWARF record paths relative to the module root or the standard library, and DWARF's compilation
  directory is `.` ([debugging.md section 2.1](../spec/debugging.md#21-file-names)); `--trimpath=false`
  restores absolute paths and is never used for bootstrap stages. The driver also runs the assembler and
  C compiler in the work directory on relative names, so the temporary directory's name never reaches an
  object, and passes `--build-id=none` to `ld`, so the result does not depend on how the system linker
  was configured. Stage 2 and stage 3 are then comparable even when built in different directories or on
  different machines, provided `as` and `ld` are the same versions (the compiler's own output does not
  depend on them, only the linked file).
- **Single-threaded.** The compiler does not use puroutines; if it ever compiles packages in parallel,
  results are merged in a fixed order.

### 6.3 Keeping two implementations in step

During the port Go is the source of truth. Developers change the Go code; the Plutonium code is
regenerated, never edited, except the hand-written files.

- **Regeneration is checked.** `TestTranslationsUpToDate` (in the prototype) re-runs go2pu over every
  translated package and fails if `selfhost/` differs. Next, each hand-written file should record the
  hash of the Go file it replaces, so that the check fails when that Go file changes until the port is
  updated (not implemented in the prototype).
- **Differential tests per phase.** The Plutonium build of each phase must produce the same output as the
  Go build on the whole test corpus (`testdata/run`, `testdata/lib`, `examples`, the standard library's
  own tests): parse then print; type-check then dump `Info` sorted by position; lower then dump SSA
  (`--ssa=all`); after each optimiser pass; `-S` listings; object bytes. The prototype's demos are small
  instances of this.
- **Mixed pipelines through text.** Go and Plutonium code cannot share a process, so phases are joined
  through files: the SSA printer exists, and an SSA reader (about 400 lines) lets the Go front end feed the
  Plutonium back end. The back end, translated first, then runs on real programs long before the front end
  is ported, and each later phase joins the self-hosted part of the pipeline as soon as it passes.
- **Package order**: back end (done), syntax, `lower`, `types2`, `load` and the driver (section 8).

### 6.4 Cutover and the first binary on a new system

At cutover the Go implementation is frozen and tagged; from then on the Plutonium source is edited by hand
and may use new language features.

- **Bootstrap chain**: release N is built by release N-1, as Go does since 1.5. The frozen Go
  implementation can always build the cutover release, so a system with a Go toolchain can rebuild any
  later release by walking the chain; the chain is tested in CI periodically.
- **No binary seeds in the repository.** Release binaries for each target are published as artifacts with
  their hashes; a checked-in seed would be an unauditable blob and would grow the repository with every
  release.
- **PlutoniumOS** gets its first compiler by **cross-compilation**: the host's stage 2 builds the compiler
  for the PlutoniumOS target, and the result is installed on the system. Rebuilding on PlutoniumOS with
  that binary must reproduce the cross-built bytes (the same compiler from the same source for the same
  target); that comparison is the trust check that a seed binary could never give.
- Self-hosting on PlutoniumOS also needs the PlutoniumOS target (system call layer for the OS package),
  the integrated assembler and linker (section 4.2), and `PLUTOROOT` installation of the library.

## 7. Language and library gaps, ranked

Ranked by how much of the port each one blocks, weighted by how much hand-written Plutonium code will hit
it after cutover.

**1. Frame addresses that escape compile silently (language/compiler, wrong code).**
Size: 471 escaping `&T{...}`, 110 slice literals and 142 address-taken locals in the compiler alone; every
Go programmer writes `return &T{...}`. Today it compiles and returns a pointer into a dead frame; it often
works at `-O2` (inlining) and fails at `-O0`. Recommendation: implement roadmap item 5 for composite
literals, slice literals and `&local` (return, store through a pointer, store in a global), before any
hand-written Plutonium is merged; the errors design proposes the same check for `return &x` (its section
9.2). The idiomatic allocating form is `new(T{...})` (Go 1.26's `new` of a value, fixed on `main` while
this plan was written). go2pu does not need the check (it allocates by Go's analysis), but everything
written after cutover does.

**2. Constant arithmetic: `go/constant` and `math/big` (library).**
Size: 229 call sites; 2,500-3,500 lines of new library. Blocks `types2` and `lower`. Recommendation: a new
value-typed constant package (section 5.3), with go/constant's API names and test tables. Not `math/big`.

**3. Escaping closures (language decision, library and translator work).**
Size: about 40 real sites (78 flagged by Go); the type checker's action queue is the one that matters.
Recommendation: keep Plutonium's rule (closures do not outlive their frame), convert the escaping ones to
a struct and an interface (section 5.4) with go2pu doing the common shapes. A language feature for
allocated closures (a closure literal whose captured variables are copied into an allocation) is
possible later; nothing in the port needs it.

**4. Large values in interfaces, especially returned errors (language, small).**
Size: 12 boxes that escape in the compiler (`lower.errorList`, `scanner.ErrorList`, `types2.Error`,
`posSpan`, `load`'s directory types), plus the whole of `go/constant`'s design. Recommendation: adopt Go's
rule that a struct or array whose only field is pointer-shaped is direct (stored in the data word): it is
a small change to `isDirect` in the compiler and the runtime's descriptor flags, and it removes most
boxing hazards in practice (`intVal{*big.Int}`-style wrappers, handles, single-field structs). Beyond that,
errors are returned as pointers, which is already the library's convention (`errors.Text`).

**5. Growable arenas with mark and release (library).**
Size: about 200 lines. Without them, compiling the compiler needs about 1.1 GiB; with the three scopes of
section 3.4, 200 to 350 MiB. Recommendation: add a chunked arena (`pluto/arena.Chunked` or a growable
mode of `Arena`) taking chunks from a page provider, with `Mark`/`Release`/`Reset` and a poisoning debug
mode.

**6. OS and process layer (library).**
Size: 2,000-2,500 lines. Blocks `load`, `build`, `cmd/pluto`. Recommendation: extend `pluto/os` (read and
write whole files, directories, stat, temporary directories, rename, remove), add `pluto/os/exec`,
`pluto/path/filepath`, `pluto/time` (roadmap item 45) and a flag parser; read the library from `PLUTOROOT`
instead of embedding it.

**7. Determinism (compiler bugs, small).**
Size: two loops (section 2.7). Blocks the stage 2 = stage 3 check. Recommendation: fix now (task filed),
and build twice in CI from then on.

**8. Interface conversion performance (runtime).**
`rt.findItab` scans every itab in the program linearly; it backs `convI2I` (converting between non-empty
interfaces) and assertions to interface types. The type checker converts `Object` and `Type` values to
`positioner` and other interfaces constantly, and the self-hosted compiler will have thousands of itabs.
Size: about 100 lines (a hash table keyed by the two descriptor pointers, or a per-call-site cache).
Recommendation: fix before measuring the self-hosted compiler's speed.

**9. Strings and formatting with allocators (library ergonomics).**
Size: 168 concatenations, 96 `Sprintf`, 60 error constructors in the compiler. The compat functions
cover the port. After cutover the idiom needs: a zero `strings.Builder` that takes the default allocator
on first growth (today it panics unless built with `NewBuilderIn` or `Grow`), `fmt.Sprintf(a, ...)`
returning a string from an allocator (the spec lists `Sprintf`; the library has `Bprintf`, roadmap item
47), and allocator forms of `Repeat`, `ReplaceAll`, `Split`, `Fields`, `Join`, `ToLower`. Recommendation:
library additions, no language change.

**10. Map ergonomics (library).**
Size: 294 map types, 900 operations. `hash.Map` is fast (section 7, gap 14) and complete enough; what
hand-written code will want is: a single-result lookup (`m.At(k)` returning the zero value), reads through
a nil `*Map` (`Len`, `Get`, `Has`, iteration), default hashing for interface keys whose dynamic values
are pointer-shaped, and for structs and arrays of hashable fields once type descriptors describe fields
(roadmap item 23). Recommendation: library additions; no built-in map syntax. Map syntax over an allocator
(`map[K]V` as sugar for `*hash.Map[K, V]` with `make(map[K]V, n, a)`) is possible but hides an allocator
and a pointer behind familiar syntax, against the language's first goals; revisit after cutover if the
method syntax proves painful.

**11. Growing slices (library).**
Size: 492 `append` calls, 162 into struct fields. `compat.Append` (grow from `alloc.Default()`, never
free the old array) is right for arena code and wrong for code using a freeing allocator.
Recommendation: add `alloc.Append(a, s, xs...)` with that contract stated, and use `vec.Vec` in long-lived
structures after cutover. No language change: an allocator argument to the built-in `append` would be
ambiguous for `[]any` and `[]Allocator`.

**12. Deep recursion and the main thread's stack (runtime).**
Size: 363 functions on recursive cycles (types2 194, parser 59, lower 53, printer 38). Go grows goroutine
stacks to 1 GiB; Plutonium's main thread has what the OS gives it (8 MiB by default on Linux) and
puroutines have fixed stacks. The parser limits nesting depth, which bounds the type checker's
recursion over expressions; nothing bounds its recursion over types (instantiation chains, embedded
types). Recommendation: run the compiler on a large explicitly reserved stack (for example 256 MiB,
mapped lazily with a guard page, through an `rt` entry option), and add a depth limit with a proper error
to the type checker's recursion over types.

**13. Sum types (language, proposed).**
Not required for the port, but the constant package, `Result`-style returns and AST node kinds are the
use cases the proposal names. Recommendation: accept option A of the sum-types proposal; the constant
package is written so that switching its `Value` to a union is local.

**14. Hash map performance (no gap).**
Measured: inserting 100,000 keys and looking each up four times, `hash.Map` takes 5.9-7.0 ms with pointer
keys and 10.3-11.2 ms with string keys; Go's map (Swiss tables) takes 8.6-9.9 ms and 10.5-12.9 ms. No
change needed.

**15. Missing small names (library).**
`math.MaxInt32` was the only name the x86-64 backend needed that the library lacked (added here with the
rest of Go's integer limits). The others go2pu reports: `strconv` `Quote`/`Unquote`/`Itoa`/`FormatInt`,
`strings` `Repeat`/`ReplaceAll`/`Split`/`Fields`/`ToLower`, `io.ReadAll`, `os` file system functions,
all covered by gaps 6 and 9 (`errors.Join` arrived with the errors design).

## 8. Effort and phasing

Sizes are in lines written or changed, not in time.

| Phase | Work | Size | Kind | Risk | Needs first | Status |
|---|---|---:|---|---|---|---|
| 0 | Escape errors for frame addresses (roadmap item 5); determinism fixes; direct single-pointer structs; itab lookup | 1,000-1,500 compiler | compiler | medium | - | direct single-pointer structs, hashed itab lookup, `//pu:stack` and the type checker's nesting limit done (roadmap item 56) |
| 1 | Library: growable arenas, OS layer, `strconv` quoting and `ParseFloat`, `unicode` predicates, slices/heap/streaming hash, strings with allocators | 6,000 | library, half mechanical | low | - | growable arenas (`arena.Chunked`), the OS layer (`os`, `os/exec`, `path/filepath`, `time`, `flag`) and the `pu` mirror package with its go2pu mapping (section 11) done (roadmap items 52 and 56); `strconv.ParseFloat`, `unicode`, `cmp`, `slices` and `container/heap` done (roadmap item 56; Go 1.27's float parsing is fast unrounded scaling, not Eisel-Lemire, and the port follows it); `strconv` quoting, the streaming hash, strings with allocators and migrating the compiler to `pu` remain |
| 2 | Back end: `ssa`, `opt`, `amd64`, `objfile` (**done** in the prototype, differential at `-O0`/`-O2`); port their Go tests; per-function arenas in `ssa` | 9,356 translated, ~300 by hand | mechanical | low | arenas | prototype done |
| 3 | SSA reader; mixed pipeline (Go front end, Plutonium back end) on the whole test corpus | 400 | hand | low | 2 | not started |
| 4 | Syntax: `token`, `scanner`, `ast`, `parser` (`recover` in the parser, `FileSet` without `sync`, iterators, drop `ast/print.go`) | 8,377 translated, ~400 by hand | mechanical | low | 1 | not started |
| 5 | `lower` (closure conversion for `funcJob`, intrinsics table, constants) | 5,766 translated, ~400 by hand | mechanical plus conversion | medium | constant package, go2pu closure conversion | not started |
| 6 | Constant package | 2,500-3,500 | hand | medium | - | done in Go (`internal/constant`, 2,800 lines, differential against go/constant; roadmap item 56); its translation waits for `strconv` quoting |
| 7 | `types2` (constants, `later` conversion, sync stubs, iterators, hashing) | 26,772 translated, 300-500 by hand | mechanical plus conversion | high (size, recursion depth, performance) | 5, 6, itab lookup | not started |
| 8 | `load`, `build`, `cmd/pluto`; stage 1, 2, 3 on Linux; bootstrap check in CI; cutover | 1,900 by hand | hand | medium | 1, 7 | not started |
| 9 | PlutoniumOS: target and OS layer, integrated assembler and linker, cross-built seed, on-system rebuild | 5,000+ | hand | high | 8, kernel system calls | not started |
| later | Formatter (`printer`, `format`, `diff`, `tabwriter`, `go/doc/comment`); idiomatic cleanup replacing compat; performance | 4,534 translated + 2,100 library | mixed | low | 8 | not started |

Roadmap items that must land first: item 5 (escape errors) before any hand-written compiler code; item 11
(the errors design) has landed and the port follows it; item 43 (debug allocator) to check
arena lifetimes; item 45 (`time`) for the driver. Items 14 to 17 (puroutines, channels, `pluto/sync`,
thread-safe allocator) are not needed: the compiler stays single-threaded. Item 22 (range over functions)
has landed, with `pluto/iter`; iterator constructors that return closures capturing their parameters are
expanded in the caller (language spec section 4.5), so the twelve loops can stay as they are, unless an
iterator keeps `yield` or is used as a func value. Item 25 (sum types) helps but is not required.

The order puts the measured, mechanical part first (it is done), then the pieces that need design (the
constant package, closure conversion), then the large mechanical part they unblock (`types2`), then the
hand-written driver, so that by the time the driver is written every phase it calls has passed its
differential tests.

## 9. The prototype

### 9.1 What exists

- `tools/go2pu`: the translator (Go, 2,100 lines with tests). `TestTranslationsUpToDate` checks that
  `selfhost/` is exactly go2pu's current output; `TestDifferential` builds each demo with Go and its
  translation with the Plutonium compiler at `-O0` and `-O2` and compares their output; `TestCompat` runs
  the compat package's own Plutonium tests; `TestHandFixNotes` checks that every unsupported construct is
  reported.
- `selfhost/` (a Plutonium module, `module selfhost`): `compat` (380 lines and tests), `elf` (the
  `debug/elf` subset), `internal/syntax/token/pos.p` (a stub of `token.Pos` until `token` is translated),
  and the translations of `internal/objfile`, `internal/ssa`, `internal/opt` and `internal/arch/amd64`
  (9,689 lines of Plutonium, 685 references to compat: 182 `Append`, 123 `MapAt`, 72 `NewMap`, 46
  `Clone`, 35 `Sprintf`, and 159 `new(...)` allocations for what Go's escape analysis moves to the heap),
  with `opt/magic.p` written by hand.
- Demos under `tools/go2pu/testdata`, translated into `selfhost/demo`: `objdemo` (writes an ELF object
  and prints it in hex), `ssademo` (builds SSA, prints dominators, frontiers, loops and the passes'
  results), `amd64demo` (compiles SSA to machine code and prints bytes, relocations and listings),
  `optdemo` (runs the `-O2` pipeline, including magic-number division, then the backend), and `rewrites`
  (one of every rewrite, including map tie-order and sort-tie behaviour).

### 9.2 Results

All five demos print byte-identical output from the Go build and the Plutonium build at `-O0`, `-O1` and
`-O2`. That is the compiler's pipeline from SSA through optimisation and code generation to object file
bytes, translated with one hand-written file, compiled by the current Go-hosted Plutonium compiler, and
computing the same results as the Go code.

### 9.3 Where the rest of the compiler stands

go2pu's notes for every package, translated into a scratch directory:

| Package | Notes | Escaping closures | No Plutonium package | Name missing in `pluto/` | `recover` | Range over func | Other |
|---|---:|---:|---:|---:|---:|---:|---:|
| `syntax/token` | 19 | 2 | 15 (sync, slices, iter) | 2 | 0 | 0 | 0 |
| `syntax/scanner` | 11 | 0 | 10 (path/filepath, unicode) | 0 | 0 | 0 | 1 |
| `syntax/ast` | 52 | 5 | 39 (reflect, slices, cmp, unicode) | 5 | 1 | 2 | 0 |
| `syntax/parser` | 8 | 1 | 3 (go/build/constraint) | 2 | 2 | 0 | 0 |
| `types2` | 344 | 29 | 279 (go/constant 127, slices 17, maphash 17, iter 11, unicode 7, ...) | 15 | 3 | 10 | 8 |
| `load` | 56 | 3 | 41 (io/fs, path, filepath, bufio) | 10 | 0 | 0 | 2 |
| `lower` | 50 | 16 | 33 (go/constant 29, hash/fnv) | 0 | 0 | 0 | 1 |
| `opt` | 0 | 0 | 0 (with the hand-written `magic.p`) | 0 | 0 | 0 | 0 |
| `arch/amd64`, `ssa`, `objfile` | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| `build` | 26 | 0 | 14 (os/exec, regexp, filepath) | 12 | 0 | 0 | 0 |
| `cmd/pluto` | 88 | 1 | 70 (cobra, flag, os/exec, time) | 17 | 0 | 0 | 0 |

### 9.4 Bugs the prototype found in the current compiler

1. **`[]T(nil)` lowered as a one-word nil** (fixed here, with a `testdata/run` test). As an argument it
   shifted every later argument: `append([]string(nil), xs...)` got a garbage length, and inlining such a
   call indexed past the callee's parameters and crashed the compiler.
2. **Frame addresses escape silently** (roadmap item 5): `return &T{...}` and returned slice literals
   dangle. Found as wrong output at `-O0` and a missing result type at `-O2`.
3. **`new(expr)` ignored its operand**: `new(T{a: 1})` returned a zeroed `T`. Fixed on `main` (3217ccc)
   while this plan was written; go2pu now emits `new(T{...})`, and the differential tests pass with it.
4. **Output depends on Go's map order** in two places (section 2.7). A task is filed.

Merging `main` midway (System V classification in `lower`, interrupt handlers in `amd64`, new SSA fields)
was the first test of the sync workflow of section 6.3: the translations were regenerated, the grown
back end needed no hand work, and the differential tests passed unchanged.

## 10. Decisions for the user

1. **Translation-based port**, with Go as the source of truth until cutover and go2pu's output
   regenerated on every change (recommended), versus a hand rewrite.
2. **Where compat lives**: private to the compiler (`selfhost/compat`, recommended), with its idiomatic
   pieces promoted into the standard library (`alloc.Append`, `Map.At` and nil-safe reads, a zero
   `Builder` that grows, `fmt.Sprintf` with an allocator).
3. **Constants**: a new value-typed constant package (recommended) versus porting `math/big` and
   `go/constant`.
4. **Escaping closures**: convert them (by go2pu where mechanical, by hand otherwise; recommended) versus
   adding allocated closures to the language.
5. **Direct interfaces for single-pointer structs**, as Go does (recommended).
6. **Escape errors (roadmap item 5) as the first prerequisite** of M3 (recommended).
7. **Sum types**: accept option A, which the constant package and error results would use.
8. **Bootstrap policy**: release N built by release N-1, the frozen Go implementation as the root of the
   chain, release artifacts with hashes and no seed binaries in the repository (recommended).
9. **Scope of M3**: self-hosting on Linux (phases 0 to 8), with PlutoniumOS self-hosting (integrated
   assembler and linker, the OS target) as a following milestone (recommended), or both in M3.
10. **Formatter**: port `pluto fmt` after cutover (recommended), since compiling does not need it.
11. **Compiler stack**: a large reserved main-thread stack plus a depth limit in the type checker
    (recommended).
12. **Library naming**: the spec's `fmt.Sprintf` writes into a caller's buffer; the port wants a version
    returning a string from an allocator. Pick the names (roadmap item 47). *Settled by the ownership
    model: `fmt.Sprintf` returns a `strings.Buf` and `Bprintf` formats into a buffer (section 11).*

## 11. Amendment: the ownership model and the `pu` package (2026-10-09)

The ownership model (language spec section 6) was approved after this plan was written. It splits types
into values, views (`string`, `[]T`, `*T`) and owners (types declared `owner`: `vec.Vec`, `hash.Map`,
`strings.Buf`, `os.File`, `owned[T]`), requires every allocation to have an owner or an arena, and makes
`fmt.Sprintf` return a `strings.Buf`. Section 3.1's translation reproduces Go's semantics on arenas through
`selfhost/compat`, which hides exactly what the model wants visible: `*hash.Map` pointers standing in for
Go maps, `compat.Append` growing arrays nobody owns, `compat.Sprintf` returning a string nobody owns. This
amendment changes the strategy for the data structures while keeping the arenas.

### 11.1 The `pu` package

A Go package `pu` (in this repository, for example `internal/pu`) mirrors the Plutonium library API for
the types the compiler uses:

| `pu` (Go) | Plutonium | Go implementation |
|---|---|---|
| `pu.Map[K, V]` | `hash.Map[K, V]` | a struct holding a Go `map[K]V` |
| `pu.Set[K]` | `hash.Set[K]` | a struct holding a `map[K]struct{}` |
| `pu.Vec[T]` | `vec.Vec[T]` | a struct holding a `[]T` |
| `pu.Buf` | `strings.Buf` | a struct holding a `[]byte` |
| `pu.Sprintf(format, args...) pu.Buf` | `fmt.Sprintf` | `fmt.Appendf` into a new `Buf` |
| `pu.New[T](v T) *T` | `a.New[T]()` on the phase arena, filled with `v` | `&v` (the collector owns it) |

The methods are the Plutonium ones, with the same names and signatures (`Get`, `Set`, `Delete`, `Len`,
`Iter`, `Push`, `At`, `Slice`, `WriteString`, `String`, `Clone`, `Drop`), so a call in Go and its
translation are the same text with a different import. In Go, `Drop` does nothing. Each type embeds a
`noCopy` marker so that `go vet`'s copylocks check reports copies, which are the Go spelling of a use after
move; `Clone` is the explicit copy in both languages.

**Status (done, roadmap item 52).** `internal/pu` exists with the names above plus `NewVec`,
`VecWithCapacity`, `NewMap`, `NewSet` and the iterators `MapIter` and `SetIter`; `pu.Map` holds its
values behind pointers so that `Ptr` and `Upsert` can return them as `hash.Map`'s do. What the translation
maps to today, where the library is not yet at the end state:

- `pu.Buf` becomes `strings.Builder` (zero value usable, `Drop` added); it becomes `strings.Buf` with
  roadmap item 51 by changing one entry of go2pu's table. `pu.Sprintf` becomes `selfhost/pu.Sprintf`,
  which returns a `strings.Builder`: `pluto/fmt` cannot return a `pluto/strings` type yet without an
  import cycle through the tests of `pluto/strings` (roadmap item 47).
- `pu.New(v)` becomes `selfhost/pu.New(v)`, which copies `v` into memory from the default allocator,
  the phase arena by the driver's convention. The library's spelling, `a.New[T]()` (added to
  `arena.Arena` and `arena.Chunked`), names its arena, which the translated code does not have at hand.
- The library gained what the mirror needs: zero `vec.Vec` and `hash.Map` values that take the default
  allocator when they first grow, `Map.At`, `Clone` on `Vec`, `Map` and `Set`, and `Drop` on each.

go2pu's own allocations follow: an escaping `&T{...}`, and a variable Go moves to the heap, now become
`pu.New(...)` (`pu.Alloc[T]()` for a variable without a value) instead of `new(...)`, so the checked-in
translations no longer depend on what `new` returns.

`TestPuMirrorsLibrary` (go2pu) checks that every name and method of `internal/pu` exists in the library
under its mapped name; the `pudemo` demo, translated with no notes, prints the same output from Go and
from Plutonium at `-O0` and `-O2`. The compiler's own code has not moved to `pu` yet (section 11.2). The
lint configuration exempts `pu.Buf`'s writes from errcheck, as errcheck exempts `strings.Builder`'s;
`fmt.Fprintf(&buf, ...)` into a `pu.Buf` needs its results discarded explicitly.

### 11.2 Migration

The compiler's Go sources move to `pu` package by package, while Go is still the source of truth and every
change is checked by the Go build and tests:

1. Map types become `pu.Map` values held by their owner (a struct field or a local), passed down as
   `*pu.Map`. A map that several structures share is owned by one of them, or by the phase.
2. Slices that grow become `pu.Vec`; slices that are only passed and read stay `[]T` views of a `Vec`'s
   `Slice()`.
3. String building (`+`, `Sprintf`, `strings.Builder`, `bytes.Buffer`) becomes `pu.Buf`; strings that are
   only passed and compared stay `string` views.
4. Pointer graphs (AST nodes, `types2` objects, SSA values) stay `*T`. In Plutonium they are views of the
   phase arena, allocated with `pu.New`, which is the "or an arena" half of the model's allocation rule.

go2pu then translates `pu.X` to the Plutonium type one to one, and `selfhost/compat` shrinks to what has no
library counterpart. Go's escape analysis is still the oracle for `&T{...}`, which becomes `pu.New` in the
Go source rather than `new(T{...})` in the translation; under the proposal of language spec section 4.2,
`new` returns an `owned[T]`, which cannot be stored in the compiler's `*T` fields, so arena allocation must
be written as such.

### 11.3 Effect on the rest of this plan

- Section 3.1: the map, `append`, concatenation, `Sprintf` and `Builder` rows translate `pu` calls
  instead of rewriting Go built-ins; `compat.Concat`, `compat.Append`, `compat.String` and the map helpers
  become unnecessary for migrated packages.
- Section 3.4: arenas stay as planned. Owners in an arena free into it, which does nothing, so the same
  code runs with per-phase arenas and, later, with a freeing allocator.
- Section 3.5: errors follow language spec section 14, including the proposal of section 14.6 if it is
  signed off.
- Section 7, gaps 9 to 11: string formatting, map ergonomics and growing slices are met by `strings.Buf`,
  `hash.Map` and `vec.Vec` as owners, and by the indexing interface (roadmap item 49) once it lands.
- The migration to `pu` is new work in phase 0 or 1 of section 8, mechanical and checked by the existing
  Go tests; it can proceed package by package in the same order as the translation.
