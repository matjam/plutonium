# Language tour

Plutonium is Go-shaped, so most Go you know compiles unchanged. This tour covers the parts that differ or are
new. Every program is compiled and run by the skill's test (`go test ./tools/skillcheck`). Section numbers
(`section 7.12`) point into `docs/spec/language/`; read that chapter when you need the full rule.

Contents: 1 program layout, 2 types and kinds, 3 declarations, 4 strings, 5 arrays, slices, vectors and
maps, 6 control flow, 7 generics, 8 enums, unions and packed types, 9 interfaces, 10 defer, 11 closures,
12 built-in functions, 13 errors and the or clause, 14 tasks and `sync.Scope`, 15 numbers and where to go next.

## 1. Program layout (chapters 14, 15)

- Source files end in `.p`. A program is a directory with `package main`. A module has a `pluto.mod` whose
  first line is `module example.com/app`; packages of the module are imported as `example.com/app/sub`.
  A lone package with no `pluto.mod` can import only the standard library. There are no downloads.
- The standard library is imported as `pluto/<name>`: `"pluto/fmt"`, `"pluto/strings"`, `"pluto/sync"`.
- `func main()` has no parameters and either no result or one `int64` result, the process exit status
  (E2009 for any other signature). Command-line arguments come from `os.Arg(i)`, `os.NArgs()`, `os.Args()` (each returns an error as well:
  an argument that is not valid UTF-8 is a `*os.NameError`; `os.ArgLossy(i)` and `os.ArgBytes(i)` are the other forms).
- `fmt.Println` writes to standard output. The built-in `println` writes to standard error and is for
  bootstrapping. A panic prints a trace to standard error and exits with status 2.
- When `main` returns the program exits, even if tasks still run (section 15.4).

```pluto-run exit=3
package main

import "pluto/fmt"

func main() int64 {
	fmt.Println("hello")
	println("this goes to stderr")
	return 3
}
```

```output
hello
```

## 2. Types and kinds (chapter 7, section 8.7)

Every type has a **kind** that decides what assignment, passing and the end of a scope do to its values.
`pluto doc pluto/vec Vec` prints the kind next to the type name.

| Kind | Types | Assignment | At end of scope |
|---|---|---|---|
| value | bool, numbers, enums, packed, and arrays/structs/unions of values | copies | nothing |
| counted | `string`, `map[K]V`, `chan T`, `shared[T]`, `synced[T]`, `weak[P]` | copies and counts | releases a count |
| view | `[]T`, `*T`, func values, ordinary interfaces, `unsafe.Pointer` | copies | nothing |
| owner | types declared `owner`, `owned[T]`, `vec.Vec`, `hash.Map`, `strings.Builder`, `sync.Scope` | **moves** | dropped (`Drop` runs) |

A struct takes the strongest kind among its fields, except that a struct with an owner field **must be
declared `owner`** (E3202). Sizes: `int`, `uint`, `uintptr` are 64 bits everywhere; `byte` is `uint8`; `rune` is
`int32`; a string and a slice are 3 words, an interface 2.

Pointers `*T` are views: they never free anything and may not outlive what they point at (section 18.3).
Pointer arithmetic does not exist. Slices, arrays, structs, maps, channels, pointers, funcs and interfaces
are written as in Go.

## 3. Declarations (chapter 10)

`const`, `iota`, `var`, `:=`, methods, embedding and type declarations are as in Go. New: default parameter
values (section 10.13), `owner` on a type declaration (section 7.18), and generic methods (section 10.15).

- Trailing parameters may have defaults: `func Grow(n int, factor int = 2)`. A call may omit a suffix. A
  default is part of the declaration, not the type, so a function value takes every argument. Variadic
  functions cannot have defaults (E2202). Library functions use `a alloc.Allocator = alloc.Default()`.
- Methods on owner types use pointer receivers unless the method consumes the owner (a value receiver on an
  owner type moves the owner into the method, section 8.6).
- `go`, `enum` and `packed` are reserved words and cannot name anything. `owner`, `union`, `task`, `or`, `bits`, `le`
  and `be` are keywords only in the positions of their construct and stay ordinary identifiers elsewhere
  (section 4.6), so `task` can still be a variable name.
- Exported names start with an upper-case letter. Unused imports and unused local variables are errors,
  as in Go.

