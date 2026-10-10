// Package debug provides a checking allocator that catches heap misuse
// deterministically: overflows past either end of an allocation, use after
// free, double and invalid frees, and leaks.
//
// Install it as the default allocator, before anything else allocates:
//
//	alloc.SetDefault(debug.New(debug.DefaultOptions()))
//
// or build a hosted program with "pluto build --alloc=debug", which does the
// same without code changes and reads settings from the PLUTO_DEBUGALLOC
// environment variable. docs/spec/debug-allocator.md describes the checks,
// the settings and the messages.
//
// Every allocation either sits at the end of its own pages, right before a
// guard page that is made inaccessible (or after one, with
// Options.Underflow), or, when guard pages are too costly, between canary
// bytes that are checked when it is freed. Freed memory is filled with
// PoisonByte, made inaccessible where it has pages of its own, and held in a
// quarantine before it is reused. A side table of every allocation catches
// frees of pointers that are not live allocations, and frees with the wrong
// size or alignment.
//
// Memory returned by Alloc is zeroed, as the Allocator interface promises.
//
// An Allocator is not safe for concurrent use. Every exported method takes
// the allocator's lock first (see lock), which is a no-op until puroutines
// run in parallel.
package debug

import (
	"pluto/alloc"
	"pluto/rt"
	"unsafe"
)

// Byte patterns.
const (
	// PoisonByte fills memory once it is freed: a stale pointer into a
	// canary-guarded block reads it (an int64 reads 0xdddddddddddddddd).
	PoisonByte = 0xDD
	// CanaryByte fills the bytes around an allocation that belong to its
	// block but not to it; they are checked when it is freed.
	CanaryByte = 0xFD
)

const pageSize = alloc.PageSize

// redzone is the canary area before and after a canary-guarded allocation.
const redzone = 16

// LeakMode says what happens at exit to allocations still live.
type LeakMode int

const (
	LeaksOff    LeakMode = iota // say nothing
	LeaksReport                 // print them
	LeaksFail                   // print them and exit with status 2 instead of 0
)

// Options configure an Allocator.
type Options struct {
	// Pages supplies memory. Guard pages need it to implement
	// PageProtector; without that every allocation is canary-guarded. In
	// hosted builds DefaultOptions uses alloc.Pages (mmap and mprotect).
	Pages alloc.PageProvider
	// Underflow puts the guard page before each allocation, at the start
	// of its pages, so reads and writes before the start fault. By
	// default the guard page follows the allocation, catching overflows.
	Underflow bool
	// CanaryBelow makes allocations smaller than this many bytes
	// canary-guarded instead of page-guarded. 0 guards every allocation
	// with pages while MaxGuarded allows.
	CanaryBelow uintptr
	// MaxGuarded limits the page-guarded blocks, live or in quarantine.
	// Each takes at least two pages and two kernel mappings (Linux allows
	// about 65530), so past the limit allocations are canary-guarded.
	MaxGuarded int
	// Quarantine is how many bytes of freed blocks (counting whole pages
	// for page-guarded ones) are held, poisoned, before their memory is
	// reused. 0 reuses memory at once.
	Quarantine uintptr
	// Leaks says what to do at exit (hosted builds) with allocations
	// still live. ReportLeaks prints them on demand.
	Leaks LeakMode
}

// DefaultOptions returns the settings New is normally given: guard pages
// after allocations, up to 16384 page-guarded blocks, a 64 MiB quarantine,
// and failing on leaks at exit.
func DefaultOptions() Options {
	return Options{
		Pages:      defaultPages(),
		MaxGuarded: 16384,
		Quarantine: 64 << 20,
		Leaks:      LeaksFail,
	}
}

// A PageProtector is a page provider that can change the access to its
// pages. FreePages must accept pages in either state.
type PageProtector interface {
	// ProtectPages makes n pages at p readable and writable (access true)
	// or inaccessible, and reports whether it could.
	ProtectPages(p unsafe.Pointer, n uintptr, access bool) bool
}

