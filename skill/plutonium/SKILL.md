---
name: plutonium
description: Write, modify and debug code in the Plutonium language (a Go-familiar systems language with ownership, no garbage collector and no data races) with the pluto compiler suite. Use when working on .p source files or pluto.mod, when the user mentions Plutonium, PlutoniumOS, kernel or freestanding code, pluto build / run / test / fmt / vet / doc / explain, when a compile error has a code like E2013 or E3301, or when translating Go to Plutonium. Load before writing any .p file; Plutonium looks like Go and is not Go.
---

# Plutonium

Plutonium is **Go-familiar, not Go-compatible**. The syntax, packages, goroutine-style concurrency (called
*tasks*), generics and tooling are close to Go, but there is no garbage collector, no `append`, no `recover`,
strings are always valid UTF-8, and memory ownership is part of the type system. Code that looks like Go and
compiles does what Go would do; everything else is a compile error that names the fix. The compiler is `pluto`,
the standard library is Plutonium source under `pluto/...`, and the specification is `docs/spec/language.md`
plus `docs/spec/language/*.md` (the specification wins over the compiler and over this skill).

Every program in this skill and its references is compiled and run by `go test ./tools/skillcheck`, every error
example must fail with the stated code, and every library name and error code it cites must exist.

## Which reference to read

| You are about to | Read (in `references/`) |
|---|---|
| write any Plutonium for the first time | `language-tour.md` |
| call a library function or import a package | `library-cheatsheet.md` (the calls used most, with the shapes that differ from Go), then `stdlib.md`, `stdlib-os-time-math.md`, `stdlib-memory-concurrency.md` (every name; `pluto doc pluto/<pkg>` is authoritative) |
| copy a working pattern (CLI, files, lists, trees, enums, unions, arenas, pools, pipelines, tests) | `idioms.md`, `idioms-types-memory.md`, `idioms-memory.md`, `idioms-concurrency.md` (an LRU cache, a worker pool, a CLI, file I/O and more) |
| fix a move, borrow, lifetime or `owner` error (E30xx to E33xx), or use arenas, `owned`, `shared`, `synced`, `weak` | `ownership-and-memory.md` |
| use `task`, channels, `select`, `sync.Scope`, locks, or fix a race error (E34xx, E35xx) | `concurrency.md` |
| handle failures, write `or` clauses, understand panics | `errors-and-panics.md` |
| look up an error code | `diagnostics.md`, or run `pluto explain E....` |
| port Go code | `go-to-plutonium.md` |
| write a kernel, driver, freestanding program, interrupt handler | `kernel-mode.md`, `stdlib-system.md` |
| run into something the compiler or library does not do as the spec says | `known-gaps.md` |

## A first program

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/strconv"
	"pluto/sync"
	"pluto/vec"
)

// A function that can fail returns error last; the or clause passes it up.
func parseAll(fields []string) (vec.Vec[int], error) {
	out := vec.New[int]() // a growing sequence is an owner, returned by move
	for _, f := range fields {
		n := strconv.Atoi(f) or return out, fmt.Errorf("bad number %q: %w", f, err)
		out.Push(n)
	}
	return out, nil
}