```pluto-run
package main

import "pluto/fmt"

// Grow has default parameters: a call may leave out a suffix of them.
func Grow(n int, factor int = 2, limit int = 1 << 20) int { return min(n*factor, limit) }

type Point struct{ X, Y int }

func (p *Point) Move(dx, dy int) { p.X += dx; p.Y += dy }
func (p Point) Dist2() int       { return p.X*p.X + p.Y*p.Y }

type Named struct {
	Point // embedded: promotes Move and Dist2
	Name  string
}

const (
	A = iota * 10
	B
	C
)

func main() {
	fmt.Println(Grow(10), Grow(10, 3), Grow(10, 3, 16))
	n := Named{Point{1, 2}, "p"}
	n.Move(2, 2)
	fmt.Println(n.Dist2(), n, A, B, C)
}
```

```output
20 30 16
25 {{3 4} p} 0 10 20
```

## 4. Strings and runes (sections 7.3, 11.12, 11.24)

A string is immutable, **always valid UTF-8**, and counted when made at run time (copying adds a count; the
last drop frees the buffer). `len` counts bytes, `s[i]` is a byte, `range` yields runes.

- `s[i:j]`, `s[i:]`, `s[:j]` on a string are **E2019**. Use `strings.Slice(s, i, j)` and
  `strings.SliceFrom(s, i)`, which panic unless both bounds are rune boundaries; `strings.Truncate(s, n)`
  rounds down to a boundary; offsets returned by `strings.Index` are always boundaries.
- `string(b)` for a `[]byte` is an error: use `strings.FromUTF8(b)` (checks, returns an error) or
  `strings.FromUTF8Lossy(b)`. `[]byte(s)` is an error: use `strings.Bytes(s)` (a `vec.Vec[byte]`) or
  `copy(buf[:], s)`. `string(i)` for an integer is an error unless `i` is a `rune` or an untyped constant:
  write `string(rune(i))` or `strconv.Itoa(i)`.
- `"\xff"` and other literals that are not valid UTF-8 are errors (E2012). Keep binary data in `[]byte`.
- `+` and `+=` allocate a new buffer. Build large strings with `strings.Builder`.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/strings"
)

func main() {
	s := "héllo, wörld"
	fmt.Println(len(s), strings.Slice(s, 0, 3), strings.SliceFrom(s, 7))
	fmt.Println(strings.Truncate(s, 2)) // "h": byte 2 is inside the é
	for i, r := range "añb" {
		fmt.Print(i, ":", string(r), " ")
	}
	fmt.Println()
	before, after, ok := strings.Cut("key=value", "=")
	fmt.Println(before, after, ok)
	text, err := strings.FromUTF8([]byte{0x68, 0x69})
	bs := strings.Bytes("hey") // an owner: a vec.Vec[byte]
	var buf [8]byte
	n := copy(buf[:], "hello")
	fmt.Println(text, err, bs.Slice(), n, string(rune(65)))
}
```

```output
14 hé  wörld
h
0:a 1:ñ 3:b
key value true
hi <nil> [104 101 121] 5 A
```

## 5. Arrays, slices, vectors and maps (sections 7.4, 7.5, 7.10, 13)

- An array `[N]T` is a value (copied on assignment). A slice `[]T` is a **view** of an array that someone else
  owns. It never grows: there is no `append` (E2013) and no `make([]T, n)` (E2014).
- A sequence that grows is `vec.Vec[T]`, an owner: `v := vec.New[T]()`, `v.Push(x)`, `v.Append(xs...)`,
  `v.Pop()`, `v.Slice()` (a view; any `Push` may invalidate it), `v.Ptr(i)` (a pointer to an element,
  invalidated the same way). `len(v)`, `v[i]`, `v[i] = x` and `for i, x := range v` work through the
  methods `Len`, `Index`, `SetIndex`, `All` (section 11.11). `&v[i]` is E2454 and `v[i].f = 1` is E2455; use
  `v.Ptr(i).f = 1`. For a fixed size use an array, for a bounded buffer `vec.NewFixed(buf[:])`.
- `slices.Clone`, `slices.Concat`, `slices.Collect` return a `vec.Vec`. `slices.Delete`, `slices.Sort`,
  `slices.Reverse` work in place on a slice. `copy` and `clear` work as in Go (`clear` on a slice of owners
  drops each).
- A slice literal (`[]string{"a"}`) is an array in the frame of the function that evaluates it, so a function cannot
  return one (E3001); a package-level `var names = []string{...}` is static and may be returned and kept.
- A map is a **counted reference**: copies share the table, and the table is freed with the last copy.
  Keys and values may not be owners (E3213: use `synced[T]` elements). Iteration order is
  unspecified. A nil map reads as empty; storing into it panics.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/slices"
	"pluto/vec"
)

func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func fill(m map[string]int) { m["a"] = 1 }

func main() {
	arr := [5]int{1, 2, 3, 4, 5}
	s := arr[1:4] // a view of arr: len 3, cap 4
	s[0] = 20
	fmt.Println(arr, len(s), cap(s), sum(s))
	fmt.Println(s[:4]) // slicing within the capacity is fine

	v := vec.New[int]()
	for i := 0; i < 5; i++ {
		v.Push(i * i)
	}
	fmt.Println(v, len(v), sum(v.Slice()))
	last, _ := v.Pop()
	v[0] = 100
	fmt.Println(last, v[0], v)

	slices.Sort(arr[:])
	fmt.Println(arr)
	k := slices.Delete(arr[:], 1, 3) // in place; returns the shorter view
	fmt.Println(k)

	m := map[string]int{}
	fill(m) // the caller's map changes
	m2 := m
	m2["b"] = 2
	delete(m, "a")
	fmt.Println(m, len(m2))
}
```

