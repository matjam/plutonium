# Translating Go to Plutonium

Plutonium is Go-familiar, not Go-compatible. A program in the common subset (spec appendix A.1) compiles
unchanged except for imports. Anything else breaks loudly: **the rule of the language is that Go code which
compiles does what Go would do**, and everything else is a compile error that names the fix. So translate by
compiling and reading the errors (`pluto build --brief` for one line each, `pluto explain E....` for the fix).

## 1. The procedure

1. Rename `*.go` to `*.p`, `go.mod` to `pluto.mod` (`module path`; no `go` or `require` lines), delete
   `_test` package clauses: tests are `*_test.p` files **in the package under test** (E4011 for `package x_test`).
2. Rewrite imports: `"fmt"` becomes `"pluto/fmt"`, and so on (table 2). `reflect` has no replacement.
3. Replace `go` with `task`; `//go:` comments with `//pu:` (or delete).
4. `pluto build ./dir`; fix errors top to bottom. The usual ones, in the order they appear:
   `append` (E2013), `make([]T, n)` (E2014), string slices (E2019), `[]byte(s)` / `string(b)`, `errors.New(var)`
   (E2008), missing library names (E0073), pointer-to-local returns (E3001), copied owners (E3301), races (E3402,
   E3502).
5. `pluto fmt -w .`, `pluto vet -unsafe` (shows unsafe code only), `pluto test ./...`.
6. Run with `pluto run --alloc=debug` once: it reports leaks and use-after-free hints.

## 2. Imports and names

| Go | Plutonium |
|---|---|
| `fmt`, `errors`, `strings`, `strconv`, `bytes`, `io`, `os`, `os/exec`, `flag`, `time`, `math`, `unicode`, `unicode/utf8`, `sort`, `slices`, `sync`, `sync/atomic`, `container/heap`, `iter`, `cmp`, `path/filepath`, `testing` | the same name under `pluto/` (`"pluto/strings"`) |
| `math/bits` | `pluto/bits` |
| `hash/fnv`, `hash/maphash` | `pluto/hash` (`FNV32a`, `FNV64a`, `SipHash24`, `Map`, `Set`) |
| `runtime` | `pluto/rt` (`rt.ExitTask` replaces `runtime.Goexit`) |
| `context`, `bufio`, `net`, `net/http`, `encoding/*`, `regexp`, `log`, `reflect`, `text/template`, `os/signal` | **not available** |

