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
	"compiler/compat"
	"pluto/alloc"
	"pluto/os"
	"pluto/rt"
	"unsafe"
)

// chunkSize is the size of the arenas' chunks. Released chunks of this
// size stay with an arena for the next step at its level; larger ones go
// back to the heap.
const chunkSize = 1 << 20

// maxDepth is how deeply Do nests: a phase of the compiler run as a step,
// and steps within it.
const maxDepth = 3

// A level is the state of the Do running at one nesting depth of a Stack.
// Each level has its own arena, and in check mode its own pages, so that
// the end of an inner step frees and seals only that step's memory. The
// level is the allocator of its step's work, so that Heap, Outer and Used
// find the running step's state from the current allocator, on whichever
// task runs it.
type level owner struct {
	mem
	ready bool
	outer alloc.Allocator // the allocator current when the Do began
	st    *Stack          // the stack the level is part of
	pages checkPages
}

// A Stack is the levels of Do on one task. The main task's is root, which
// the package's Do uses; a task that runs steps beside it has a Stack of
// its own (NewStack) and runs them with its Do.
type Stack owner struct {
	levels [maxDepth]level
	depth  int             // Do's whose work is running
	heap   alloc.Allocator // the parent of the arenas
	check  bool            // PLUTO_SCRATCH_CHECK
}

var (
	root   Stack
	heap   alloc.Allocator // the allocator of the first Do, parent of the arenas
	check  bool            // PLUTO_SCRATCH_CHECK
	inited bool
)

// current is the stack of the running task: the one whose step's work is
// running, or root.
func current() *Stack {
	if l, ok := alloc.Default().(*level); ok {
		return l.st
	}
	initOnce()
	return &root
}

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
func Do(s Step) { current().do(s, nil) }

// InitStack readies st, which is empty, for a task that runs steps beside
// the main task. The task runs them with st.Do and frees st's memory with
// st.Trim when it is done.
func InitStack(st *Stack) {
	initOnce()
	st.heap = heap
	st.check = check
}

// Do is the package's Do on st's levels.
func (st *Stack) Do(s Step) { st.do(s, nil) }

// do is Do, with s.ScratchKeep allocating from keep when it is not nil.
func (st *Stack) do(s Step, keep alloc.Allocator) {
	if st.depth == maxDepth {
		panic("scratch.Do nested too deeply")
	}
	l := &st.levels[st.depth]
	if !l.ready {
		if st.check {
			l.mem = newMem(&l.pages)
		} else {
			l.mem = newMem(st.heap)
		}
		l.st = st
		l.ready = true
	}
	l.outer = alloc.Default()
	m := l.mem.Mark()
	st.depth++
	r := alloc.Use(l)
	s.ScratchWork()
	r.Restore()
	st.depth--
	l.outer = nil
	if keep != nil {
		k := alloc.Use(keep)
		s.ScratchKeep()
		k.Restore()
	} else {
		s.ScratchKeep()
	}
	l.mem.Release(m)
	if st.check {
		l.mem.Trim()
		l.pages.seal()
	}
}

// Trim returns to the heap the spare memory that st's arenas keep for
// later steps.
func (st *Stack) Trim() {
	for i := st.depth; i < maxDepth; i++ {
		if st.levels[i].ready {
			st.levels[i].mem.Trim()
		}
	}
}

// homes holds, for each key DoHome has been given, the memory of what its
// last step with that key kept; filling is the home of the step running.
var (
	homes   map[unsafe.Pointer]*home
	filling *home
)

// DoHome is Do for a step whose ScratchKeep makes a new version of the
// thing key points to, such as an optimised copy of a function. What
// ScratchKeep allocates goes into memory of key's own and, when there is
// any, the memory the previous DoHome with the same key kept is freed:
// nothing may still reach the older version. A step whose ScratchKeep
// allocates nothing leaves the older version in place. Within a Do's work
// DoHome is Do.
func DoHome(key unsafe.Pointer, s Step) {
	st := current()
	if st.depth != 0 {
		st.do(s, nil)
		return
	}
	filling = newHome()
	st.do(s, filling)
	keepHome(key)
}

// keepHome makes what filling holds key's home, freeing the memory of
// key's previous home, if there was one. With filling empty it does
// nothing.
func keepHome(key unsafe.Pointer) {
	h := filling
	filling = nil
	if h.empty() {
		freeHome(h)
		return
	}
	r := alloc.Use(heap)
	if homes == nil {
		homes = map[unsafe.Pointer]*home{}
	}
	if old := homes[key]; old != nil {
		freeHome(old)
	}
	homes[key] = h
	r.Restore()
}

