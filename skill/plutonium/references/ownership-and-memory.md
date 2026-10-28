# Ownership and memory

There is no garbage collector. Every piece of memory has one of: frame or static storage, one **owner**,
**counted** references, or an **arena**. The compiler checks this with no annotations except the word `owner` on
a type declaration. Full rules: spec chapter 18 (`docs/spec/language/18-ownership-and-memory.md`).

## 1. Choosing the type

| I need | Write | Kind |
|---|---|---|
| a number, flag, small struct | the plain type | value |
| text | `string` (copies freely, counted) | counted |
| a sequence that grows | `vec.Vec[T]` (`vec.New[T]()`, `vec.Make[T](n)`, `vec.WithCapacity[T](n)`) | owner |
| a sequence of fixed size | `[N]T`, lent as `a[:]` | value |
| a bounded buffer that never allocates | `vec.NewFixed(buf[:])` over an array | view |
| one object on the heap that must keep its address, or a tree/list node | `new(T{...})` or `alloc.Owned[T]()`, an `owned[T]` | owner |
| one object several places keep, one task | `alloc.Shared[T]()`, a `shared[T]` | counted |
| one object several tasks keep | `alloc.Synced[T]()`, a `synced[T]` | counted |
| a back pointer that must not keep the parent alive | `weak[shared[T]]` or `weak[synced[T]]` | counted |
| a table | `map[K]V` (counted reference, keys and values not owners) or `hash.Map[K, V]` (owner) | counted / owner |
| something that must be released (file, lock, handle) | a struct declared `owner` with a `Drop` method | owner |
| to let a function look at something | `*T`, `[]T`, `&x`, `v.Slice()` (a view) | view |

## 2. Owners move

Assigning, passing by value, returning, sending on a channel, storing in a composite literal, converting to an
owning interface and `new(v)` **move** an owner: the source variable holds nothing afterwards and the compiler
rejects its use until it is assigned again (E3301; E3302 when only some path moved it, such as an earlier loop
iteration). Reading does not move: `&x`, pointer-receiver method calls, field reads, `len(x)`, `==`, converting to
`any`/`error` (which lends), `_ = x`.

- Borrow with a pointer: `func use(c *Conn)` called as `use(&c)`. Consume with a value: `func finish(c Conn)`.
- Copy explicitly with `Clone()` (`vec.Vec`, `hash.Map` and `owned[T]` of a non-owner `T` have one).
- Move only out of a **whole variable**. Out of a field, element, pointer target or package variable is E3303:
  use `take(&place)`, which returns the value and leaves the zero value (which drops to nothing).
- One expression may not move a variable and also use it: `send(buf, buf.Len())` is an error; compute `n` first.
  Assigning the result back is not a use: `v = grow(v)` is fine. A pointer-receiver method may be called on a
  temporary owner (`readAll(f).Slice()`); the temporary and any view of it end with the statement.
- `for _, x := range xs` over owners copies each element: E3304. Use `for i := range xs` and `&xs[i]`.
- Owner elements in `copy` (E3306), variadic arguments (E3310) and slice literals (E3312) are errors for the same
  reason; a captured owner cannot be moved inside a closure (E3305): pass it as an argument.
- A moved owner is dropped by its new holder. A value-receiver method on an owner consumes it.

```pluto-run
package main

import "pluto/fmt"

type Frame owner struct {
	next owned[Frame]
	val  int
}

type Stack owner struct{ top owned[Frame] }

func (s *Stack) Push(v int) {
	f := new(Frame{val: v}) // an owned[Frame]
	f.next = take(&s.top)   // move the old top out of the field
	s.top = f               // move the new frame in
}

func (s *Stack) Pop() owned[Frame] {
	f := take(&s.top)
	if f != nil {
		s.top = take(&f.next)
	}
	return f // moves to the caller
}

func main() {
	var s Stack
	s.Push(1)
	s.Push(2)
	f := s.Pop()
	fmt.Println(f.val)
	g := s.Pop()
	h := s.Pop() // an empty box: nil
	fmt.Println(g.val, h == nil)
}
```

```output
2
1 true
```

The usual errors, each a complete program:

```pluto-error E3301
package main

import "pluto/vec"

func main() {
	a := vec.New[int]()
	b := a // a moves to b
	a.Push(1)
	_ = b
}
```

```pluto-error E3302
package main

import "pluto/vec"

func eat(v vec.Vec[int]) {}

func main() {
	a := vec.New[int]()
	for i := 0; i < 2; i++ {
		eat(a) // moved in the previous iteration
	}
}
```