```output
[1 20 3 4 5] 3 4 27
[20 3 4 5]
[0 1 4 9 16] 5 30
16 100 [100 1 4 9]
[1 3 4 5 20]
[1 5 20]
map[b:2] 1
```

## 6. Control flow (chapter 12)

`if`, `for`, `switch`, `select`, labels, `goto`, `break`, `continue` and `fallthrough` are as in Go. Plus:

- `for i := range n` ranges over an integer; `for v := range Enum` over the members of an enum;
  `for x := range f` over an iterator function; a library container ranges over its `All` method
  (`vec.Vec` yields index and element, `hash.Map` key and value).
- A `for` loop reuses its variables, so a closure or pointer to a loop variable that outlives the iteration
  is **E3006**. Use the value inside the iteration, or pass it as an argument.
- A `switch` on an enum or a union with no `default` must name every member or variant (E2312, E2414), and
  then ends a function like a `return` does.
- `x := f() or return` is the or clause (section 11.27; see `errors-and-panics.md`).
- `task f(x)` starts a task (see `concurrency.md`). The word `go` is an error.

```pluto-run
package main

import "pluto/fmt"

type Level enum {
	Debug
	Info
	Warn
}

func classify(x any) string {
	switch v := x.(type) {
	case nil:
		return "nil"
	case int, int64:
		return fmt.Sprint("integer ", v)
	case string:
		return "string " + v
	case error:
		return "error " + v.Error()
	default:
		return "other"
	}
}

func main() {
	for i := range 3 {
		fmt.Print(i, " ")
	}
	for l := range Level {
		fmt.Print(l, " ")
	}
	fmt.Println(len(Level))
	fmt.Println(classify(nil), classify(3), classify("s"), classify(fmt.Errorf("e")), classify(2.5))
}
```

```output
0 1 2 Debug Info Warn 3
nil integer 3 string s error e other
```

## 7. Generics (sections 10.9, 10.15, 11.16, 11.17)

Type parameters work as in Go: constraints (`cmp.Ordered`, `comparable`, `any`, `~int | ~string`), inference,
generic types and **generic methods** (a method may have its own type parameters).

- A type parameter admits an owner type only if declared `[T owner any]` (E3207 otherwise). In such code
  values of `T` move. `vec.Vec[T]` and `hash.Map[K, V]` accept owner elements and own them; the built-in `map` does
  not (E3213): put `synced[T]`/`shared[T]` boxes in it, or use a `hash.Map`.
- A generic type that stores an owner (a `vec.Vec[T]`, an `owned[T]`) is itself an owner and must be declared
  `owner` (E3202).
- Constraint `portable` (section 8.9) means "can cross address spaces"; it is for kernels.

```pluto-run
package main

import (
	"pluto/cmp"
	"pluto/fmt"
	"pluto/vec"
)

type Stack[T any] owner struct {
	items vec.Vec[T]
}

func (s *Stack[T]) Push(x T)       { s.items.Push(x) }
func (s *Stack[T]) Pop() (T, bool) { return s.items.Pop() }

func Max[T cmp.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

type Pair[T any] struct{ a, b T }

// A generic method has type parameters of its own.
func (p Pair[T]) Apply[U any](f func(T) U) Pair[U] { return Pair[U]{f(p.a), f(p.b)} }

func main() {
	var st Stack[string]
	st.Push("a")
	st.Push("b")
	x, _ := st.Pop()
	fmt.Println(x, Max(3, 9), Max("a", "b"))
	fmt.Println(Pair[int]{1, 2}.Apply(func(n int) float64 { return float64(n) / 2 }))
}
```

