//pu:build linux

package alloc

import (
	"pluto/rt"
	"unsafe"
)

// mmapPages is the hosted page provider.
type mmapPages struct{}

func (mmapPages) AllocPages(n uintptr) unsafe.Pointer { return rt.Mmap(n * PageSize) }

func (mmapPages) FreePages(p unsafe.Pointer, n uintptr) { rt.Munmap(p, n*PageSize) }

// ProtectPages makes n pages at p readable and writable, or inaccessible.
// The debug allocator uses it for guard pages and freed memory.
func (mmapPages) ProtectPages(p unsafe.Pointer, n uintptr, access bool) bool {
	prot := rt.ProtNone
	if access {
		prot = rt.ProtRead | rt.ProtWrite
	}
	return rt.Mprotect(p, n*PageSize, prot)
}

// Pages is the hosted page provider, backed by mmap.
var Pages PageProvider = mmapPages{}

func init() {
	SetDefault(NewSyncSlab(Pages))
}

// ----------------------------------------------------------------------------
// The hosted default allocator

const (
	syncBatch    = 32 // slots moved between a cache and the shared lists at once
	syncCacheMax = 2 * syncBatch
)

// A SyncSlab is the size-class allocator of Slab made safe for use by
// several puroutines at once (language spec section 7.2). Each processor of
// the hosted scheduler (a P, rt.ProcPin) has a cache of free slots per size
// class, used without locks; the caches refill from, and overflow to,
// shared free lists behind one runtime lock, which also serve threads that
// hold no P. Freed slots are zeroed before they are published, and every
// slot that moves between processors passes through the lock, so a Free is
// synchronised before any Alloc that reuses its memory (memory-model.md
// section 3.8). Requests over 2048 bytes go straight to the page provider,
// which must itself be safe for concurrent use, as mmap is.
type SyncSlab struct {
	pages   PageProvider
	lock    rt.Lock
	central [numClasses]unsafe.Pointer
	caches  [rt.MaxProcs]syncCache
}

// A syncCache is one processor's free slots. It is padded to its own cache
// lines.
type syncCache struct {
	free [numClasses]unsafe.Pointer
	n    [numClasses]int32
	_    [32]byte
}

// NewSyncSlab returns a concurrent slab allocator drawing pages from pages.
func NewSyncSlab(pages PageProvider) *SyncSlab {
	s := New[SyncSlab](bootstrap{pages})
	s.pages = pages
	return s
}

// Alloc returns size zeroed bytes aligned to align.
func (s *SyncSlab) Alloc(size, align uintptr) unsafe.Pointer {
	if size > 1<<maxClassShift || align > 1<<maxClassShift {
		return s.pages.AllocPages((size + PageSize - 1) / PageSize)
	}
	c := classOf(size, align)
	var p unsafe.Pointer
	if id := rt.ProcPin(); id >= 0 {
		pc := &s.caches[id]
		if pc.free[c] == nil {
			s.refill(pc, c)
		}
		p = pc.free[c]
		if p != nil {
			pc.free[c] = *(*unsafe.Pointer)(p)
			pc.n[c]--
		}
	} else {
		s.lock.Lock()
		if s.central[c] == nil {
			s.grow(c)
		}
		p = s.central[c]
		if p != nil {
			s.central[c] = *(*unsafe.Pointer)(p)
		}
		s.lock.Unlock()
	}
	rt.ProcUnpin()
	if p != nil {
		*(*unsafe.Pointer)(p) = nil // the rest of the slot is already zero
	}
	return p
}

// Free returns memory to its size class.
func (s *SyncSlab) Free(p unsafe.Pointer, size, align uintptr) {
	if p == nil {
		return
	}
	if size > 1<<maxClassShift || align > 1<<maxClassShift {
		s.pages.FreePages(p, (size+PageSize-1)/PageSize)
		return
	}
	c := classOf(size, align)
	slot := uintptr(1) << (minClassShift + c)
	clear(unsafe.Slice((*byte)(p), slot))
	if id := rt.ProcPin(); id >= 0 {
		pc := &s.caches[id]
		*(*unsafe.Pointer)(p) = pc.free[c]
		pc.free[c] = p
		pc.n[c]++
		if pc.n[c] > syncCacheMax {
			s.drain(pc, c)
		}
	} else {
		s.lock.Lock()
		*(*unsafe.Pointer)(p) = s.central[c]
		s.central[c] = p
		s.lock.Unlock()
	}
	rt.ProcUnpin()
}

// refill moves a batch of class c's free slots from the shared lists to
// the cache pc, taking a chunk of pages if they are empty.
func (s *SyncSlab) refill(pc *syncCache, c int) {
	s.lock.Lock()
	for i := 0; i < syncBatch; i++ {
		if s.central[c] == nil {
			s.grow(c)
		}
		p := s.central[c]
		if p == nil {
			break
		}
		s.central[c] = *(*unsafe.Pointer)(p)
		*(*unsafe.Pointer)(p) = pc.free[c]
		pc.free[c] = p
		pc.n[c]++
	}
	s.lock.Unlock()
}

// drain moves a batch of class c's free slots from the cache pc to the
// shared lists.
func (s *SyncSlab) drain(pc *syncCache, c int) {
	s.lock.Lock()
	for i := 0; i < syncBatch; i++ {
		p := pc.free[c]
		pc.free[c] = *(*unsafe.Pointer)(p)
		pc.n[c]--
		*(*unsafe.Pointer)(p) = s.central[c]
		s.central[c] = p
	}
	s.lock.Unlock()
}

// grow threads a new chunk of pages onto class c's shared list; s.lock is
// held.
func (s *SyncSlab) grow(c int) {
	chunk := s.pages.AllocPages(chunkPages)
	if chunk == nil {
		return
	}
	slot := uintptr(1) << (minClassShift + c)
	n := chunkPages * PageSize / slot
	head := s.central[c]
	for i := n; i > 0; i-- {
		p := unsafe.Add(chunk, (i-1)*slot)
		*(*unsafe.Pointer)(p) = head
		head = p
	}
	s.central[c] = head
}