// Block kinds.
const (
	kindCanary      = 0 // in a slab slot between canaries
	kindGuardAfter  = 1 // at the end of its pages, before a guard page
	kindGuardBefore = 2 // at the start of its pages, after a guard page
)

// Record states.
const (
	stateUnused = 0
	stateLive   = 1
	stateFreed  = 2 // in quarantine
)

// A record describes one allocation, live or in quarantine.
type record struct {
	ptr    uintptr // the address Alloc returned
	base   uintptr // start of the block holding it
	span   uintptr // bytes in the block, guard page included
	size   uintptr // bytes requested
	pc     uintptr // where it was allocated, or 0
	freePC uintptr // where it was freed, or 0
	seq    uint64  // allocation number, from 1
	fseq   uint64  // free number, from 1, once freed
	align  uint32
	state  uint8
	kind   uint8
	next   int32 // quarantine or free list link, -1 at the end
}

// An Allocator is the checking allocator. Create one with New.
type Allocator struct {
	opts  Options
	pages alloc.PageProvider
	prot  PageProtector // nil if pages cannot be protected
	slab  *alloc.Slab   // memory for canary-guarded blocks

	recs     []record // indexed by record number
	nrecs    int      // records ever used (the rest of recs is fresh)
	freeRecs int32    // unused records, linked through next; -1 if none

	table []uint32 // open addressing from ptr to record number + 1; 0 is empty
	shift uint     // 64 - log2(len(table))
	used  int      // occupied table slots

	// The quarantine: freed page-guarded and canary-guarded blocks, each
	// oldest first, holding qbytes of blocks in all.
	qGuarded, qCanary fifo
	qbytes            uintptr
	frees             uint64 // frees so far, to order the two queues

	blocks [cacheMaxPages - 1]blockCache // page-guarded blocks for reuse

	guarded   int     // page-guarded blocks, live or quarantined
	live      int     // live allocations
	liveBytes uintptr // bytes they asked for
	seq       uint64  // allocations made

	next *Allocator // every Allocator, for the fault handler
}

// all lists every Allocator, newest first.
var all *Allocator

// The Allocator satisfies alloc.Allocator through its pointer.
var _ alloc.Allocator = (*Allocator)(nil)

// New returns a debug allocator with the given options. Its own state comes
// straight from opts.Pages, never from another allocator. In hosted builds
// it also catches memory faults, to explain those its guard pages cause,
// and reports leaks at exit as opts.Leaks says.
func New(opts Options) *Allocator {
	pages := opts.Pages
	if pages == nil {
		panic("debug allocator: Options.Pages is nil")
	}
	d := (*Allocator)(pages.AllocPages(pagesFor(unsafe.Sizeof(Allocator{}))))
	if d == nil {
		panic("debug allocator: out of memory")
	}
	d.opts = opts
	d.pages = pages
	if p, ok := pages.(PageProtector); ok {
		d.prot = p
	}
	d.slab = alloc.NewSlab(pages)
	d.freeRecs = -1
	d.qGuarded = fifo{-1, -1}
	d.qCanary = fifo{-1, -1}
	d.growRecords()
	d.growTable()
	d.next = all
	all = d
	register(d)
	return d
}

// lock and unlock guard the allocator's state. Puroutines do not run in
// parallel yet, so they do nothing; once they do, a single lock here makes
// the allocator safe for concurrent use. The fault handler and exit hook
// must then take it too.
func (d *Allocator) lock()   {}
func (d *Allocator) unlock() {}