func main() int64 {
	nums, err := parseAll([]string{"3", "4", "5"})
	if err != nil {
		fmt.Println("error:", err)
		return 1
	}
	var partial [3]int
	var s sync.Scope // tasks that may use main's variables, joined before s ends
	for i := range partial {
		s.Task(func() { partial[i] = nums[i] * nums[i] }) // disjoint writes: no lock
	}
	s.Wait()
	fmt.Println(nums, partial)
	return 0
}
```

```output
[3 4 5] [9 16 25]
```

## The five rules that prevent most errors

1. **Never guess a library name.** The library is small and its names differ from Go's: there is no
   `strings.Split`, `strings.Fields`, `sort.Slice`, `bufio`, `fmt.Sscanf`, `os.IsNotExist`, `io.Copy`, `sync.Map`,
   `Time.Format`. `os.Args` is a function. Look the name up in `references/library-cheatsheet.md` or `references/stdlib*.md`, or run
   `pluto doc pluto/strings` and `pluto doc pluto/strings Cut`. A wrong name is E0073 and names the nearest
   match.
2. **A slice, pointer, closure or interface that points at a local is a view, and a view cannot outlive what
   it looks at.** Slices never grow: there is no `append` (use `vec.Vec[T]`: `v.Push(x)`), no `make([]T, n)`
   (use `vec.Make[T](n)` or an array), and `new(T)` returns `owned[T]`, not `*T`. Do not return `&local`,
   `&T{...}`, `local[:]`, a slice literal (`return []string{"a"}`), a capturing closure or a large value converted
   to an interface (E3001); return the value or an owner (`vec.Vec`, `owned[T]`), a package-level slice, or fill
   a buffer the caller passes. Constructors return `T` or `owned[T]`, not `*T`.
3. **Types have kinds, and owners move.** A type is a *value* (copies), *counted* (strings, maps, channels,
   `shared`/`synced`/`weak`: copies and counts), a *view*, or an *owner* (`vec.Vec`, `hash.Map`,
   `strings.Builder`, `sync.Scope`, `owned[T]`, any type you declare `owner`: assignment, passing and
   returning **move**). Borrow with `&x`; give away with `x`; copy with `x.Clone()`; take out of a field with
   `take(&s.f)`. A struct that contains an owner **must be declared `owner`** (E3202). Using a moved variable is
   E3301.
4. **Errors are values; a panic ends the program.** There is no `recover`. Return `error`, and propagate with the
   or clause: `n := strconv.Atoi(s) or return` (the clause sees `err`). `errors.New` takes a constant;
   use `fmt.Errorf("...%w", err)` for context. `s[i:j]` on a string is E2019: use
   `strings.Slice(s, i, j)`, `strings.SliceFrom(s, i)`, `strings.Cut`.
5. **Tasks share only what is sendable, and the compiler rejects races.** `task f(x)` (not `go`) copies or moves
   its arguments; a capturing function literal cannot be a task (pass values as arguments). To use the
   caller's variables from parallel code use `var s sync.Scope; s.Task(func(){...}); s.Wait()`. Everything
   several tasks write needs one lock held at every access, an atomic, or a disjoint index (E3402, E3502).
   `shared[T]` is for one task; use `synced[T]` across tasks.

## Go to Plutonium: the differences

**Looks like Go, behaves differently**

| Go | Plutonium |
|---|---|
| `new(T)` is a `*T` | `owned[T]`, an owner that dereferences like `*T`; free when dropped |
| a captured variable lives on the heap | it lives where it was declared; a closure is a view of its frame |
| each `for` iteration has its own variable | the same storage is reused; a closure or pointer that keeps it past the iteration is E3006 |
| `fmt.Println(v)` of any value | works on owners too: it lends `v`, nothing moves; passing `v` to your own function by value moves it |
| `x[k]` on a container is an element | on `vec.Vec`/`hash.Map` it is a **copy** from `Index`; `&v[i]` is E2454, `v[i].f = 1` is E2455 (use `v.Ptr(i)`) |
| a struct is always copyable | a struct holding an owner is declared `owner` and moves |
| a map holds anything | keys and values may not be owners (E3213); a map is a counted reference, copies share it |
| `errors.New` allocates | no allocation, constant text only; each call returns a new error |
| a deferred call in a loop is fine | E2530 if a path can reach the `defer` again; move the body to a function |
| `panic` unwinds, `recover` stops it | a panic never returns; hosted code runs deferred calls, owners are not dropped |
| a stack grows | a task has a fixed stack; deep recursion in a task can overflow (panic with a trace) |
| `print`/`println` are rarely used | they write to **stderr**; `fmt.Println` writes to stdout; `func main() int64` returns the exit status |

**Removed (compile error that names the fix)**

`append` (E2013), `make([]T, n)` (E2014), `s[i:j]` on a string (E2019), `string(bytes)`, `[]byte(s)`, `[]rune(s)`,
`string(intVar)` (use `string(rune(i))` or `strconv.Itoa`), `recover` (E2004), `complex`/`real`/`imag` (E2006),
`import "reflect"`, `go` statements, identifiers named `go`, `enum` or `packed` (reserved words), `//go:` comments, `package x_test`, goroutines started during package
initialization (E2587), `errors.New(nonConstant)` (E2008), `copy` of owner elements (E3306), string literals
that are not valid UTF-8 (E2012), `iter.Pull`, a local `sync.WaitGroup` passed to a task (use `sync.Scope`).

**Renamed or new**

| Go | Plutonium |
|---|---|
| `go f(x)` | `task f(x)` |
| `var wg sync.WaitGroup; wg.Add(1); go func(){ defer wg.Done(); ... }()` | `var s sync.Scope; s.Task(func(){ ... }); s.Wait()` (a local `WaitGroup` passed to a task is E3004); partial results go in an indexed array, one element per task |
| `.go`, `go.mod` | `.p`, `pluto.mod` (`module path` as the first line) |
| `import "strings"` | `import "pluto/strings"` |
| `runtime.Goexit` | `rt.ExitTask()` |
| `os.Args` | `os.Args()` (a `vec.Vec[string]`), `os.Arg(i)`, `os.NArgs()` |
| `strings.Split`, `Fields` | `strings.SplitSeq`, `FieldsSeq` (range over them), `SplitInto`, `FieldsInto` |
| `sort.Slice` | `slices.SortFunc(x, func(a, b T) int)`, `sort.SortFunc(x, func(a, b T) bool)` |
| new keywords/types | `owner`, `enum`, `union`, `packed`, `owned[T]`, `shared[T]`, `synced[T]`, `weak[P]`, `volatile[T]`, `aligned[T, N]`, `ring[T, N]`, `portable` |
| new syntax | `x := f() or return`, default parameters `func f(n int, k int = 2)`, `take(&place)`, `Color.Red`, `Shape.Rect{...}` |