// newHome returns an empty home. The home itself is in the heap, and
// freeHome frees its memory and it.
func newHome() *home {
	h := (*home)(heap.Alloc(unsafe.Sizeof(home{}), unsafe.Alignof(home{})))
	if h == nil {
		panic("scratch: out of memory")
	}
	clear(unsafe.Slice((*byte)(unsafe.Pointer(h)), unsafe.Sizeof(home{})))
	h.init()
	h.heap = heap
	return h
}

func freeHome(h *home) {
	h.drop()
	heap.Free(unsafe.Pointer(h), unsafe.Sizeof(home{}), unsafe.Alignof(home{}))
}

// homeKey is the key of the home BeginHome began, and homeUse makes the
// home being filled the current allocator until EndHome.
var (
	homeKey unsafe.Pointer
	homeUse alloc.Restore
)

// BeginHome makes memory of key's own the current allocator until
// EndHome, as DoHome does for its step's ScratchKeep: a ScratchKeep that
// makes the first versions of several things, such as the lowered
// functions, gives each its own memory this way, for later DoHome steps
// with the same key to free. It is called outside any Do's work, and
// EndHome before the next BeginHome.
func BeginHome(key unsafe.Pointer) {
	if current().depth != 0 || homeKey != nil || key == nil {
		panic("scratch.BeginHome within a step's work or another BeginHome")
	}
	filling = newHome()
	homeKey = key
	homeUse = alloc.Use(filling)
}

// EndHome ends what BeginHome began: what was allocated since becomes the
// key's home.
func EndHome() {
	if homeKey == nil {
		panic("scratch.EndHome without BeginHome")
	}
	old := take(&homeUse)
	old.Restore()
	key := homeKey
	homeKey = nil
	keepHome(key)
}

// Drop frees the memory DoHome kept for key, once nothing will use the
// version of key's thing it holds again.
func Drop(key unsafe.Pointer) {
	if h := homes[key]; h != nil {
		delete(homes, key)
		freeHome(h)
	}
}

// A Region is memory for things that are freed together, such as what
// the language server makes when it checks one package or reads one
// message: chunks taken from the heap and filled in order, freed all at
// once by Release. NewRegion makes one, and Use makes it the current
// allocator. Without generation checks its chunks start small and double,
// so that a region holding one short string costs little.
//
// HeapFor finds the region a pointer is in, so that a cache the type
// checker fills lazily on a type comes from the memory the type is in,
// and is freed with it. Every live region's chunks are recorded for it.
//
// In check mode (PLUTO_SCRATCH_CHECK) the chunks are pages of their own,
// which Release makes inaccessible, so that a pointer into a released
// region faults where it is used. With generation checks the memory is
// pluto/arena's Chunked (mem_on.p), whose Release ends the registration of
// each allocation, so that using one after Release panics and the
// registrations do not pile up.
type Region owner struct {
	regionMem                 // Alloc and Free; mem_on.p and mem_off.p
	heap      alloc.Allocator // for Heap
}

// A regionSpan is the memory of one chunk of a live region.
type regionSpan struct {
	lo, hi uintptr
	r      uintptr // the *Region as a number; Release removes the span before it frees the Region
}

// spansMu guards spans: tasks that solve beside each other look up regions
// through HeapFor.
var spansMu rt.Lock

var (
	spans      unsafe.Pointer // [spansCap]regionSpan, sorted by lo, in the heap
	spansLen   int
	spansCap   int
	regions    int     // live regions
	regionUsed uintptr // bytes of their chunks
)

func initOnce() {
	if !inited {
		check = os.GetenvLossy("PLUTO_SCRATCH_CHECK") != ""
		heap = alloc.Default()
		root.heap, root.check = heap, check
		inited = true
	}
}

// NewRegion returns an empty region. The Region itself is in the heap,
// and Release frees it.
func NewRegion() *Region {
	initOnce()
	r := (*Region)(heap.Alloc(unsafe.Sizeof(Region{}), unsafe.Alignof(Region{})))
	if r == nil {
		panic("scratch: out of memory")
	}
	clear(unsafe.Slice((*byte)(unsafe.Pointer(r)), unsafe.Sizeof(Region{})))
	r.init()
	r.heap = heap
	regions++
	return r
}