// Alloc returns size zeroed bytes aligned to align (a power of two), or nil
// if memory runs out.
func (d *Allocator) Alloc(size, align uintptr) unsafe.Pointer {
	pc := rt.AllocSite()
	d.lock()
	if size == 0 {
		size = 1
	}
	if align == 0 {
		align = 1
	}
	if align&(align-1) != 0 || align > 1<<31 {
		failHead("allocation of ")
		printDec(size)
		print(" bytes with alignment ")
		printDec(align)
		print(", which is not a power of two\n")
		rt.Abort()
	}
	ri := d.newRecord()
	r := &d.recs[ri]
	ok := false
	if d.wantGuard(size, align) {
		ok = d.allocGuarded(r, size, align)
	}
	if !ok && !d.allocCanary(r, size, align) {
		r.next = d.freeRecs
		d.freeRecs = ri
		d.unlock()
		return nil
	}
	d.seq++
	r.size = size
	r.align = uint32(align)
	r.pc = pc
	r.freePC = 0
	r.seq = d.seq
	r.state = stateLive
	r.next = -1
	d.live++
	d.liveBytes += size
	d.insert(ri)
	d.unlock()
	return unsafe.Pointer(r.ptr)
}

// wantGuard reports whether an allocation should get guard pages. At the
// limit, the oldest page-guarded block in quarantine makes way for it.
func (d *Allocator) wantGuard(size, align uintptr) bool {
	if d.prot == nil || size < d.opts.CanaryBelow {
		return false
	}
	if d.guarded >= d.opts.MaxGuarded && d.qGuarded.head >= 0 {
		d.evict(&d.qGuarded)
	}
	return d.guarded < d.opts.MaxGuarded
}

// allocGuarded places an allocation next to a guard page.
func (d *Allocator) allocGuarded(r *record, size, align uintptr) bool {
	data := pagesFor(size)
	if align > pageSize {
		data = pagesFor(size + align - 1)
	}
	total := data + 1
	var guard, lo, hi uintptr // the guard page, and the data pages [lo, hi)
	place := func(base uintptr) {
		if d.opts.Underflow {
			guard = base
			lo = base + pageSize
			hi = lo + data*pageSize
			r.ptr = alignUp(lo, align)
			r.kind = kindGuardBefore
		} else {
			lo = base
			hi = base + data*pageSize
			guard = hi
			r.ptr = (hi - size) &^ (align - 1)
			r.kind = kindGuardAfter
		}
		r.base = base
		r.span = total * pageSize
	}
	if base := d.uncache(total); base != 0 {
		// A block from the cache: its guard page is still inaccessible;
		// open its data pages and zero the allocation.
		place(base)
		if d.prot.ProtectPages(unsafe.Pointer(lo), data, true) {
			fill(r.ptr, size, 0)
			fill(lo, r.ptr-lo, CanaryByte)
			fill(r.ptr+size, hi-r.ptr-size, CanaryByte)
			d.guarded++
			return true
		}
		d.pages.FreePages(unsafe.Pointer(base), total)
	}
	mem := d.pages.AllocPages(total)
	if mem == nil {
		return false
	}
	place(uintptr(mem))
	if !d.prot.ProtectPages(unsafe.Pointer(guard), 1, false) {
		// Out of kernel mappings, most likely: use canaries instead.
		d.pages.FreePages(mem, total)
		return false
	}
	// Fresh pages are zero already.
	fill(lo, r.ptr-lo, CanaryByte)
	fill(r.ptr+size, hi-r.ptr-size, CanaryByte)
	d.guarded++
	return true
}

// The block cache keeps small page-guarded blocks that left quarantine,
// still inaccessible, for reuse by blocks of the same size. That saves
// mapping, unmapping and protecting a guard page for each allocation.
const (
	cacheMaxPages = 5  // blocks of 2 to 5 pages, guard page included
	cacheDepth    = 64 // blocks kept per size
)

// cache returns true if it kept the block at base of total pages.
func (d *Allocator) cache(base, total uintptr) bool {
	if total > cacheMaxPages {
		return false
	}
	c := &d.blocks[total-2]
	if c.n == cacheDepth {
		return false
	}
	c.base[c.n] = base
	c.n++
	return true
}

// uncache returns a cached block of total pages, or 0.
func (d *Allocator) uncache(total uintptr) uintptr {
	if total > cacheMaxPages {
		return 0
	}
	c := &d.blocks[total-2]
	if c.n == 0 {
		return 0
	}
	c.n--
	return c.base[c.n]
}

// A blockCache holds blocks of one size.
type blockCache struct {
	base [cacheDepth]uintptr
	n    int
}