## Ownership on one page

**Kinds.** value (bits; copies), counted (`string`, `map`, `chan`, `shared`, `synced`, `weak`; copies and adds
a count; last release destroys), view (`*T`, `[]T`, func values, ordinary interfaces; copies, never frees), owner
(moves; `Drop` runs at end of life). `pluto doc` prints the kind next to every type.

**Move and drop.** Assigning, passing by value, returning, sending, `new(v)` and storing in a literal move an owner;
the source is unusable until reassigned. An owner is dropped at the end of its scope on every path, in reverse
declaration order, after deferred calls; a `Drop(){...}` method (pointer receiver) releases the resource. Moving
out of a field/element/pointer target is E3303: use `take(&place)`. Assigning over an owner drops the old value.

**When you need an owner.** Memory that grows (`vec.Vec`), a heap object with a stable address or a node in a
list or tree (`owned[T]`), a resource that must be released (declare `type File owner struct{...}` with `Drop`),
an arena. Otherwise use plain values, strings, arrays and views. Counted boxes for sharing: `alloc.Shared[T]()`
(one task) or `alloc.Synced[T]()` (many), `weak[...]` for back pointers.

**Lending (the four ways to let code look without moving).**
1. `&x`, a pointer parameter: the callee borrows for the call (`func use(c *Conn)`; call `use(&c)`).
2. A derived view: `v.Slice()`, `a[i:j]`, `(*T)(box)`, `&*box`, `v.Ptr(i)`: valid while the owner is unmoved
   and, for `vec`, until the next `Push`.
3. Conversion to an ordinary interface (`any`, `error`, `fmt.Stringer`): `fmt.Println(v)` lends `v`;
   you cannot assert an owner back out (E3308).
4. A closure, method value or `sync.Scope` task capturing the frame: valid while the frame is, joined before the
   scope ends.

**Allocation is explicit.** `new`, `vec.New`, strings, maps and channels use the current allocator (`alloc.Use`);
pass an arena for scratch memory (`vec.New[T](&a)`); an arena-allocated thing is a view of the arena. See
`ownership-and-memory.md`.

## Toolchain

Build the compiler from the repository: `go build -o pluto ./cmd/pluto` (or `GOBIN=$HOME/go/bin go install
./cmd/pluto`). The standard library is embedded; set `PLUTOROOT=$PWD/lib` to use the working tree's library
instead (needed when you edit `lib/pluto`). Run with `unset GOROOT` if the Go toolchain complains about versions.

| Command | Use |
|---|---|
| `pluto build [dir] -o out` | compile and link a hosted program (ELF, static, no libc). Flags: `-O 0\|1\|2` (default 2), `-g`, `-S -` (listing), `--ssa=main.f`, `--tags a,b`, `--brief`, `--gencheck=false` (unchecked mode), `--alloc=debug`, `--mode hosted\|freestanding\|kernel`, `--target x86_64-linux\|x86_64-none`, `-c` (object file only), `--ldscript`, `--format elf\|binary`, `--trimpath`, `--work`, `-v` |
| `pluto run [dir] [-- args]` | build and run |
| `pluto test [pkgs]` | run `TestXxx(t *testing.T)` from `*_test.p` files; `-run re`, `-v`, `-count n`, `-bench re`, `-O n`, `-brief`; packages are directories, `pluto/strings`, or `./...` |
| `pluto fmt [-l] [-w] [-d] [paths]` | gofmt for `.p` files; `-l` lists, `-w` rewrites, `-d` diffs |
| `pluto vet -unsafe [pkgs]` | lists unsafe operations and the safe functions that call them (the only vet check) |
| `pluto doc [-all] [pkg] [Sym[.Method]]` | package docs with the kind of each type; `pluto doc pluto/vec Vec.Push` |
| `pluto explain [E....]` | long explanation of an error code, with example and fix; no argument lists all codes |
| `pluto version`, `pluto lsp` | version; language server on stdin/stdout |

- A program is a directory with `package main`. A module has `pluto.mod` (`module example.com/app`); its packages are
  imported as `example.com/app/sub`. Standard library imports are `pluto/<name>`. No package downloads exist.
- Environment: `PLUTOROOT` (library tree), `PLUTO_BRIEF=1` (one-line errors), `PLUTO_MAXPROCS` (worker threads),
  `PLUTO_DEBUGALLOC` (options for `--alloc=debug`).
