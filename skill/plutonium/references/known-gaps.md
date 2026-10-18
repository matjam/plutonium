# Known gaps: where today's compiler and library differ from the specification

The specification (`docs/spec/language/`) is the source of truth, and the compiler and library are fixed to match
it. These are the differences found while writing this skill. Each is backed by a block that the skill's test
compiles and runs against the current compiler. **If a block here starts to fail, the gap has been closed**: delete
the item and let the specification's text stand. Until then, write code that works today and do not "fix" the
compiler's behavior in application code.

## 1. `vec.Vec`, `hash.Map` and `map` cannot hold owners

Spec sections 7.18, 10.9 and 18.5 say the containers are generic over owner types, so a `vec.Vec[Conn]` of owners moves
its elements, `Pop` and `Remove` take one out, and `hash.Map` holds owners in place. Today `vec.Vec[T any]` and
`hash.Map[K comparable, V any]` have type parameters that do not carry `owner`, so an owner element is E3207.
Workarounds: an array of owners (`[8]Conn`, ranged by index), `owned[Node]` boxes linked into a list or tree, or
`synced[T]`/`shared[T]` boxes as the element type (the container then copies counted references).

```pluto-error E3207
package main

import "pluto/vec"

type Conn owner struct{ id int }

func main() {
	v := vec.New[Conn]() // the spec allows this; today T does not admit owners
	_ = v
}
```

```pluto-error E3207
package main

import "pluto/hash"

type Conn owner struct{ id int }

func main() {
	m := hash.NewMap[string, Conn](4)
	_ = m
}
```

The library has no `hash.Map.Remove` or `Pop` (section F.3 names them); use `Delete(k)` for counted values.

```pluto-absent
hash.Map.Remove
hash.Map.Pop
```

## 2. Sending a `vec.Vec` on a channel is rejected when `pluto/fmt` is linked

Section 8.8 lists `vec.Vec`, `hash.Map` and `strings.Builder` as sendable owners, and sections 12.5 and 19.8 send
a `vec.Vec[int]` on a channel. Today the race analysis sees the owner's allocator field (the default allocator,
`*alloc.SyncSlab`) as memory that may change, and E3501 rejects the send **whenever `pluto/fmt` is in the program**
(without `fmt` the same program builds). Send plain values, strings, structs of values, or `owned[T]` of a plain
struct; or keep the vector in a `synced` object with a lock.

```pluto-error E3501
package main

import (
	"pluto/fmt"
	"pluto/vec"
)

func produce(out chan<- vec.Vec[int]) {
	v := vec.New[int]()
	v.Push(1)
	out <- v
	close(out)
}

func main() {
	ch := make(chan vec.Vec[int], 2)
	task produce(ch)
	t := 0
	for v := range ch {
		t += v[0]
	}
	fmt.Println(t)
}
```

## 4. A string made in a local arena and passed through `any` panics at return

Section 18.8 says a string built while a local arena is the current allocator is a view of the arena and the
compiler checks it. Passing such a string to `fmt.Println` (or any `...any` call) in the function that owns the
arena converts it to an interface, which holds a counted reference in the function's frame until the function
returns, after the arena was dropped. The program builds and **panics at return** with
`use of memory from an arena that was reset`. Print arena-backed text with `os.Stdout.Write(b.Bytes())`, or print
a length, or copy it to a heap string first.

```pluto-run exit=2
package main

import (
	"pluto/arena"
	"pluto/fmt"
	"pluto/strings"
)

func handle() int {
	a := arena.NewFixed(16 << 10)
	var out strings.Builder
	out.Init(&a)
	out.WriteString("hello")
	fmt.Println(out.String()) // the string's reference is released after a is dropped
	return 1
}

func main() {
	fmt.Println(handle())
}
```

```output
hello
```

## 5. `os.File` is a pointer, not an owner

Appendix A.3 and section 18.13 describe `os.Open` returning an `os.File` that closes when its variable goes out of
scope. Today `os.Open`, `os.Create` and `os.OpenFile` return `*os.File`: close it yourself (`defer f.Close()`),
since the pointer's end of scope closes nothing.

```pluto-compile
package main

import "pluto/os"

func main() {
	f, err := os.Open("/dev/null")
	if err != nil {
		return
	}
	var p *os.File = f // a pointer, so storing it in a *os.File variable compiles
	defer p.Close()
}
```

## 6. Error codes that differ from the text

- `var c Color = 1` (an untyped constant assigned to an enum) reports E0023 (IncompatibleAssign), not E2315 as
  section 7.12 says.
- `import "reflect"` in a program without a `pluto.mod` reports E4007 (only the standard library can be imported),
  not E2005 (section 14.3).

```pluto-error E0023
package main

type Color enum {
	Red
	Green
}

func main() {
	var c Color = 1
	_ = c
}
```

## 7. Arenas and waiting locks in freestanding builds

Section 19.5 says a program whose dependencies request no waiting contains no scheduler. Today importing
`pluto/arena` in a freestanding or kernel build (even for `arena.NewUnsafe` alone) makes the program need a bound
scheduler, and the build fails with E2593 until a `//pu:scheduler` variable exists. A kernel that allocates from an
arena binds its scheduler first (see `testdata/kernel/sched`), or allocates from its own `alloc.Allocator`.

```pluto-error E2593 mode=kernel
package main

import (
	"pluto/arch/x86"
	"pluto/arena"
	"pluto/rt"
)

//pu:panic
func onPanic(p *rt.Panic) {
	for {
		x86.Hlt()
	}
}

var heap [1 << 16]byte

func main() {
	a := arena.NewUnsafe(heap[:])
	_ = a
}
```

## 8. Smaller notes

- When a struct literal is assigned to an interface whose method has a pointer receiver (E0021), the compiler's fix
  suggests `&T{...}`. That is right inside a function that only uses the value, and E3001 in a constructor that
  returns it: build the node with `new(T{...})` and, for a tree of interface values, declare the interface `owner`
  (`idioms-types-memory.md` #9).
- `pluto vet` has one check, `-unsafe`; there is no general lint. `pluto fmt` is the only style tool.
- Library names that Go has and Plutonium lacks are listed in `go-to-plutonium.md`.