```pluto-error E3303
package main

import "pluto/vec"

type Holder owner struct{ v vec.Vec[int] }

func steal(h *Holder) vec.Vec[int] { return h.v } // use take(&h.v)

func main() {
	var h Holder
	_ = steal(&h)
}
```

```pluto-error E3304
package main

import "pluto/vec"

func main() {
	xs := [2]vec.Vec[int]{}
	for _, x := range xs { // copies an owner; range over the indexes
		_ = x
	}
}
```

```pluto-error E3301
package main

import "pluto/vec"

func eat(v vec.Vec[int]) {}

func main() {
	v := vec.New[int]()
	defer func() { println(v.Len()) }() // runs when main returns and uses v
	eat(v)                              // moves v first: use after move
}
```

```pluto-error E3305
package main

import "pluto/vec"

func eat(v vec.Vec[int]) {}

func main() {
	a := vec.New[int]()
	f := func() { eat(a) } // the literal may run twice, or never
	f()
}
```

```pluto-error E3306
package main

import "pluto/vec"

func main() {
	var xs, ys [2]vec.Vec[int]
	copy(xs[:], ys[:]) // would make a second owner of every element
}
```

```pluto-error E3308
package main

import "pluto/vec"

func main() {
	var x any = vec.New[int]() // lends the vector to x
	v := x.(vec.Vec[int])      // a second owner: refused
	_ = v
}
```

## 3. Declaring owner types

```go
type File owner struct{ fd int32 }          // moves; may have Drop
func (f *File) Drop() { closeFD(f.fd) }      // pointer receiver, no parameters, no results (E3201, E3206)
type Frame owner uintptr                     // an integer that is a resource
type Log owner File                          // dropping a Log runs File's drop glue
```

- A struct with an owner field, an array of owners, an embedded owner or an `owned[T]` **must** be declared `owner`
  (E3202); an unnamed struct cannot have owner fields (E3203). A generic type that stores `vec.Vec[T]` or
  `owned[T]` is declared `type Stack[T any] owner struct{...}`.
- `owner` may mark a struct, array, integer, pointer, interface (owning interface), union, `ring` or `aligned`
  type, not a string, slice, map, channel, function, bool, float, enum, packed or `volatile` type (E3204), and
  not an alias (E3205).
- `Drop` must treat the zero value as empty: a moved-from variable, a `take` result and zeroed memory all hold it.
  A variable known to hold nothing is not dropped at all. `Drop` cannot report failure: give the type a
  `Close() error` for callers that care.
- A type parameter admits an owner only if declared `[T owner any]` (E3207). The standard library's other type
  parameters never take owners: `hash.Set` keys, `hash.Map` keys and `vec.Fixed` elements are copied. Keep owners
  in a `vec.Vec`, or as the values of a `hash.Map`.
- Generic code over `[T owner any]` whose range over a `vec.Vec[T]` (or a `hash.Map`'s values) binds the element
  gets `*T`. This compiler rejects its instance with a value type, such as `F[int]`, with E2500 for now; a range
  that binds only the index is fine. Code meant for both kinds of type takes pointers by index instead:
  `for i := range v.Len() { p := v.Ptr(i) }`.
- A built-in map cannot hold owners as key or value (E3213): hold `synced[T]` or `shared[T]` boxes, or use a
  `hash.Map[K, V]`, whose values may be owners. `vec.Vec[T]` and `hash.Map[K, V]` own their elements: `Push`, `Set`
  and `m[k] = v` move one in (dropping the one replaced), `Pop`, `Remove` (`m.Remove(k)`) and `m.Pop()` move one
  out, `Ptr` lends one, a `range` yields pointers to the elements, and dropping the container drops the elements in
  order. The methods that copy an element (`At`, `v[i]`, `Last`, `Get`, `Clone`) do not compile for owners.
- Converting a plain value into an owner type is allowed only in the package that declares it (E3212).

## 4. Drops

An owner is dropped when its variable's scope ends on every path (fall off, `return`, `break`, `continue`,
`goto`), in **reverse order of declaration**; parameters after locals. A deferred call runs first; a deferred function literal that uses an owner counts as a use at every
exit, so moving or dropping that owner later in the function is E3301 or E3302. Results move out,
they are not dropped. Assigning to a place that holds an owner drops the old value first. A temporary
(`_ = open()`, `makeVec().Slice()`) is dropped at the end of its statement. Early drop: `x.Drop()` on a whole
owner variable works for every owner type, with or without a `Drop` method; `defer x.Drop()` saves `&x` and drops
whatever `x` holds when it runs. Calling `Drop` through a pointer or an interface is E3307.