```output
b 9 b
{0.5 1}
```

## 8. Enums, unions and packed types (sections 7.12, 7.13, 7.14)

- `enum`: a closed set of named integer constants. The first member is 0. Members are selected as
  `Color.Red` and bare in a `case`. No arithmetic, and untyped constants are not assignable (`var c Color = 1`
  is an error, E2315). Convert an integer with the comma-ok form `c, ok := Color(n)`. `len(Color)` and
  `for c := range Color` use the members. `String()` is generated.
- `union`: a tagged union, payloads inline, no allocation. Build with `Shape.Rect{w: 2, h: 3}`,
  `Shape.Dot`, and for a generic union `type Result[T any] union{ Ok T; Err string }` the instance's variant,
  `Result[int].Ok(5)`. Take apart with a type switch (`case Circle:` names the variant; `v` is its
  payload). The zero value is the **first** variant. A union with an owner payload must be `owner union`.
- `packed`: bit-exact layout for registers and headers; see `kernel-mode.md` for `volatile`, `ring` and more.

```pluto-run
package main

import "pluto/fmt"

type Color enum {
	Red
	Green
	Blue
}

func name(c Color) string {
	switch c { // no default: every member must be listed
	case Red:
		return "r"
	case Green:
		return "g"
	case Blue:
		return "b"
	}
}

type Shape union {
	Circle struct{ r float64 }
	Rect   struct{ w, h float64 }
	Dot
}

func area(s Shape) float64 {
	switch v := s.(type) {
	case Circle:
		return 3 * v.r * v.r
	case Rect:
		return v.w * v.h
	case Dot:
		return 0
	}
}

func main() {
	for c := range Color {
		fmt.Print(c, ":", name(c), " ")
	}
	n := 7
	if c, ok := Color(n); !ok { // a non-constant integer converts only in the comma-ok form
		fmt.Println("7 is not a color:", c)
	}
	fmt.Println(area(Shape.Rect{w: 2, h: 3}), area(Shape.Dot), Shape.Circle{r: 1})
}
```

```output
Red:r Green:g Blue:b 7 is not a color: Red
6 0 Circle({1})
```

## 9. Interfaces (section 7.9)

Interfaces and type switches work as in Go, with ownership rules:

- Converting an owner, counted box, channel or map to an ordinary interface (`any`, `error`, `fmt.Stringer`)
  **lends** it: the interface is a view of the variable and cannot outlive it. `fmt.Println(v)` therefore
  works on a `vec.Vec` without moving it.
- You cannot assert an owner back out of an ordinary interface (E3308). An *owning* interface
  (`type R owner interface{ ... }`) owns what it holds and moves.
- A value-receiver method of an owner type is not in the interface method set (it would consume the owner).
  Give interface methods pointer receivers and convert `&x`.
- A large value converted to an interface is copied into the converting function's frame, so returning that
  interface from the function is an error (E3001). Return a pointer to something that lives long enough, or a
  small error value (sections 7.9, 16.2).

```pluto-compile
package main

type Buf owner struct{ data []byte }

func (b *Buf) Len() int   { return len(b.data) } // in the method set of *Buf
func (b Buf) Finish() int { return len(b.data) } // consumes b: in no interface's method set

type Lener interface{ Len() int }

func show(b Buf) int {
	var l Lener = &b // a view of b; "var l Lener = b" is an error
	return l.Len() + b.Finish()
}

func main() { _ = show(Buf{}) }
```

## 10. Defer (section 12.19)

`defer` is as in Go with these rules: arguments are evaluated at the statement; an owner passed by value moves
there; `defer x.Drop()` or `defer x.Close()` (pointer receiver) saves `&x`; a deferred literal that uses an owner
counts as a use of it at every exit, so moving the owner later is E3301. A deferred call runs before the
function's variables are dropped. Each `defer` statement has one slot, so a `defer` inside a loop that a path can
reach again is **E2530**: move the loop body into a function (a loop that leaves right after the `defer`, such as
`mu.Lock(); defer mu.Unlock(); return v`, is fine). Only calls that may stand as a statement can be deferred as
built-ins (`clear`, `close`, `copy`, `delete`, `panic`, `print`, `println`; E2532 for the rest). A function has at
most 64 defers. There is no `recover`; on a panic the hosted runtime runs the pending deferred calls, then exits with
status 2. A worked example with `Drop` is in `ownership-and-memory.md`.

## 11. Closures and function values (sections 7.8, 11.4, 11.5)

