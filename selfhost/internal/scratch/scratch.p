// Package scratch is the self-hosted compiler's internal/scratch: Do runs
// a step with an arena as the default allocator and frees the arena's
// memory when the step's results have been copied out. Hand-written; go2pu
// keeps it in place of a translation of scratch.go.
//
// With PLUTO_SCRATCH_CHECK set, the arena's memory comes from pages that
// are never reused: when a step ends they are freed and made inaccessible,
// so a pointer into a step's memory that outlives the step faults where it
// is used, with a stack trace, instead of reading memory a later step
// reuses.
package scratch

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/os"
	"pluto/rt"
	"selfhost/compat"
	"unsafe"
)

// chunkSize is the size of the arenas' chunks. Released chunks of this
// size stay with an arena for the next step at its level; larger ones go
// back to the heap.
const chunkSize = 1 << 20

// maxDepth is how deeply Do nests: a phase of the compiler run as a step,
// and steps within it.
const maxDepth = 3

// A level is the state of the Do running at one nesting depth. Each level
// has its own arena, and in check mode its own pages, so that the end of
// an inner step frees and seals only that step's memory.
type level owner struct {
	scr   arena.Chunked
	ready bool
	outer alloc.Allocator // the allocator current when the Do began
	pages checkPages
}

var (
	levels [maxDepth]level
	depth  int  // Do's whose work is running
	heap   alloc.Allocator // the allocator of the first Do, parent of the arenas
	check  bool // PLUTO_SCRATCH_CHECK
	inited bool
)

// A Step is a step of the compiler run by Do. Its methods' names are
// unique to it: the goexit analysis (internal/lower/goexit.go) takes an
// interface call to reach every method of its name and signature, and a
// call through a func() value to reach every func() literal, so Step keeps
// the step's code from widening either set.
type Step interface {
	// ScratchWork does the step's work, allocating from scratch memory.
	ScratchWork()
	// ScratchKeep copies out of scratch memory what ScratchWork made that
	// must outlive the step, allocating from the caller's allocator.
	ScratchKeep()
}

// Do runs s.ScratchWork with an arena as the default allocator, then
// s.ScratchKeep with the caller's, and then frees everything ScratchWork
// allocated. Do nests up to maxDepth deep: a Do within another's work
// uses an arena of its own, and its ScratchKeep copies into the outer
// step's memory.
func Do(s Step) {
	if !inited {
		check = os.GetenvLossy("PLUTO_SCRATCH_CHECK") != ""
		heap = alloc.Default()
		inited = true
	}
	if depth == maxDepth {
		panic("scratch.Do nested too deeply")
	}
	l := &levels[depth]
	if !l.ready {
		if check {
			l.scr = arena.NewChunked(chunkSize, &l.pages)
		} else {
			l.scr = arena.NewChunked(chunkSize, heap)
		}
		l.ready = true
	}
	l.outer = alloc.Default()
	m := l.scr.Mark()
	depth++
	r := alloc.Use(&l.scr)
	s.ScratchWork()
	r.Restore()
	depth--
	l.outer = nil
	s.ScratchKeep()
	l.scr.Release(m)
	if check {
		l.scr.Trim()
		l.pages.seal()
	}
}

// Trim returns to the heap the spare memory that the arenas keep for later
// steps, after a large step whose like does not follow.
func Trim() {
	for i := depth; i < maxDepth; i++ {
		if levels[i].ready {
			levels[i].scr.Trim()
		}
	}
}

// Outer makes the allocator that was current when the innermost running
// Do began the current one until Restore is called, so that memory a step
// adds to state outliving it (a memo table, an interning table, a pool)
// does not come from that step's scratch memory. Outside a Do's work it
// changes nothing.
func Outer() alloc.Restore {
	if depth > 0 {
		return alloc.Use(levels[depth-1].outer)
	}
	return alloc.Use(alloc.Default())
}

// Heap makes the heap the current allocator until Restore is called, so
// that a cache filled lazily by whichever step first asks (the type
// checker's expanded methods and underlying types) does not come from any
// step's scratch memory.
func Heap() alloc.Restore {
	if depth > 0 {
		return alloc.Use(heap)
	}
	return alloc.Use(alloc.Default())
}

// String returns a copy of s from the current allocator.
func String(s string) string { return compat.Concat(s) }

// Error returns an error with err's text whose memory comes from the
// current allocator, or nil.
func Error(err error) error {
	if err == nil {
		return nil
	}
	return compat.NewError(String(err.Error()))
}

// checkPages is the arena's parent allocator in check mode: it hands out
// memory from regions it maps, never reusing any, and seal frees and makes
// inaccessible everything handed out since the last seal.
type checkPages struct {
	base  unsafe.Pointer // the region being filled, or nil
	used  uintptr
	size  uintptr
	spans [checkSpans]checkSpan // handed out since the last seal
	n     int
}

type checkSpan struct {
	p unsafe.Pointer
	n uintptr
}

const (
	checkRegion = 64 << 20
	checkSpans  = 1 << 16
	pageSize    = 4096
)

func (c *checkPages) Alloc(size, align uintptr) unsafe.Pointer {
	if align < 16 {
		align = 16
	}
	if size > checkRegion/4 || align > pageSize {
		n := (size + pageSize - 1) &^ (pageSize - 1)
		p := rt.Mmap(n)
		if p != nil {
			c.add(p, n)
		}
		return p
	}
	off := (c.used + align - 1) &^ (align - 1)
	if c.base == nil || off+size > c.size {
		c.close()
		c.base = rt.Mmap(checkRegion)
		if c.base == nil {
			return nil
		}
		c.size, off = checkRegion, 0
	}
	c.used = off + size
	return unsafe.Add(c.base, off)
}

// Free does nothing: memory is freed by seal.
func (c *checkPages) Free(p unsafe.Pointer, size, align uintptr) {}

// close ends the region being filled: the pages handed out from it are
// recorded for seal, and the rest is unmapped.
func (c *checkPages) close() {
	if c.base == nil {
		return
	}
	u := (c.used + pageSize - 1) &^ (pageSize - 1)
	if u < c.size {
		rt.Munmap(unsafe.Add(c.base, u), c.size-u)
	}
	if u > 0 {
		c.add(c.base, u)
	}
	c.base, c.used, c.size = nil, 0, 0
}

func (c *checkPages) add(p unsafe.Pointer, n uintptr) {
	if c.n == len(c.spans) {
		panic("scratch: too many regions in one step (PLUTO_SCRATCH_CHECK)")
	}
	c.spans[c.n] = checkSpan{p, n}
	c.n++
}

// seal frees the pages handed out since the last seal and makes them
// inaccessible.
func (c *checkPages) seal() {
	c.close()
	for i := 0; i < c.n; i++ {
		s := c.spans[i]
		rt.Discard(s.p, s.n)
		if !rt.Mprotect(s.p, s.n, rt.ProtNone) {
			panic("scratch: mprotect failed (PLUTO_SCRATCH_CHECK)")
		}
	}
	c.n = 0
}