A panic drops nothing (the hosted runtime first runs deferred calls); `os.Exit` drops nothing; `rt.ExitTask()`
unwinds the task's frames like `return`. When `main` returns after every task has retired (a `sync.Scope`
join guarantees it; a `WaitGroup` or channel join makes it likely, not certain, D31), package-level owners are
dropped in reverse initialization order and the debug allocator reports leaks; if tasks still run the program just
ends, as in Go, after a wall-clock grace of about a millisecond. Release what must be released on every exit
with `defer` in `main`.

```pluto-run
package main

import "pluto/fmt"

type File owner struct{ name string }

func (f *File) Drop() { fmt.Println("closed", f.name) }

func open(name string) File { return File{name: name} }

func main() {
	a := open("a")
	defer a.Drop() // saves &a; closes a at the end of main
	b := open("b")
	b.Drop() // early drop; b is not dropped again
	fmt.Println("b dropped early")
	_ = open("temp") // a temporary: dropped at the end of the statement
	fmt.Println("end")
}
```

```output
closed b
b dropped early
closed temp
end
closed a
```

## 5. Views and where they may go

A view (`*T`, `[]T`, a closure that captures, `v.Slice()`, an interface holding a local) is valid only while what
it looks at is. Rules (section 18.3):

- Passing a view **down** into a call is always fine, and a function may return a view derived from its
  parameters (`return b[4:]`).
- A view of the **frame** (a local variable, local array, `&T{...}`, a capturing closure, an interface copy of a
  large value) may not be returned (E3001), stored in a package variable or through a pointer the function
  received (E3002), passed to a function that keeps it (E3003), passed to a `task` statement (E3004 or E2011) or
  sent on a channel (E3005 or E2011). Fixes: return an owner (`vec.Vec`, `new(...)`), copy the data, or make the
  data live longer (package variable, caller-provided buffer).
- A view of a **local owner's** memory (`v.Slice()`, `&*p`, `s.top.val`) cannot be used after the owner is
  moved, dropped or reassigned (E3313), and an owner that holds a view of another local must be declared after it
  so it drops first. A view of a temporary owner (`makeVec().Slice()`) dies at the end of the statement (E3009).
- The compiler follows a view through local variables, their fields and the calls that store it into other
  arguments. A view stored anywhere it cannot follow (through a pointer, in a variable that a closure uses or
  whose address is kept, by a call it cannot see into) forbids moving, dropping or reassigning the owner while the
  view could still be used: E3314. Keep such views in local variables and end their use before the owner changes.
- A **loop** reuses its variables: keeping a view of an iteration's variable or a closure that captures it where
  the next iteration can see it is E3006.
- Views into heap memory reached through other heap memory are checked at run time: a use after the memory was
  freed panics with `use of freed memory` (generation checks; `pluto build --gencheck=false` turns them off).

```pluto-error E3001
package main

func first(n int) []int {
	var a [4]int
	return a[:n] // a view of a, which dies with this call
}

func main() { _ = first(2) }
```

```pluto-error E3002
package main

var keep *int

func main() {
	x := 1
	keep = &x // x lives in main's frame, keep lives forever
}
```

```pluto-error E3003
package main

var keep *int

func remember(p *int) { keep = p }

func main() {
	x := 1
	remember(&x) // remember keeps its argument
}
```

```pluto-error E3006
package main

func main() {
	var fs [3]func() int
	for i := 0; i < 3; i++ {
		fs[i] = func() int { return i } // the next iteration reuses i
	}
	_ = fs
}
```

```pluto-error E3313
package main

import "pluto/vec"

func main() {
	v := vec.New[int]()
	v.Push(1)
	s := v.Slice() // a view of v's memory
	w := v         // v moves; s is stale
	_ = w
	_ = s[0]
}
```

```pluto-error E3202
package main

import "pluto/vec"

type Stack struct{ items vec.Vec[int] } // must be: type Stack owner struct

func main() { var s Stack; _ = s }
```

```pluto-error E3207
package main

import "pluto/vec"

func Dup[T any](x T) (T, T) { return x, x } // copies; use [T owner any] to move

func main() {
	Dup(vec.New[int]())
}
```

```pluto-error E3213
package main

import "pluto/vec"

func main() {
	m := map[string]vec.Vec[int]{} // an owner element; use a synced[T] element
	_ = m
}
```

## 6. Boxes: `owned`, `shared`, `synced`, `weak`