// allocCanary places an allocation in a slab slot between canaries.
func (d *Allocator) allocCanary(r *record, size, align uintptr) bool {
	front := max(uintptr(redzone), align)
	span := front + size + tailFor(size)
	slabAlign := min(align, pageSize)
	if align > pageSize {
		span += align
	}
	p := d.slab.Alloc(span, slabAlign)
	if p == nil {
		return false
	}
	base := uintptr(p)
	r.base = base
	r.span = span
	r.ptr = alignUp(base+front, align)
	r.kind = kindCanary
	fill(base, r.ptr-base, CanaryByte)
	fill(r.ptr+size, base+span-r.ptr-size, CanaryByte)
	return true
}

// tailFor returns the canary bytes after an allocation of size bytes: at
// least a redzone, ending on a 16-byte boundary.
func tailFor(size uintptr) uintptr {
	return redzone + (redzone-size%redzone)%redzone
}

// Free releases memory returned by Alloc, which must be given the same size
// and alignment. Freeing anything else stops the program with a message.
func (d *Allocator) Free(p unsafe.Pointer, size, align uintptr) {
	pc := rt.AllocSite()
	d.lock()
	if p == nil {
		d.unlock()
		return
	}
	if size == 0 {
		size = 1
	}
	if align == 0 {
		align = 1
	}
	addr := uintptr(p)
	ri := d.lookup(addr)
	if ri < 0 {
		d.badFree(addr, pc)
	}
	r := &d.recs[ri]
	if r.state == stateFreed {
		failHead("double free of ")
		printHex(addr)
		print(" (")
		printDec(r.size)
		print("-byte allocation #")
		printDec(uintptr(r.seq))
		print(")\n")
		printSite("  freed again at ", pc)
		printRecordSites(r)
		rt.Abort()
	}
	if r.size != size || uintptr(r.align) != align {
		failHead("free of ")
		printHex(addr)
		print(" with size ")
		printDec(size)
		print(", align ")
		printDec(align)
		print(": it was allocated with size ")
		printDec(r.size)
		print(", align ")
		printDec(uintptr(r.align))
		print("\n")
		printSite("  freed at ", pc)
		printSite("  allocated at ", r.pc)
		rt.Abort()
	}
	d.checkCanaries(r, "when it was freed")
	fill(r.ptr, r.size, PoisonByte)
	if r.kind != kindCanary {
		lo, hi := dataPages(r)
		d.prot.ProtectPages(unsafe.Pointer(lo), (hi-lo)/pageSize, false)
	}
	r.state = stateFreed
	r.freePC = pc
	d.live--
	d.liveBytes -= r.size
	// Into quarantine, then trim it to its limit, oldest first.
	d.frees++
	r.fseq = d.frees
	r.next = -1
	q := &d.qCanary
	if r.kind != kindCanary {
		q = &d.qGuarded
	}
	if q.tail >= 0 {
		d.recs[q.tail].next = ri
	} else {
		q.head = ri
	}
	q.tail = ri
	d.qbytes += r.span
	for d.qbytes > d.opts.Quarantine {
		switch {
		case d.qCanary.head < 0:
			d.evict(&d.qGuarded)
		case d.qGuarded.head < 0:
			d.evict(&d.qCanary)
		case d.recs[d.qGuarded.head].fseq < d.recs[d.qCanary.head].fseq:
			d.evict(&d.qGuarded)
		default:
			d.evict(&d.qCanary)
		}
	}
	d.unlock()
}

// A fifo is a queue of records linked through next.
type fifo struct {
	head, tail int32 // -1 when empty
}

