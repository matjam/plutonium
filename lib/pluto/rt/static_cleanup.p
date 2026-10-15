package rt

import "unsafe"

// Static cleanup is a compiler/runtime contract (language section 15.4).
// The table has one four-word record per resource-bearing static object:
// address, size, completed prefix, and transferred-payload flag. The compiler
// owns its fixed storage; publication and progress are atomic for service readers.
type staticCleanupRecord struct {
	base        unsafe.Pointer
	size        uintptr
	done        uint64
	transferred uintptr
}

var staticCleanupTable []staticCleanupRecord
var staticCleanupReady uint32
var staticCleanupCurrent uint64
var staticCleanupTask uint64

func staticCleanupSet(p unsafe.Pointer, n uintptr) {
	staticCleanupTable = unsafe.Slice((*staticCleanupRecord)(p), n)
	store64(&staticCleanupTask, staticCleanupIdentity())
	store32(&staticCleanupReady, 1)
}

func staticCleanupSelect(i uintptr) {
	store64(&staticCleanupCurrent, uint64(uintptr(unsafe.Pointer(&staticCleanupTable[i]))))
}

// Keep task identity and progress bookkeeping out of every user Drop frame.
//
//pu:noinline
func staticCleanupPause() uint64 {
	if !staticCleanupOnTask() {
		return 0
	}
	p := load64(&staticCleanupCurrent)
	store64(&staticCleanupCurrent, 0)
	return p
}

//pu:noinline
func staticCleanupResume(p uint64) {
	if staticCleanupOnTask() {
		store64(&staticCleanupCurrent, p)
	}
}

func staticCleanupOnTask() bool {
	owner := load64(&staticCleanupTask)
	return owner != 0 && staticCleanupIdentity() == owner
}

// No initializing BootTask call on this primitive path. Before tasking there
// is only the startup context; after tasking, current uses the actual backend
// slot and nil on service contexts, without a shared nil fallback.
func staticCleanupIdentity() uint64 {
	if !tasking {
		return uint64(uintptr(unsafe.Pointer(&bootTask)))
	}
	return uint64(uintptr(unsafe.Pointer(current())))
}

// staticCleanupCheck guards program writes, including writes through aliases.
// Compiler mandatory zeroing and progress updates do not call it recursively.
// Keep its table scan out of every checked store's control-flow graph.
//
//pu:noinline
func staticCleanupCheck(p unsafe.Pointer, n uintptr) {
	if n == 0 || load32(&staticCleanupReady) == 0 {
		return
	}
	a := uintptr(p)
	for i := 0; i < len(staticCleanupTable); i++ {
		r := &staticCleanupTable[i]
		b := uintptr(r.base)
		done := uintptr(load64(&r.done))
		if done != 0 && a < b+done && (a >= b || n > b-a) {
			panicMessage("write to a finalized package resource during shutdown", "", "", "", "")
		}
	}
}

// Completion applies only to the static object currently being destroyed.
// Nested heap cleanup is independent. Declaration/index-order traversal makes
// completed inline subobjects a prefix, including intervening padding.
//
//pu:noinline
func staticCleanupDone(p unsafe.Pointer, n uintptr) bool {
	if !staticCleanupOnTask() {
		return false
	}
	r := (*staticCleanupRecord)(unsafe.Pointer(uintptr(load64(&staticCleanupCurrent))))
	if r == nil {
		return false
	}
	if staticCleanupRecordDone(r, p, n) {
		return true
	}
	// An owning interface's static payload is released by its dynamic drop,
	// not by an independent schedule entry. Its own extent must still be
	// zeroed and protected, including after a move to another interface slot.
	for i := 0; i < len(staticCleanupTable); i++ {
		r = &staticCleanupTable[i]
		if r.transferred != 0 && staticCleanupRecordDone(r, p, n) {
			return true
		}
	}
	return false
}

func staticCleanupRecordDone(r *staticCleanupRecord, p unsafe.Pointer, n uintptr) bool {
	a, b := uintptr(p), uintptr(r.base)
	if a < b || a-b > r.size || n > r.size-(a-b) {
		return false
	}
	end := a - b + n
	if n == 0 && a == b && r.size == 1 {
		end = 1
	}
	if end > uintptr(load64(&r.done)) {
		store64(&r.done, uint64(end))
	}
	return true
}