- Hosted builds check generations on heap pointers: a use of freed memory panics `use of freed memory`.
- Exit status of a run: `main`'s `int64`, 2 after a panic or deadlock.

## Reading a diagnostic

The program is `a := vec.New[int](); b := a; a.Push(1)`. `pluto build` prints (paths shortened):

```
main.p:8:2: a was moved to b at line 7, so it can no longer be used here
    pluto/vec.Vec[int] is an owner type: its values own what they hold and
    release it when they are dropped. Assigning an owner moves it instead of
    copying it, so that what it owns has exactly one owner. Since line 7, b owns
    it and a holds nothing.
    If you need two independent values, copy it explicitly at line 7:
        b := a.Clone()
    If b only needs to look at the value, give it a pointer instead:
        b := &a
    (see "Moves" in the Plutonium spec, section 18.5)
    (run "pluto explain E3301" for a longer explanation)
```

The first line is `file:line:col: message` (all that `--brief` prints, with no code); the code is in the last
line, and the indented text names the cause and shows the fix. Codes: E0xxx Go type rules,
E1xxx syntax, E2000 to E2499 Plutonium type rules (removed constructs, enums, unions, defaults, `or`), E2500 to
E2999 compile-time rules (directives, kernel, defer, task), **E3xxx ownership** (E30xx views and frames, E31xx
closures, E32xx owner types, E33xx moves and drops, E34xx scoped tasks, E35xx races), E4xxx packages, E5xxx link.
Fix the **first** error; later ones are often consequences. `--brief` (or `PLUTO_BRIEF=1`) prints one line per error
and **leaves out the code and the fix**: use it only to scan a long list, and run `pluto build` without it to read a
fix or get the code for `pluto explain`.
Typical fixes:

| Code | Cause | Fix |
|---|---|---|
| E0008, E0101 | unused import / variable | delete it (Plutonium is as strict as Go) |
| E0073 | unknown library name | check `stdlib*.md`; the message suggests the nearest |
| E2013, E2014 | `append`, `make([]T, n)` | `vec.Vec`, `vec.Make`, arrays |
| E2019 | slicing a string | `strings.Slice`, `SliceFrom`, `Cut`, `Truncate` |
| E3001 to E3005 | a view of a local escapes | return an owner or the value; pass a buffer in; `sync.Scope` for tasks |
| E3006 | loop variable kept by a closure | use it inside the iteration; pass as argument |
| E3202 | struct with an owner field | declare it `owner` |
| E3207 | owner given to a type parameter that copies | `[T owner any]`, or a pointer |
| E3213 | owner in a map | `synced[T]` elements |
| E3301, E3302 | use after move | `&x` to borrow, `x.Clone()`, reorder |
| E3303 | move out of a field | `take(&s.f)` |
| E3402, E3502 | write race | one lock at every access, `atomic`, or disjoint indexes |
| E2590 | capturing literal as a task | pass the values as arguments |

## Workflow

1. **Read** the relevant reference (above), and `pluto doc` for any library name you are not certain of.
2. **Write** small, then `pluto fmt -w .`.
3. **Build** `pluto build ./dir` (or `pluto run`); fix the first error by its code, reading the
   explanation; `pluto explain E....` for the rule and a worked fix. Do not silence an error with `unsafe` or
   `//pu:nochecks`; those are for reviewed low-level code.
4. **Test** `pluto test ./...` (write `*_test.p` next to the code, in the same package). Run once with
   `pluto run --alloc=debug` for leaks and stale uses (it lists live allocations and exits with status 2).
5. **Vet** `pluto vet -unsafe ./...` if you touched `unsafe`, `pluto/arch/x86` or `//pu:` directives.
6. Hosted code needs no `//pu:` directives; the compiler infers lifetimes, locks and what never returns.
7. In kernel or freestanding work, also `pluto build --mode kernel -c` and read `kernel-mode.md`.
8. If the compiler contradicts the specification, check `known-gaps.md`, then say so rather than working around
   it silently.

## Repository map (when working on the compiler or library itself)

`cmd/pluto` (driver) · `internal/{syntax,types2,load,moves,escape,race,scoped,lower,ssa,opt,arch,build,diag}`
(pipeline; see `AGENTS.md`) · `lib/pluto` (standard library, Plutonium) · `docs/spec/language/` (specification,
sections cited as "section 18.3") · `docs/decisions.md` (design decisions D1 to D30) · `docs/roadmap.md` · `examples/`
and `testdata/run` (programs) · `cmd/pluto/testdata/diag` (every diagnostic, full output) · `tools/go2pu` (compiler
port helper) · `tools/skillcheck` (checks this skill). Regenerate the generated references with
`skill/plutonium/tools/regenerate.sh`.