Library names that differ or do not exist (checked against the library by the skill's tests):

| Go | Plutonium |
|---|---|
| `strings.Split(s, sep)`, `strings.Fields(s)` | `for p := range strings.SplitSeq(s, sep)`, `strings.FieldsSeq(s)`; into a caller's slice: `strings.SplitInto`, `FieldsInto`; `strings.Lines(s)` |
| `strings.NewReader`, `NewReplacer`, `Title` | none; use `strings.Builder`, `ReplaceAll`, loops |
| `os.Args` (a variable) | `os.Args()` returns `vec.Vec[string]`; or `os.Arg(i)`, `os.NArgs()` |
| `os.IsNotExist(err)` | `errors.Is(err, os.ErrNotExist)` |
| `os.Open` returns `*os.File` | the same, `defer f.Close()`; `os.ReadFile` returns `vec.Vec[byte]`, so `text, err := strings.FromUTF8(data.Slice())` |
| `bufio.Scanner`, `bufio.Reader` | `io.ReadAll(os.Stdin)` then `strings.Lines`, or `os.Stdin.Read(buf[:])` |
| `fmt.Sscanf`, `fmt.Scan*` | `strconv.Atoi`, `ParseInt`, `ParseFloat` on pieces from `strings.Cut` / `SplitSeq` |
| `sort.Slice(x, func(i, j int) bool)` | `sort.SortFunc(x, func(a, b T) bool)` (less) or `slices.SortFunc(x, func(a, b T) int)` (three-way) |
| `slices.Insert`, `slices.Grow` | `v.Insert(i, xs...)` on a `vec.Vec` |
| `sync.Map`, `sync.Pool`, `sync.OnceFunc` | `hash.Map` with a `sync.Mutex`; a channel; `sync.Once` |
| `sync.WaitGroup.Go`, a local `WaitGroup` | `sync.Scope` (`s.Task(f)`, `s.Wait()`) |
| `io.Copy`, `io.Discard`, `io.MultiWriter` | a loop over `Read`/`Write` |
| `time.Parse`, `Time.Format` | `time.ParseDuration`; `t.RFC3339()`, `t.AppendRFC3339(&buf)` |
| `strconv.ParseBool` | compare with `"true"` / `"false"` |
| `filepath.Walk`, `Glob` | `filepath.WalkDir`, `os.ReadDir` |
| `iter.Pull` | not provided; loop with `range` over the function |
| `hash/fnv` `New32a()` | `var h hash.FNV32a; h.Write(b); h.Sum32()` |
| `runtime.Goexit()` | `rt.ExitTask()` |

```pluto-absent
strings.Split
strings.SplitN
strings.Fields
strings.Title
strings.NewReader
strings.NewReplacer
fmt.Sscanf
fmt.Sscan
fmt.Scan
fmt.Scanln
os.IsNotExist
os.Setenv
io.Copy
io.Discard
io.MultiWriter
sort.Slice
sort.SliceStable
sort.Stable
slices.Insert
slices.Grow
sync.Map
sync.Pool
sync.OnceFunc
strconv.ParseBool
time.Parse
iter.Pull
bytes.NewReader
path/filepath.Walk
path/filepath.Glob
hash.New32a
hash.New64a
```

```pluto-names
strings.SplitSeq
strings.SplitInto
strings.FieldsSeq
strings.FieldsInto
strings.Lines
strings.Bytes
strings.Runes
strings.FromUTF8
strings.FromUTF8Lossy
strings.Slice
strings.SliceFrom
strings.Truncate
strings.Builder
strings.Cut
unicode/utf8.RuneCountInString
unicode/utf8.RuneStartBefore
os.Args
os.Arg
os.NArgs
os.ReadFile
os.WriteFile
os.Open
os.Stdin
os.Stdout
os.ErrNotExist
io.ReadAll
slices.SortFunc
slices.Sort
slices.Clone
slices.Collect
slices.Concat
slices.Delete
sort.SortFunc
sort.Strings
sort.Ints
vec.Make
vec.WithCapacity
vec.AppendString
vec.Vec.Push
vec.Vec.Insert
sync.Scope.Task
sync.WaitGroup.Task
sync/atomic.Int64.Add
errors.AsType
time.Since
time.ParseDuration
time.Time.RFC3339
path/filepath.WalkDir
hash.NewMap
hash.FNV32a.Sum32
rt.ExitTask
```

## 3. Syntax and semantics that change

| Go | Plutonium | Error / note |
|---|---|---|
| `go f(x)` | `task f(x)` | `go` is an error that names `task`; arguments must be sendable |
| `go func() { use(n) }()` | `task func(n int) { use(n) }(n)` | E2590: no captures in a task literal |
| `wg.Add(1); go func(){ defer wg.Done(); ... }()` | `s.Task(func(){ ... })` on a `sync.Scope` | joins before the function ends; may use locals |
| `append(s, x)` | `v.Push(x)` on a `vec.Vec` | E2013 |
| `s = append(s, xs...)` | `v.Append(xs...)` | |
| `append([]T(nil), s...)` | `slices.Clone(s)` (a `vec.Vec`) | |
| `s = append(s[:i], s[j:]...)` | `s = slices.Delete(s, i, j)` | in place |
| `make([]T, n)` | `vec.Make[T](n)` (owner) or `var a [N]T; a[:]` | E2014 |
| `make([]T, 0, n)` | `vec.WithCapacity[T](n)` | |
| `new(T)` / `&T{}` returned | `new(T)` returns `owned[T]`, not `*T`; returning `&local` is E3001 | return the value or an `owned[T]` |
| `[]byte(s)`, `[]rune(s)` | `strings.Bytes(s)`, `strings.Runes(s)` (owners) or `copy(buf[:], s)` | |
| `string(b)` | `strings.FromUTF8(b)` or `strings.FromUTF8Lossy(b)` | |
| `string(i)` for an `int` | `string(rune(i))` or `strconv.Itoa(i)` | |
| `s[i:j]` on a string | `strings.Slice(s, i, j)`, `strings.SliceFrom(s, i)` | E2019; panics off a rune boundary |
| `errors.New(msg)` with a variable | `fmt.Errorf("%s", msg)` | E2008; constants are fine |
| `if err != nil { return err }` | unchanged, or `x := f() or return` | |
| `recover()` | return an `error` | E2004; a panic ends the program |
| `defer` in a loop | move the body into a function | E2530 |
| `for i := 0; ...` closures keeping `i` | pass `i` as an argument | E3006 |
| `map[K]V` of structs/pointers | the same; owners are not allowed as values (E3213) | maps are counted references |
| `type T struct{ f *os.File }` | the field is a pointer (view) | fine; closing is explicit |
| a struct holding a `vec.Vec`, `owned[T]`, `strings.Builder` | declare it `owner` | E3202 |
| copying such a struct | moves it; `Clone()` to copy | E3301 |
| `x := m[k]` where `m` is `hash.Map` | works via `Index`; `&m[k]` is E2454 | use `m.Ptr(k)` |
| `main` returns nothing, `os.Exit(n)` | `func main() int64 { return n }` | |
| `println` / `print` to stderr | the same, stderr | |
| `unsafe` conversions | unrestricted but unsafe code; `unsafe.Drop` is new | audit with `pluto vet -unsafe` |
| `init()` starting goroutines | not allowed | E2587 |
| goroutine leaks keep the process alive | `main` returning ends the program | join your tasks |
| `copy`, `clear`, `min`, `max`, `delete`, `close`, `len`, `cap` | the same | |

A function that returned a slice of a local array (Go moved it to the heap) is the most common semantic change:
Plutonium never moves a variable to the heap, so return a `vec.Vec`, an `owned[T]`, or fill a buffer the caller
passes. Interfaces holding large values are views of the frame: returning one is E3001.

## 4. A worked translation

The Go program (not checked):

```go
type Counter struct{ counts map[string]int }

func NewCounter() *Counter { return &Counter{counts: map[string]int{}} }

func (c *Counter) Add(line string) {
	for _, w := range strings.Fields(line) {
		c.counts[strings.ToLower(w)]++
	}
}

func (c *Counter) Top() []string {
	var keys []string
	for k := range c.counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func main() {
	results := make([]int, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results[i] = i * i }()
	}
	wg.Wait()
	s := "héllo"
	fmt.Println(s[0:3], []byte(s))
}
```

The Plutonium program (compiled and run by the skill's tests):

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/slices"
	"pluto/strings"
	"pluto/sync"
	"pluto/vec"
)

type Counter struct {
	counts map[string]int // a map is a counted reference: Counter stays an ordinary struct
}

func NewCounter() Counter { return Counter{counts: map[string]int{}} } // not *Counter: it would point into this frame

func (c *Counter) Add(line string) {
	for w := range strings.FieldsSeq(line) {
		c.counts[strings.ToLower(w)]++
	}
}

func (c *Counter) Top() vec.Vec[string] { // a growing result is an owner
	keys := vec.New[string]()
	for k := range c.counts {
		keys.Push(k)
	}
	slices.Sort(keys.Slice())
	return keys
}

func parse(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	return len(s), nil
}

func run(lines []string) (int, error) {
	total := 0
	for _, l := range lines {
		n := parse(l) or return 0, err
		total += n
	}
	return total, nil
}

func main() {
	c := NewCounter()
	var results [4]int // make([]int, 4) is E2014
	var s sync.Scope   // replaces the WaitGroup
	for i := 0; i < 4; i++ {
		s.Task(func() { results[i] = i * i }) // may use main's variables; i is the iteration's own
	}
	s.Wait()
	c.Add("a b A")
	fmt.Println(c.Top(), results)
	t, err := run([]string{"ab", "c"})
	fmt.Println(t, err)
	str := "héllo"
	fmt.Println(strings.Slice(str, 0, 3)) // str[0:3] is E2019
	b := strings.Bytes(str)               // []byte(str) is an error: the result is an owner
	back, _ := strings.FromUTF8(b.Slice())
	fmt.Println(len(b), back)
}
```

```output
[a b] [0 1 4 9]
3 <nil>
hé
6 héllo
```

## 5. `tools/go2pu`

`go run ./tools/go2pu -out DIR -mod MODULE [-map from=to]... GODIR...` translates Go packages into Plutonium
source. It exists to port the **compiler itself** (the self-hosting plan, `docs/plans/2026-10-09-self-hosting-plan.md`).
It is not a general porting tool: it translates packages of the compiler's own Go module
(`github.com/matjam/plutonium`), runs `go build -gcflags=-m` for escape information, and emits calls into the
`selfhost/compat` and `selfhost/pu` helper packages.

- **It rewrites:** maps to `hash.Map` calls; `append`, `make`, string concatenation, `string(b)`, `[]byte(s)`,
  `fmt.Sprintf`, `strings.Builder`, `bytes.Buffer`, `strconv.Append*` to `compat` functions that keep Go's
  semantics on an arena; heap-escaping variables to `pu.New`; a local `WaitGroup` used in the supported shapes to a
  `sync.Scope`; `x, err := f(); if err != nil { return 0, err }` to `x := f() or return`; string slices;
  `go` to `task`; `sort.Slice` with an index comparison to `sort.SortFunc`.
- **It does not translate** (it marks the spot with a `go2pu:` comment and lists it on standard error): closures
  and interface values that escape their frame, `recover`, reflection, goroutines and WaitGroups outside the
  supported shapes, maps modified while ranged over, binary data in string literals, `encoding/binary`,
  `iter` and range-over-func, `defer` in loops, packages with no counterpart.
- **It does not** make the result idiomatic (it leans on `compat` and arenas), check ownership, or run the code.
  After it, compile with `pluto build`, read each remaining error, and replace the `compat` calls with
  `vec.Vec` and owners by hand.

For an ordinary Go program, translate by hand with sections 1 to 4, which is usually a short edit.

## 6. Mistakes to avoid when translating

- Do not invent a `pluto/strings` function from Go habit: check section 2 and `stdlib.md` first.
- Do not wrap every struct in `owner`: only a struct that **contains** an owner type needs it.
- Do not return `&x` or `x[:]` of a local; return the value, or accept a buffer from the caller.
- Do not hold a `vec.Vec` slice (`v.Slice()`, `v.Ptr(i)`) across a `Push`.
- Do not copy `vec.Vec`, `hash.Map`, `strings.Builder` or `sync.Scope` values around: pass `&v` to borrow.
- Do not rely on goroutine leak detection or on `main` waiting for tasks.
- Do not use `s[i]` to build text from bytes beyond ASCII; range over runes.