// evict releases the oldest block in q for reuse, first checking that a
// canary-guarded one was not written to after it was freed.
func (d *Allocator) evict(q *fifo) {
	ri := q.head
	r := &d.recs[ri]
	q.head = r.next
	if q.head < 0 {
		q.tail = -1
	}
	d.qbytes -= r.span
	if r.kind == kindCanary {
		d.checkPoison(r)
		d.checkCanaries(r, "after it was freed")
		d.slab.Free(unsafe.Pointer(r.base), r.span, min(uintptr(r.align), pageSize))
	} else {
		if !d.cache(r.base, r.span/pageSize) {
			d.pages.FreePages(unsafe.Pointer(r.base), r.span/pageSize)
		}
		d.guarded--
	}
	d.remove(ri)
	r = &d.recs[ri]
	r.state = stateUnused
	r.next = d.freeRecs
	d.freeRecs = ri
}

// dataPages returns the readable pages [lo, hi) of a page-guarded block.
func dataPages(r *record) (uintptr, uintptr) {
	if r.kind == kindGuardBefore {
		return r.base + pageSize, r.base + r.span
	}
	return r.base, r.base + r.span - pageSize
}

// owned returns the bytes of r's block around the allocation that hold
// canaries: [lo, r.ptr) and [r.ptr+r.size, hi).
func owned(r *record) (uintptr, uintptr) {
	if r.kind == kindCanary {
		return r.base, r.base + r.span
	}
	return dataPages(r)
}

// checkCanaries stops the program if the canaries around r were changed.
func (d *Allocator) checkCanaries(r *record, when string) {
	lo, hi := owned(r)
	end := r.ptr + r.size
	if at := findNot(end, hi-end, CanaryByte); at != 0 {
		failHead("heap overflow: the ")
		printDec(r.size)
		print("-byte allocation at ")
		printHex(r.ptr)
		print(" (allocation #")
		printDec(uintptr(r.seq))
		print(") was written past its end, at offset ")
		printDec(at - r.ptr)
		print("; found ")
		print(when)
		print("\n")
		printRecordSites(r)
		rt.Abort()
	}
	if at := findNot(lo, r.ptr-lo, CanaryByte); at != 0 {
		// Report the byte nearest the allocation.
		for p := at; p < r.ptr; p++ {
			if *(*byte)(unsafe.Pointer(p)) != CanaryByte {
				at = p
			}
		}
		failHead("heap underflow: the ")
		printDec(r.size)
		print("-byte allocation at ")
		printHex(r.ptr)
		print(" (allocation #")
		printDec(uintptr(r.seq))
		print(") was written before its start, at offset -")
		printDec(r.ptr - at)
		print("; found ")
		print(when)
		print("\n")
		printRecordSites(r)
		rt.Abort()
	}
}

// checkPoison stops the program if a freed canary-guarded block was
// written to.
func (d *Allocator) checkPoison(r *record) {
	if at := findNot(r.ptr, r.size, PoisonByte); at != 0 {
		failHead("write after free: the ")
		printDec(r.size)
		print("-byte allocation at ")
		printHex(r.ptr)
		print(" (allocation #")
		printDec(uintptr(r.seq))
		print(") was written at offset ")
		printDec(at - r.ptr)
		print(" after it was freed\n")
		printRecordSites(r)
		rt.Abort()
	}
}

// badFree reports a free of a pointer that is not a live or quarantined
// allocation.
func (d *Allocator) badFree(addr, pc uintptr) {
	failHead("invalid free of ")
	printHex(addr)
	for i := 0; i < d.nrecs; i++ {
		r := &d.recs[i]
		if r.state != stateUnused && addr > r.ptr && addr < r.ptr+r.size {
			print(": it points at offset ")
			printDec(addr - r.ptr)
			print(" of the ")
			printDec(r.size)
			print("-byte allocation at ")
			printHex(r.ptr)
			if r.state == stateFreed {
				print(", which was freed")
			}
			print("\n")
			printSite("  freed at ", pc)
			printRecordSites(r)
			rt.Abort()
		}
	}
	print(": it was not allocated by this allocator, or was freed long enough ago to leave quarantine\n")
	printSite("  freed at ", pc)
	rt.Abort()
}