A function literal that captures variables is a **view of its frame**: it is valid while the frame is. You can
call it, pass it down, return it (it is built in a region of the caller's frame, with no allocation) and defer it.
Storing it in a package variable or sending it to a task is an error (E3002, E2590), and keeping one that
captures a loop variable past its iteration is E3006. Pass values as arguments to a `task`.

## 12. Built-in functions (chapter 13)

| Built-in | Notes |
|---|---|
| `len`, `cap` | also `len(E)` of an enum (member count) and `len(x)` of any type with `Len() int` |
| `copy(dst, src)` | slices, or bytes from a string; `copy` of owner elements is E3306 |
| `clear(m)`, `clear(s)` | empties a map; zeroes a slice (drops owners) |
| `close(ch)`, `delete(m, k)` | as Go |
| `make(chan T, n)`, `make(map[K]V, n)` | an optional last argument is an allocator: `make(chan T, n, a)`; `make` of a slice is E2014 |
| `new(T)`, `new(v)` | returns `owned[T]`, not `*T` (an optional allocator comes last) |
| `take(&place)` | moves a value out of a field, element or pointer target, leaving the zero value |
| `min`, `max` | as Go 1.21; work on strings and enums too |
| `panic(v)` | ends the program; no `recover` |
| `print`, `println` | write to standard error, any integer, float, bool, string, pointer |
| absent | `append` (E2013), `recover` (E2004), `complex`, `real`, `imag` (E2006) |

## 13. Errors and the or clause (chapters 11.27, 16)

A failure is an `error` result. `x := f() or return` calls `f`; if its last result (which must be exactly `error`) is
not `nil`, the clause runs with `err` declared and must leave (`return`, `break`, `continue`, `goto`, `panic`, or a
block that ends in one). `return` alone passes `err` up unchanged; `return 0, err` and `return fmt.Errorf("...%w", err)`
are the forms with other results. Details and the error table: `errors-and-panics.md`.

```pluto-run
package main

import (
	"pluto/errors"
	"pluto/fmt"
	"pluto/strconv"
)

var ErrNegative = errors.New("negative")

func parse(s string) (int, error) {
	n := strconv.Atoi(s) or return 0, fmt.Errorf("parse %q: %w", s, err) // err is declared by the clause
	if n < 0 {
		return 0, ErrNegative
	}
	return n, nil
}

func sum(xs []string) (int, error) {
	total := 0
	for _, x := range xs {
		n := parse(x) or return 0, err
		total += n
	}
	return total, nil
}

func main() {
	fmt.Println(sum([]string{"1", "2"}))
	_, err := sum([]string{"1", "-5"})
	fmt.Println(err, errors.Is(err, ErrNegative))
	_, err = sum([]string{"x"})
	fmt.Println(err)
}
```

```output
3 <nil>
negative true
parse "x": strconv.Atoi: parsing "x": invalid syntax
```

## 14. Tasks and sync.Scope (sections 12.12, 19.1, 19.4)

`task f(x)` starts a task; its arguments are copied (owners move) and must be sendable. Tasks talk over channels.
A `sync.Scope` starts tasks that may use the caller's variables and joins them at `Wait` or at the end of the
scope; shared writes need one lock, an atomic, or disjoint indexes. More: `concurrency.md`.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/sync"
)

func square(n int, out chan<- int) { out <- n * n }

func main() {
	out := make(chan int)
	for i := 1; i <= 3; i++ {
		task square(i, out) // the arguments are copied into the task
	}
	sum := 0
	for i := 0; i < 3; i++ {
		sum += <-out
	}
	fmt.Println(sum)

	var mu sync.Mutex
	total := 0
	var s sync.Scope // tasks that may use main's variables, joined before s ends
	for i := 1; i <= 100; i++ {
		s.Task(func() {
			mu.Lock()
			total += i
			mu.Unlock()
		})
	}
	s.Wait()
	fmt.Println(total)
}
```

```output
14
5050
```

## 15. Numbers and where to go next

Integer arithmetic wraps on overflow, division by zero panics, floats never panic, there are no complex numbers,
and numeric conversions are explicit (section 11.24).

- Ownership, `owned`/`shared`/`synced`/`weak`, arenas: `ownership-and-memory.md`.
- Tasks, channels, `select`, `sync.Scope`, races: `concurrency.md`.
- Errors, `or` clauses and panics: `errors-and-panics.md`.
- Library names: `stdlib.md` and the other `stdlib-*.md` files.
- Translating Go: `go-to-plutonium.md`. Kernels: `kernel-mode.md`. Examples: `idioms.md`.
