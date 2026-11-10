package arena

import (
	"pluto/alloc"
	"pluto/rt"
	"unsafe"
)

// A Chunked's allocation bookkeeping is separate from payload and comes from
// the named parent allocator (a Fixed keeps its own, fixed.p). Reclaimed
// records are returned to it. Runtime reader and address-history metadata
// uses the checked directory's permanent storage.
type allocation struct {
	prev     *allocation
	p        unsafe.Pointer
	size     uintptr
	token    uintptr
	sequence uint64
}

type lifetime struct {
	parent   alloc.Allocator
	head     *allocation
	identity uint64
	reset    uint64
	next     uint64
	// retired is set by an allocate that failed because the address
	// history of the extent's granules is exhausted: the address just past
	// the last such granule. The caller retires that storage by allocating
	// after it (abi.md section 1.3, Arena epochs).
	retired uintptr
}

func newLifetime(parent alloc.Allocator) lifetime {
	return lifetime{parent: parent, identity: rt.ArenaIdentity()}
}

func (l *lifetime) allocate(p unsafe.Pointer, size uintptr) unsafe.Pointer {
	if l.next == ^uint64(0) {
		panic("arena: allocation identity exhausted")
	}
	l.retired = 0
	raw := l.parent.Alloc(unsafe.Sizeof(allocation{}), unsafe.Alignof(allocation{}))
	if raw == nil {
		return nil
	}
	token := uintptr(0)
	result := p
	if genCheck {
		token = rt.GenRegister(unsafe.Pointer(uintptr(p)), size, p, 0)
		result = rt.GenTagArena(token, p, size)
		if result == nil {
			if token != 0 {
				l.retired = rt.GenArenaExhausted(p, size)
			}
			rt.GenUnregister(token)
			l.parent.Free(raw, unsafe.Sizeof(allocation{}), unsafe.Alignof(allocation{}))
			return nil
		}
	}
	l.next++
	r := (*allocation)(raw)
	*r = allocation{prev: l.head, p: result, size: size, token: token, sequence: l.next}
	l.head = r
	return result
}

func (l *lifetime) sequence() uint64 {
	if l.head != nil {
		return l.head.sequence
	}
	return 0
}

func (l *lifetime) valid(identity, reset, sequence uint64, end uintptr) bool {
	if identity != l.identity || reset != l.reset || identity == 0 {
		return false
	}
	if sequence == 0 {
		return end == 0
	}
	for r := l.head; r != nil; r = r.prev {
		if r.sequence == sequence {
			return end == uintptr(r.p)+r.size
		}
	}
	return false
}

func (l *lifetime) end() uintptr {
	if l.head != nil {
		return uintptr(l.head.p) + l.head.size
	}
	return 0
}

func (l *lifetime) freeTail(p unsafe.Pointer, size, pc uintptr) bool {
	r := l.head
	if r == nil || p == nil || uintptr(p) < uintptr(r.p) || uintptr(p) > uintptr(r.p)+r.size || size != uintptr(r.p)+r.size-uintptr(p) {
		return false
	}
	if uintptr(p) == uintptr(r.p) {
		return l.reclaim(r.sequence-1, pc, true)
	}
	if genCheck {
		rt.GenTrimArena(r.token, uintptr(p), pc)
	}
	r.size = uintptr(p) - uintptr(r.p)
	return false
}

func (l *lifetime) reclaim(sequence uint64, pc uintptr, freed bool) bool {
	pending := l.head
	for l.head != nil && l.head.sequence > sequence {
		r := l.head
		l.head = r.prev
		if genCheck {
			rt.GenEndArena(r.token, pc, freed)
		}
	}
	// Complete invalidation before invoking a parent callback. A callback
	// that ends the task must not skip the rest of this detached release list.
	stop := l.head
	exiting := false
	for r := pending; r != stop; {
		next := r.prev
		if rt.CatchAllocFree(l.parent, unsafe.Pointer(r), unsafe.Sizeof(allocation{}), unsafe.Alignof(allocation{})) {
			exiting = true
		}
		r = next
	}
	return exiting
}

func (l *lifetime) resetMarks() {
	if l.reset == ^uint64(0) {
		panic("arena: mark identity exhausted")
	}
	l.reset++
}