// CheckHeap verifies the canaries of every live allocation and the poison
// of every canary-guarded block in quarantine, and stops the program with
// a message at the first that was overwritten. It runs at exit in hosted
// builds.
func (d *Allocator) CheckHeap() {
	d.lock()
	for i := 0; i < d.nrecs; i++ {
		r := &d.recs[i]
		switch r.state {
		case stateLive:
			d.checkCanaries(r, "by a heap check")
		case stateFreed:
			if r.kind == kindCanary {
				d.checkPoison(r)
				d.checkCanaries(r, "after it was freed")
			}
		}
	}
	d.unlock()
}

// maxLeakLines limits the allocations ReportLeaks lists one by one.
const maxLeakLines = 64

// ReportLeaks prints every live allocation, with its size, address,
// allocation number and allocation site, and returns how many there are.
// It prints nothing when there are none.
func (d *Allocator) ReportLeaks() int {
	d.lock()
	n := d.live
	if n == 0 {
		d.unlock()
		return 0
	}
	print("debug allocator: ")
	printDec(uintptr(n))
	if n == 1 {
		print(" allocation")
	} else {
		print(" allocations")
	}
	print(" still live, ")
	printDec(d.liveBytes)
	print(" bytes:\n")
	shown := 0
	for i := 0; i < d.nrecs && shown < maxLeakLines; i++ {
		r := &d.recs[i]
		if r.state != stateLive {
			continue
		}
		shown++
		print("  ")
		printDec(r.size)
		print(" bytes at ")
		printHex(r.ptr)
		print(" (allocation #")
		printDec(uintptr(r.seq))
		print("), allocated at ")
		printRet(r.pc)
		print("\n")
	}
	if n > shown {
		print("  and ")
		printDec(uintptr(n - shown))
		print(" more\n")
	}
	d.unlock()
	return n
}

// Stats describe an Allocator's current state.
type Stats struct {
	Live        int     // live allocations
	LiveBytes   uintptr // bytes they asked for
	Quarantined int     // freed blocks held in quarantine
	Guarded     int     // page-guarded blocks, live or in quarantine
	Allocs      uint64  // allocations made so far
}

// Stats returns the allocator's counters.
func (d *Allocator) Stats() Stats {
	d.lock()
	s := Stats{Live: d.live, LiveBytes: d.liveBytes, Guarded: d.guarded, Allocs: d.seq}
	for i := d.qGuarded.head; i >= 0; i = d.recs[i].next {
		s.Quarantined++
	}
	for i := d.qCanary.head; i >= 0; i = d.recs[i].next {
		s.Quarantined++
	}
	d.unlock()
	return s
}

// DescribeFault explains a memory access fault at addr, by the instruction
// at pc, if addr lies in a block of a debug allocator: a guard page of a live
// allocation (an overflow or underflow) or a freed block (a use after free).
// It prints a "panic: " message and returns true, or returns false if addr
// is not in any block. Hosted builds call it from the fault handler; a
// kernel can call it from its page fault handler.
func DescribeFault(addr, pc uintptr, write bool) bool {
	for d := all; d != nil; d = d.next {
		for i := 0; i < d.nrecs; i++ {
			r := &d.recs[i]
			if r.state == stateUnused || r.kind == kindCanary || addr < r.base || addr >= r.base+r.span {
				continue
			}
			access := "read"
			if write {
				access = "write"
			}
			switch {
			case r.state == stateFreed:
				failHead("use after free: ")
			case addr < r.ptr:
				failHead("heap underflow: ")
			default:
				failHead("heap overflow: ")
			}
			print(access)
			print(" at ")
			printHex(addr)
			print(", offset ")
			if addr < r.ptr {
				print("-")
				printDec(r.ptr - addr)
			} else {
				printDec(addr - r.ptr)
			}
			print(" of the ")
			printDec(r.size)
			print("-byte allocation at ")
			printHex(r.ptr)
			print(" (allocation #")
			printDec(uintptr(r.seq))
			if r.state == stateFreed {
				print(", freed")
			}
			print(")\n")
			print("  faulting instruction at pc ")
			rt.PrintPC(pc)
			print("\n")
			printRecordSites(r)
			return true
		}
	}
	return false
}