`new(T{...})`, `new(v)` and `alloc.Owned[T]()` return `owned[T]`, **not** `*T`; storing it in a `*T` is an error.
Boxes dereference like `*T` (`p.field`, `*p`, `p[i]`, `len(p)`, methods). `(*T)(p)` or `&*p` lends a pointer,
bounded by the box. `p.Clone()` copies the object (non-owner `T`); `p.Drop()` drops it early. Boxes compare by
address and with `nil`. A box cannot hold an interface type (E3210).

- `owned[T]`: one owner; sendable if `T` is (sending moves).
- `shared[T]`: reference count, **one task only** (not sendable: E2010). Copy it to add a reference.
- `synced[T]`: atomic count, sendable. The object's fields that tasks change need a lock of the same object or
  atomics (race rules, `concurrency.md`). `task obj.Method()` on a `synced` box gives the task its own reference.
- `weak[P]` (`P` a `shared`/`synced` box): `weak[shared[T]](p)` makes one; `w.Upgrade()` returns `(P, bool)`.
  Use it for parent/back pointers: strong cycles are never freed.
- Strings, maps and channels are counted too; their counts become atomic once the first task starts.

```pluto-run
package main

import (
	"pluto/alloc"
	"pluto/fmt"
)

type Tree owner struct {
	left, right owned[Tree]
	key         int
}

func build(depth int) owned[Tree] {
	t := alloc.Owned[Tree]()
	t.key = depth
	if depth > 0 {
		t.left = build(depth - 1) // moves into the field
		t.right = build(depth - 1)
	}
	return t // moves to the caller
}

func count(t *Tree) int {
	if t == nil {
		return 0
	}
	return 1 + count((*Tree)(t.left)) + count((*Tree)(t.right)) // a box lends a *Tree
}

type Item struct {
	name string
	hits int
}

func main() {
	t := build(3)
	fmt.Println(count(&*t)) // 15 nodes, freed when t goes out of scope

	p := new(Item{name: "x"})
	p.hits++
	q := p.Clone() // a second box with a copy of the object
	q.hits = 10
	fmt.Println(p.hits, q.hits, *p)

	a := alloc.Shared[Item]()
	a.name = "shared"
	b := a // the strong count is now 2
	b.hits = 5
	w := weak[shared[Item]](a)
	fmt.Println(a.hits, b.name)
	if s, ok := w.Upgrade(); ok {
		fmt.Println("alive", s.hits)
	}
}
```

```output
15
1 10 {x 1}
5 shared
alive 5
```

## 7. Allocators and arenas (section 18.9)

Every allocating operation takes memory from the **current allocator**: the one `alloc.Use` installed for this
task, else the program-wide default (hosted builds start with a thread-safe one; freestanding builds have none
until `alloc.SetDefault`). `new`, `make(chan/map)` and the library constructors take an explicit allocator as
the last argument (`new(T, &a)`, `vec.New[int](&a)`, `make(map[K]V, n, &a)`; library functions declare it as the
defaulted parameter `a alloc.Allocator = alloc.Default()`); variadic functions such as `fmt.Sprintf`, and the
operations that make strings (`+`), use the current allocator, which `alloc.Use` replaces. Owners remember the
allocator they came from and free to it, so an owner built in an arena drops normally and its free costs nothing.

- `arena.NewFixed(capacity)` is an owner with fixed private backing. `arena.NewChunked()` grows from its parent.
  `a.New[T]()` returns a `*T` and `a.Slice[T](n)` a `[]T`, valid until `a.Reset()`, `a.Release(mark)` or `a` is
  dropped. Using a view of a local arena after its `Reset` or `Release` is E3313; where the compiler cannot
  tell which arena was reclaimed (through a pointer), the stale use panics at run time. A mark is not a view. The values in arena memory are not
  dropped for you.
- `defer alloc.Use(&a).Restore()` makes `a` the current allocator for the rest of the function. Anything allocated
  in that window is a **view of the arena**: returning it or storing it past the arena is E3001. Allocate what must
  survive from `alloc.Default()` captured before `Use`, or from an arena the caller passes.
- Every task starts with the program-wide default allocator, not its starter's override. An arena is not safe for
  several tasks: give each task its own.
