// Package alloc provides heap allocation: the default allocator, a slab
// allocator, and typed allocation functions.
//
// Nothing in Plutonium allocates unless asked. new and make allocate from
// the default allocator (or one passed explicitly); so do the functions here.
// In hosted builds the default is a slab allocator over mmap. Freestanding
// builds start without one: a kernel sets it once its page allocator works.
package alloc

import (
	"pluto/rt"
	"unsafe"
)

// An Allocator provides zeroed memory and takes it back.
type Allocator = rt.Allocator

// Default returns the current default allocator, or nil.
func Default() Allocator { return rt.DefaultAllocator() }

// SetDefault replaces the default allocator for all later allocations.
func SetDefault(a Allocator) { rt.SetDefaultAllocator(a) }

// A Restore puts back the allocator that was the default before Use.
type Restore struct {
	prev Allocator
}

// Use makes a the default allocator until the returned Restore's Restore
// method is called:
//
//	defer alloc.Use(arena).Restore()
func Use(a Allocator) Restore {
	r := Restore{prev: Default()}
	SetDefault(a)
	return r
}

// Restore reinstates the previous default allocator.
func (r Restore) Restore() { SetDefault(r.prev) }

// New allocates a zeroed T from the default allocator.
func New[T any]() *T {
	var zero T
	return (*T)(must(Default(), unsafe.Sizeof(zero), unsafe.Alignof(zero)))
}

// NewIn allocates a zeroed T from a.
func NewIn[T any](a Allocator) *T {
	var zero T
	return (*T)(must(a, unsafe.Sizeof(zero), unsafe.Alignof(zero)))
}

// Free returns p, allocated by New, to the default allocator. If T has a
// Drop method it is called first.
func Free[T any](p *T) { FreeIn(Default(), p) }

// FreeIn returns p, allocated from a, to a.
func FreeIn[T any](a Allocator, p *T) {
	if p == nil {
		return
	}
	if d, ok := any(p).(interface{ Drop() }); ok {
		d.Drop()
	}
	var zero T
	a.Free(unsafe.Pointer(p), unsafe.Sizeof(zero), unsafe.Alignof(zero))
}

// Raw allocates size zeroed bytes aligned to align from a, for code that
// lays out a variable-sized object itself. Like New, it panics if a is nil
// or out of memory. Return the memory with a.Free(p, size, align).
func Raw(a Allocator, size, align uintptr) unsafe.Pointer { return must(a, size, align) }

func must(a Allocator, size, align uintptr) unsafe.Pointer {
	if a == nil {
		panic("no allocator: call alloc.SetDefault before allocating")
	}
	if size == 0 {
		size = 1
	}
	p := a.Alloc(size, align)
	if p == nil {
		panic("out of memory")
	}
	return p
}

// ----------------------------------------------------------------------------
// Slab allocator

// A PageProvider supplies whole pages of zeroed memory.
type PageProvider interface {
	AllocPages(n uintptr) unsafe.Pointer
	FreePages(p unsafe.Pointer, n uintptr)
}

// PageSize is the page size the slab allocator assumes.
const PageSize = 4096

const (
	minClassShift = 4  // smallest slot: 16 bytes
	maxClassShift = 11 // largest slot: 2048 bytes
	numClasses    = maxClassShift - minClassShift + 1
	chunkPages    = 16 // pages fetched at once to refill a class
)

// A Slab is a size-class allocator. Requests up to 2048 bytes come from
// per-class free lists refilled a chunk of pages at a time; larger requests
// go straight to the page provider. It is not safe for concurrent use.
type Slab struct {
	pages PageProvider
	free  [numClasses]unsafe.Pointer
}

// NewSlab returns a slab allocator drawing pages from pages.
func NewSlab(pages PageProvider) *Slab {
	s := NewIn[Slab](bootstrap{pages})
	s.pages = pages
	return s
}

// bootstrap allocates the Slab itself straight from the page provider.
type bootstrap struct{ pages PageProvider }

func (b bootstrap) Alloc(size, align uintptr) unsafe.Pointer {
	return b.pages.AllocPages((size + PageSize - 1) / PageSize)
}

func (b bootstrap) Free(p unsafe.Pointer, size, align uintptr) {}

func classOf(size, align uintptr) int {
	if align > size {
		size = align
	}
	c := 0
	for s := uintptr(1) << minClassShift; s < size; s <<= 1 {
		c++
	}
	return c
}

// Alloc returns size zeroed bytes aligned to align.
func (s *Slab) Alloc(size, align uintptr) unsafe.Pointer {
	if size > 1<<maxClassShift || align > 1<<maxClassShift {
		return s.pages.AllocPages((size + PageSize - 1) / PageSize)
	}
	c := classOf(size, align)
	p := s.free[c]
	if p == nil {
		s.refill(c)
		p = s.free[c]
		if p == nil {
			return nil
		}
	}
	s.free[c] = *(*unsafe.Pointer)(p)
	*(*unsafe.Pointer)(p) = nil // the rest of the slot is already zero
	return p
}

// Free returns memory to its size class.
func (s *Slab) Free(p unsafe.Pointer, size, align uintptr) {
	if p == nil {
		return
	}
	if size > 1<<maxClassShift || align > 1<<maxClassShift {
		s.pages.FreePages(p, (size+PageSize-1)/PageSize)
		return
	}
	c := classOf(size, align)
	slot := uintptr(1) << (minClassShift + c)
	// Zero the slot so Alloc can hand it out without clearing.
	b := unsafe.Slice((*byte)(p), slot)
	clear(b)
	*(*unsafe.Pointer)(p) = s.free[c]
	s.free[c] = p
}

func (s *Slab) refill(c int) {
	chunk := s.pages.AllocPages(chunkPages)
	if chunk == nil {
		return
	}
	slot := uintptr(1) << (minClassShift + c)
	n := chunkPages * PageSize / slot
	// Thread the new slots onto the free list in address order.
	var head unsafe.Pointer
	for i := n; i > 0; i-- {
		p := unsafe.Add(chunk, (i-1)*slot)
		*(*unsafe.Pointer)(p) = head
		head = p
	}
	s.free[c] = head
}
