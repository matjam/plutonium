# Known gaps: where today's compiler and library differ from the specification

The specification (`docs/spec/language/`) is the source of truth, and the compiler and library are fixed to match
it. These are the differences found while writing this skill. Each is backed by a block that the skill's test
compiles and runs against the current compiler. **If a block here starts to fail, the gap has been closed**: delete
the item and let the specification's text stand. Until then, write code that works today and do not "fix" the
compiler's behavior in application code.

## 5. Arenas and waiting locks in freestanding builds

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

## 6. Smaller notes

- `pluto vet` has one check, `-unsafe`; there is no general lint. `pluto fmt` is the only style tool.
- Library names that Go has and Plutonium lacks are listed in `go-to-plutonium.md`.