- `arena.NewUnsafe(buf)` is the manual, unchecked borrowed-buffer arena (unsafe code).
- Text made in a local arena (by a `strings.Builder` started with `Init(&a)`, or any string made in the `alloc.Use`
  window) is a view of the arena. Converting it to an interface (`fmt.Println(b.String())`, any `any` parameter) in
  the function that owns the arena is E3313: the interface's copy holds the string until the function returns,
  after the arena is dropped. Print such text with `os.Stdout.Write(b.Bytes())`, or copy it to a string from
  the default allocator before the arena is installed or after it is gone (`strings.FromUTF8Lossy(b.Bytes())`
  outside a `Use` window).
- Debug: `pluto build --alloc=debug` (and `pluto run --alloc=debug`) runs with the checking allocator: guard pages,
  canaries, and at exit a report of the allocations still live (exit status 2 when there are leaks).

```pluto-run
package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
	"pluto/vec"
)

type Field struct{ key, val string }

func fields() int {
	a := arena.NewFixed(16 << 10) // private fixed-capacity backing
	fs := vec.New[Field](&a)      // allocates from a, so it holds a view of a
	fs.Push(Field{"a", "1"})
	fs.Push(Field{"b", "2"})
	return fs.Len() // fs, then a, are dropped here; returning fs would be E3001
}

func scoped() int {
	a := arena.NewChunked()       // grows from the default allocator
	defer alloc.Use(&a).Restore() // new, make, strings and Sprintf now allocate in a
	s := fmt.Sprintf("n=%d", 5)   // a string in a: a view of a
	return len(s)                 // return a number, not s
}

func main() {
	fmt.Println(fields(), scoped())

	a := arena.NewFixed(1024)
	p := a.New[int]() // a *int into the arena, valid until a is reset or dropped
	*p = 4
	xs := a.Slice[int](3)
	xs[1] = *p
	m := a.Mark()
	ys := a.Slice[int](100)
	ys[0] = 1
	n := len(ys)
	a.Release(m) // reclaims ys only: using ys after this is E3313
	fmt.Println(xs, n, a.Used() < 1024)
	a.Reset() // reclaims everything; a view used past this is E3313
}
```

```output
2 3
[0 4 0] 100 true
```

## 8. Unsafe code

`import "unsafe"`, `pluto/arch/x86`, `alloc.Raw/New/Free`, `arena.NewUnsafe`, bodiless functions, `rt.Pinned` and
`//pu:nochecks` are the unsafe surface (section 18.12). Application code needs none of it. `pluto vet -unsafe`
lists every unsafe function and the safe code that calls it. `unsafe.Drop(p)` drops `*p` without freeing it.

## 9. Error code cheat sheet

| Code | Meaning | Usual fix |
|---|---|---|
| E3001-E3005 | a view of the frame is returned, stored, passed to a keeper, started as a task, sent | return an owner; copy; use `sync.Scope`; allocate with `new` |
| E3006 | a view or closure of a loop variable outlives its iteration | use the value inside the iteration; pass it as an argument |
| E3009 | a view of a temporary owner is stored | name the owner in a variable first |
| E3100-E3102 | a returned closure cannot be placed in the caller's region: it is built in a nested literal (E3100), kept past a loop iteration or chained in a loop, `it = Filter(it, f)` (E3101), or built by a recursive function (E3102) | make the closure in the function that returns it and return it by value; compose a fixed number of closures; keep state in a value the caller owns |
| E3201, E3206 | `Drop` on a non-owner, or wrong signature | `func (t *T) Drop()` on a type declared `owner` |
| E3202, E3203 | owner field in a struct not declared `owner` | add `owner` to the declaration |
| E3207 | owner passed for a type parameter that copies | declare `[T owner any]` |
| E3212 | converting a value to a foreign owner type | call the package's constructor |
| E3213 | owner in a map key or value | `synced[T]` values |
| E3301, E3302 | use after a move | borrow with `&x`, `Clone()`, or reorder |
| E3303 | moving out of a field, element or pointer | `take(&place)` |
| E3304, E3306, E3310, E3312 | copying owners (range, `copy`, variadic, slice literal) | indexes and `&xs[i]`; move one by one |
| E3305 | moving a captured owner in a closure | pass it as an argument |
| E3307 | `Drop` through a pointer or interface | drop the owner variable itself |
| E3308, E3309 | owner taken from an interface or a container's `Index` | assert to an interface; use `Ptr`, `Pop`, `Remove` |
| E3313, E3314 | a view of an owner used after the owner moved or changed (E3314: the view was stored where the compiler cannot follow it) | order the statements, copy, or keep the view in a local variable |
| E3401-E3407 | scoped task rules | see `concurrency.md` |
| E3501-E3503 | data races | see `concurrency.md` |

`pluto explain E3301` prints the long explanation with an example. `diagnostics.md` lists every code.
