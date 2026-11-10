package arena

import (
	"pluto/rt"
	"unsafe"
)

// Drop entries (language spec section 18.9, Drop entries; abi.md section
// 1.3). New and Slice of a Fixed or Chunked record one for a type with drop
// glue, and Reset, Release and Drop run them, last first, before they check
// the count of live allocations.

// A dropEntry is the address of n values and the function that drops
// them. A nil fn is an entry already taken off the table.
type dropEntry struct {
	p  unsafe.Pointer
	n  uintptr
	fn func(unsafe.Pointer, uintptr)
}

// dropValues drops the n values of type T at p in place, last first, and
// leaves the zero value in each place, as an entry does.
func dropValues[T owner any](p unsafe.Pointer, n uintptr) {
	s := unsafe.Slice((*T)(p), int(n))
	for i := len(s) - 1; i >= 0; i-- {
		unsafe.Drop(&s[i])
		rt.ZeroValues(&s[i], 1)
	}
}

// dropNone is the function of an entry already taken off its table. It
// keeps nothing, and the escape analysis counts it among the functions an
// entry may call, so a program with no entries still has a known one.
func dropNone(p unsafe.Pointer, n uintptr) {}

// runEntry takes e off its table and runs it.
func runEntry(e *dropEntry) {
	fn, p, n := e.fn, e.p, e.n
	if fn == nil {
		fn = dropNone
	}
	e.fn = nil
	fn(p, n)
}

// entriesPerBlock is the number of entries in one block of a Chunked.
const entriesPerBlock = 15

// An entryBlock holds drop entries of a Chunked. It is allocated in the
// arena's own chunks, and the blocks are linked newest first.
type entryBlock struct {
	prev *entryBlock
	used int
	e    [entriesPerBlock]dropEntry
}