// Use makes r the current allocator until Restore is called.
func (r *Region) Use() alloc.Restore { return alloc.Use(r) }

// Used returns the bytes of r's chunks.
func (r *Region) Used() int { return int(r.bytes()) }

// Release frees r's memory and r. Nothing in it may be used afterwards.
func (r *Region) Release() {
	if r == nil {
		return
	}
	r.release()
	regions--
	heap.Free(unsafe.Pointer(r), unsafe.Sizeof(Region{}), unsafe.Alignof(Region{}))
}

// Regions returns how many regions are live and the bytes of their chunks.
func Regions() (int, int) { return regions, int(regionUsed) }

func span(i int) *regionSpan {
	return (*regionSpan)(unsafe.Add(spans, uintptr(i)*unsafe.Sizeof(regionSpan{})))
}

// findSpan returns the index of the first span whose lo is above p.
func findSpan(p uintptr) int {
	lo, hi := 0, spansLen
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		if span(m).lo <= p {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo
}

func addSpan(s regionSpan) {
	spansMu.Lock()
	defer spansMu.Unlock()
	size := unsafe.Sizeof(regionSpan{})
	if spansLen == spansCap {
		n := spansCap * 2
		if n == 0 {
			n = 256
		}
		p := heap.Alloc(uintptr(n)*size, unsafe.Alignof(regionSpan{}))
		if p == nil {
			panic("scratch: out of memory")
		}
		if spansLen > 0 {
			copy(unsafe.Slice((*byte)(p), uintptr(spansLen)*size), unsafe.Slice((*byte)(spans), uintptr(spansLen)*size))
			heap.Free(spans, uintptr(spansCap)*size, unsafe.Alignof(regionSpan{}))
		}
		spans, spansCap = p, n
	}
	i := findSpan(s.lo)
	for j := spansLen; j > i; j-- {
		*span(j) = *span(j - 1)
	}
	*span(i) = s
	spansLen++
}

func removeSpan(lo uintptr) {
	spansMu.Lock()
	defer spansMu.Unlock()
	i := findSpan(lo) - 1
	if i < 0 || span(i).lo != lo {
		panic("scratch: region chunk not recorded")
	}
	for j := i; j+1 < spansLen; j++ {
		*span(j) = *span(j + 1)
	}
	spansLen--
}

// regionOf returns the live region whose memory p is in, or nil.
func regionOf(p unsafe.Pointer) *Region {
	spansMu.Lock()
	defer spansMu.Unlock()
	if spansLen == 0 {
		return nil
	}
	a := uintptr(p)
	i := findSpan(a) - 1
	if i < 0 {
		return nil
	}
	if s := span(i); a < s.hi {
		return (*Region)(unsafe.Pointer(s.r))
	}
	return nil
}

// HeapFor makes current, until Restore is called, the memory that a cache
// filled lazily on the object at p should come from: the region p is in,
// or otherwise the heap, as Heap does. A cache so made is freed with its
// object, and does not come from memory that lives less long.
func HeapFor(p unsafe.Pointer) alloc.Restore {
	if r := regionOf(p); r != nil {
		return alloc.Use(r)
	}
	return Heap()
}

// mapChunk maps n bytes (a multiple of pageSize) aligned to align: pages
// of their own for a chunk in check mode, which sealChunk frees and makes
// inaccessible.
func mapChunk(n, align uintptr) unsafe.Pointer {
	if align <= pageSize {
		return rt.Mmap(n)
	}
	raw := rt.Mmap(n + align)
	if raw == nil {
		return nil
	}
	lead := (align - uintptr(raw)&(align-1)) & (align - 1)
	if lead > 0 {
		rt.Munmap(raw, lead)
	}
	if align > lead {
		rt.Munmap(unsafe.Add(raw, lead+n), align-lead)
	}
	return unsafe.Add(raw, lead)
}

func sealChunk(p unsafe.Pointer, n uintptr) {
	rt.Discard(p, n)
	if !rt.Mprotect(p, n, rt.ProtNone) {
		panic("scratch: mprotect failed (PLUTO_SCRATCH_CHECK)")
	}
}

// Trim returns to the heap the spare memory that the main task's arenas
// keep for later steps, after a large step whose like does not follow.
func Trim() { current().Trim() }

// Used returns the bytes the innermost running step has allocated from
// scratch memory, or 0 outside a step's work. A step whose work allocated
// nothing holds nothing in scratch memory and has nothing to copy out.
func Used() int {
	if l, ok := alloc.Default().(*level); ok {
		return l.mem.Used()
	}
	return 0
}

// Outer makes the allocator that was current when the innermost running
// Do began the current one until Restore is called, so that memory a step
// adds to state outliving it (a memo table, an interning table, a pool)
// does not come from that step's scratch memory. Outside a Do's work it
// changes nothing.
func Outer() alloc.Restore {
	switch a := alloc.Default().(type) {
	case *level:
		return alloc.Use(a.outer)
	case *Worker:
		if a.outer == nil {
			return alloc.Use(a)
		}
		return alloc.Use(a.outer)
	case *Locked:
		return alloc.Use(a)
	}
	return alloc.Use(alloc.Default())
}

// Heap makes the heap the current allocator until Restore is called, so
// that a cache filled lazily by whichever step first asks (the type
// checker's expanded methods and underlying types) does not come from any
// step's scratch memory, any Region or any home. It reads no state but the
// current allocator's, so that tasks that run steps beside each other may
// call it.
func Heap() alloc.Restore {
	switch a := alloc.Default().(type) {
	case *level:
		return alloc.Use(a.st.heap)
	case *Worker:
		return alloc.Use(a.heap)
	case *Region:
		return alloc.Use(a.heap)
	case *home:
		return alloc.Use(a.heap)
	}
	return alloc.Use(alloc.Default())
}

// A Locked allocator is an allocator that several tasks use at once
// through a lock: the memory of the running step, while workers solve
// beside each other (Worker), for what they add to state that outlives
// their steps. With generation checks it is a custom allocator, whose
// allocations are registered over those of the allocator behind it: what
// it allocates from a Do's arena stays registered when the arena releases
// it, so it suits memory freed one allocation at a time.
type Locked struct {
	mu rt.Lock // an allocator's lock, which never parks
	a  alloc.Allocator
}

// NewLocked returns the current allocator behind a lock. The caller
// keeps it where the tasks that use it can reach it, such as its frame.
func NewLocked() Locked { return Locked{a: alloc.Default()} }

func (l *Locked) Alloc(size, align uintptr) unsafe.Pointer {
	l.mu.Lock()
	p := l.a.Alloc(size, align)
	l.mu.Unlock()
	return p
}

func (l *Locked) Free(p unsafe.Pointer, size, align uintptr) {
	l.mu.Lock()
	l.a.Free(p, size, align)
	l.mu.Unlock()
}

// A Worker runs steps on a task of its own, beside other workers, in
// scratch memory of its own. Its steps do not nest. Outer within a step
// makes outer current: a Locked allocator the workers share, or memory of
// the task's own that outlives the steps. A Worker with no outer is such
// memory itself, and Outer within it changes nothing.
//
// Its Alloc and Free are mem's: a Worker forwarding them would be a custom
// allocator of its own, whose allocations generation checks register over
// mem's, and Release, which ends mem's registrations, would leave those.
type Worker owner struct {
	mem
	outer alloc.Allocator
	heap  alloc.Allocator // for Heap
	base  memMark         // the arena empty, for Reset
}

// NewWorker returns a worker whose steps keep their results in outer. Its
// arena's chunks come from the heap, which tasks share.
func NewWorker(outer alloc.Allocator) Worker {
	initOnce()
	w := Worker{mem: newMem(heap), outer: outer, heap: heap}
	w.base = w.mem.Mark()
	return w
}

// Work runs s.ScratchWork in w's memory and keeps that memory until
// Reset, so that its results can be copied out later, in order with other
// workers'.
func (w *Worker) Work(s Step) {
	r := alloc.Use(w)
	s.ScratchWork()
	r.Restore()
}

// Mark, Release and Shared are Do in parts, for a task that calls its
// step's methods directly rather than through Step: Mark notes the arena,
// Release frees what was allocated since, and Shared is the outer memory.
func (w *Worker) Mark() memMark { return w.mem.Mark() }

func (w *Worker) Release(m memMark) { w.mem.Release(m) }

func (w *Worker) Shared() alloc.Allocator { return w.outer }

// Reset frees everything w's steps allocated.
func (w *Worker) Reset() { w.mem.Release(w.base) }

// Drop frees w's memory.
func (w *Worker) Drop() {
	w.mem.Release(w.base)
	w.mem.Trim()
}

// Do runs s.ScratchWork in w's memory and s.ScratchKeep in its outer
// memory, then frees what ScratchWork allocated.
func (w *Worker) Do(s Step) {
	m := w.mem.Mark()
	r := alloc.Use(w)
	s.ScratchWork()
	r.Restore()
	o := alloc.Use(w.outer)
	s.ScratchKeep()
	o.Restore()
	w.mem.Release(m)
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

// A bump is the scratch memory of one level of Do, or of a Worker: chunks
// taken from parent and filled in order, each beginning with a link to
// the chunk before it and its own size. Release goes back to a mark;
// standard chunks it frees are kept for reuse until Trim, larger ones go
// back to parent. Unlike pluto/arena's Chunked it keeps no record of each
// allocation (language spec section 18, Arenas, asks one of a library
// arena): scratch memory is raw allocation whose lifetimes the compiler
// manages itself, and a record per allocation doubled its memory.
type bump struct {
	parent alloc.Allocator
	cur    unsafe.Pointer // the chunk being filled, linking to older ones
	off    uintptr        // bytes of cur in use, its header included
	size   uintptr        // cur's size
	spare  unsafe.Pointer // standard chunks kept for reuse
	used   uintptr        // bytes handed out, padding included
}

// A bumpMark is how full a bump was; see bump.Release.
type bumpMark struct {
	cur  unsafe.Pointer
	off  uintptr
	size uintptr
	used uintptr
}

const bumpHeader = 16

func (b *bump) Alloc(size, align uintptr) unsafe.Pointer {
	if size == 0 {
		size = 1
	}
	if align < 8 {
		align = 8
	}
	base := uintptr(b.cur)
	off := (base+b.off+align-1)&^(align-1) - base
	if b.cur == nil || off+size > b.size {
		if !b.grow(size + align) {
			return nil
		}
		base = uintptr(b.cur)
		off = (base+b.off+align-1)&^(align-1) - base
	}
	p := unsafe.Add(b.cur, off)
	b.used += off + size - b.off
	b.off = off + size
	// A chunk may hold data from before a Release.
	clear(unsafe.Slice((*byte)(p), size))
	return p
}

// grow makes a chunk with room for need bytes the current one.
func (b *bump) grow(need uintptr) bool {
	n := uintptr(chunkSize)
	if need+bumpHeader > n {
		n = (need + bumpHeader + 15) &^ 15
	}
	var c unsafe.Pointer
	if n == chunkSize && b.spare != nil {
		c = b.spare
		b.spare = *(*unsafe.Pointer)(c)
	} else {
		c = b.parent.Alloc(n, 16)
		if c == nil {
			return false
		}
	}
	*(*unsafe.Pointer)(c) = b.cur
	*(*uintptr)(unsafe.Add(c, 8)) = n
	b.cur, b.off, b.size = c, bumpHeader, n
	return true
}

// Free gives back the space of the latest allocation, which makes a
// growing slice cheaper; any other Free does nothing, as memory is freed
// by Release.
func (b *bump) Free(p unsafe.Pointer, size, align uintptr) {
	if size == 0 {
		size = 1
	}
	base := uintptr(b.cur)
	if b.cur == nil || uintptr(p) < base+bumpHeader || uintptr(p)+size != base+b.off {
		return
	}
	b.used -= size
	b.off -= size
}

func (b *bump) Mark() bumpMark { return bumpMark{b.cur, b.off, b.size, b.used} }

// Release frees everything allocated since m was taken.
func (b *bump) Release(m bumpMark) {
	for b.cur != m.cur {
		c := b.cur
		if c == nil {
			panic("scratch: Release of a stale mark")
		}
		b.cur = *(*unsafe.Pointer)(c)
		n := *(*uintptr)(unsafe.Add(c, 8))
		if n == chunkSize {
			*(*unsafe.Pointer)(c) = b.spare
			b.spare = c
		} else {
			b.parent.Free(c, n, 16)
		}
	}
	b.off, b.size, b.used = m.off, m.size, m.used
}

// Trim returns the spare chunks to parent.
func (b *bump) Trim() {
	for b.spare != nil {
		c := b.spare
		b.spare = *(*unsafe.Pointer)(c)
		b.parent.Free(c, chunkSize, 16)
	}
}

// Used returns the bytes handed out since b was empty, padding included.
func (b *bump) Used() int { return int(b.used) }
