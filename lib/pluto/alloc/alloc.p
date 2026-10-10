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

// A Restore puts back the allocator that was the default before Use. It is
// an owner: dropping it puts the previous allocator back, so the allocator
// Use installed is the default only while the Restore lives.
type Restore owner struct {
	prev Allocator
	cur  Allocator // the allocator Use installed; nil once restored
}

// Use makes a the running puroutine's default allocator until the returned
// Restore is dropped, or its Restore method is called:
//
//	defer alloc.Use(arena).Restore()
//
// The override belongs to the puroutine that calls Use: Default returns a
// there and the program-wide default (SetDefault) everywhere else, and a
// puroutine it starts begins with the program-wide default.
//
// a may be a local arena: the Restore holds a, so the compiler checks that
// the Restore does not outlive it (language spec section 7.1).
//
//pu:resultholds a
func Use(a Allocator) Restore {
	return Restore{prev: rt.UseAllocator(a), cur: a}
}

// Restore reinstates the previous default allocator. It consumes r.
func (r Restore) Restore() { r.Drop() }

// Drop reinstates the previous default allocator, if r still holds one.
func (r *Restore) Drop() {
	if r.cur != nil {
		rt.UseAllocator(r.prev)
		r.cur = nil
	}
}

// New allocates a zeroed T from a, by default the default allocator. T may
// be an owner: the memory itself has no owner until the caller gives it one
// (language spec section 6.6).
func New[T owner any](a Allocator = Default()) *T {
	var p *T // only for its element's size: Sizeof does not evaluate *p
	return (*T)(must(a, unsafe.Sizeof(*p), unsafe.Alignof(*p), rt.CallerPC()))
}

// Free returns p, allocated by New, to the allocator it came from: a, by
// default the default allocator. *p is destroyed first: if T is an owner,
// its Drop method, then its fields; otherwise the strings in it are
// released (language spec sections 6.4 and 6.10). The call's site is
// published through rt.AllocSite while the allocator runs (if
// rt.TrackSites).
func Free[T owner any](p *T, a Allocator = Default()) {
	if p == nil {
		return
	}
	unsafe.Drop(p)
	rt.ReleaseValues(p, 1)
	if rt.TrackSites {
		rt.SetAllocSite(rt.CallerPC())
	}
	a.Free(unsafe.Pointer(p), unsafe.Sizeof(*p), unsafe.Alignof(*p))
	if rt.TrackSites {
		rt.SetAllocSite(0)
	}
}

// FreeChan frees a channel made by make(chan T, ...) and its buffer,
// returning them to the allocator they came from, whatever the default is
// now. No puroutine may use the channel afterwards, and none may be waiting
// on it (FreeChan panics then); values still buffered are discarded, and
// the strings in them released (language spec section 6.10).
//
// Channels are freed explicitly until the compiler counts channel
// references (roadmap item 48, language spec section 4.8); then the last
// reference frees the channel and FreeChan goes away. A channel made in an
// arena needs no FreeChan.
func FreeChan[T any](ch chan T) {
	p := *(*unsafe.Pointer)(unsafe.Pointer(&ch))
	if p != nil && rt.HoldsStrings[T]() {
		var zero T
		buf, first, n, size := rt.ChanBuffered(p)
		for i := 0; i < n; i++ {
			rt.ReleaseValues((*T)(unsafe.Add(buf, uintptr((first+i)%size)*unsafe.Sizeof(zero))), 1)
		}
	}
	rt.FreeChan(p)
}

// Raw allocates size zeroed bytes aligned to align from a (by default the
// default allocator), for code that lays out a variable-sized object
// itself. Like New, it panics if a is nil or out of memory. Return the
// memory with a.Free(p, size, align).
func Raw(size, align uintptr, a Allocator = Default()) unsafe.Pointer {
	return must(a, size, align, rt.CallerPC())
}

// must allocates for the call that returns to pc (see rt.AllocSite and
// rt.TrackSites).
func must(a Allocator, size, align, pc uintptr) unsafe.Pointer {
	if a == nil {
		panic("no allocator: call alloc.SetDefault before allocating")
	}
	if size == 0 {
		size = 1
	}
	if rt.TrackSites {
		rt.SetAllocSite(pc)
	}
	p := a.Alloc(size, align)
	if rt.TrackSites {
		rt.SetAllocSite(0)
	}
	if p == nil {
		panic("out of memory")
	}
	return p
}

// ReportLeaks prints the allocations the default allocator still holds, if
// it keeps track of them (the debug allocator, pluto/alloc/debug, does), and
// returns how many there are. Other allocators print nothing and report 0.
func ReportLeaks() int {
	if r, ok := Default().(interface{ ReportLeaks() int }); ok {
		return r.ReportLeaks()
	}
	return 0
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
//
// The free lists link free slots through their first word. The links are
// addresses of memory nobody owns, not references the program holds, so
// they are kept as uintptr: the compiler's escape analysis (language spec
// section 6.3) then does not count a slot handed back to Free as kept, nor
// the free list as something a freed slot points to.
type Slab struct {
	pages PageProvider
	free  [numClasses]uintptr
}

// NewSlab returns a slab allocator drawing pages from pages.
func NewSlab(pages PageProvider) *Slab {
	s := New[Slab](bootstrap{pages})
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
	if s.free[c] == 0 {
		s.refill(c)
		if s.free[c] == 0 {
			return nil
		}
	}
	p := unsafe.Pointer(s.free[c])
	s.free[c] = *(*uintptr)(p)
	*(*uintptr)(p) = 0 // the rest of the slot is already zero
	if genCheck {
		return genTag(p)
	}
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
	if genCheck {
		live := genRelease(p, slot)
		p = unsafe.Pointer(uintptr(p)) // the plain address
		if !live {
			return // the slot is worn out: never hand it out again
		}
	}
	// Zero the slot so Alloc can hand it out without clearing.
	b := unsafe.Slice((*byte)(p), slot)
	clear(b)
	*(*uintptr)(p) = s.free[c]
	s.free[c] = uintptr(p)
}

func (s *Slab) refill(c int) {
	var chunk unsafe.Pointer
	first := uintptr(0)
	if genCheck {
		chunk = s.genChunk()
		first = genFirst(uintptr(1) << (minClassShift + c))
	} else {
		chunk = s.pages.AllocPages(chunkPages)
	}
	if chunk == nil {
		return
	}
	slot := uintptr(1) << (minClassShift + c)
	n := chunkPages * PageSize / slot
	// Thread the new slots onto the free list in address order.
	var head uintptr
	for i := n; i > first; i-- {
		p := unsafe.Add(chunk, (i-1)*slot)
		*(*uintptr)(p) = head
		head = uintptr(p)
	}
	s.free[c] = head
}
