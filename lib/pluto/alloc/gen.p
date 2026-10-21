package alloc

import (
	"pluto/rt"
	"unsafe"
)

// Slab chunks retain their existing metadata prefix, but every checked
// access uses the runtime directory (ABI section 1.3). Persistent address
// history belongs to the runtime, including after mappings are reused.
const (
	genChunkSize = 1 << 16
	genGranule   = 16
	genHeader    = genChunkSize / genGranule * 2
)

func genFirst(slot uintptr) uintptr { return (genHeader + slot - 1) / slot }

func genChunk(pages PageProvider, owner uintptr) unsafe.Pointer {
	p, raw, _ := genPages(pages, genChunkSize, genChunkSize)
	if p != nil {
		token := rt.GenRegister(unsafe.Add(p, genHeader), genChunkSize-genHeader, raw, 0)
		rt.GenSlabOwner(token, owner)
		*(*uintptr)(p) = token
	}
	return p
}

// genLive is rt.GenRegisteredLive with its test of an untagged pointer
// (an allocator made from pages, as the default one is) done inline.
//
//pu:inline
func genLive(p unsafe.Pointer) bool {
	return *(*uintptr)(unsafe.Pointer(&p))>>48 == 0 || rt.GenRegisteredLive(p)
}

func genBacking(p unsafe.Pointer, owner uintptr) {
	if genCheck && !rt.GenSlabBackingLive(p, owner) {
		panic("alloc: slab backing memory was reclaimed")
	}
}

func genTag(p unsafe.Pointer, slot uintptr) unsafe.Pointer {
	base := uintptr(p) &^ (genChunkSize - 1)
	token := *(*uintptr)(unsafe.Pointer(base))
	return rt.GenTagHeap(token, p, slot)
}

//pu:inline
func genRelease(p unsafe.Pointer, slot uintptr) bool {
	if !rt.GenRegisteredLive(p) {
		panic("alloc: Free of memory that was already freed")
	}
	base := uintptr(p) &^ (genChunkSize - 1)
	token := *(*uintptr)(unsafe.Pointer(base))
	return rt.GenFreeHeap(token, p, slot)
}

// genLargeHeader sits immediately before the aligned contiguous payload.
// The granule table's physical prefix is mirrored by stable runtime
// metadata, which remains readable after this complete mapping is freed.
type genLargeHeader struct {
	raw   unsafe.Pointer
	pages uintptr
	token uintptr
}

// genPages aligns inside a complete provider allocation. Keeping its
// original tagged pointer avoids splitting nested arena allocations or
// losing their lifetime identity while trimming alignment padding.
func genPages(pages PageProvider, size, align uintptr) (unsafe.Pointer, unsafe.Pointer, uintptr) {
	if size > ^uintptr(0)/4 || align > ^uintptr(0)/4 {
		return nil, nil, 0
	}
	n := (size + PageSize - 1) / PageSize
	if align > PageSize {
		n += align/PageSize - 1
	}
	raw := pages.AllocPages(n)
	if raw == nil {
		return nil, nil, 0
	}
	base := (uintptr(raw) + align - 1) &^ (align - 1)
	return unsafe.Pointer(base), raw, n
}

func genLarge(pages PageProvider, size, align uintptr) unsafe.Pointer {
	if size == 0 {
		size = 1
	}
	if align < 16 {
		align = 16
	}
	if size > ^uintptr(0)/4 || align > ^uintptr(0)/4 {
		return nil
	}
	table := (size + 15) / 16 * 2
	off := (64 + table + unsafe.Sizeof(genLargeHeader{}) + align - 1) &^ (align - 1)
	n := off + size
	base, raw, pageCount := genPages(pages, n, max(align, uintptr(genChunkSize)))
	if base == nil {
		return nil
	}
	p := unsafe.Add(base, off)
	h := (*genLargeHeader)(unsafe.Add(p, -int(unsafe.Sizeof(genLargeHeader{}))))
	h.raw, h.pages = raw, pageCount
	h.token = rt.GenRegister(p, size, raw, 0)
	if h.token == 0 {
		pages.FreePages(raw, pageCount)
		return nil
	}
	tagged := rt.GenTagHeap(h.token, p, size)
	if tagged == nil {
		// Exhausted address history: retain the complete mapping rather than
		// returning its prefix or payload to the provider.
		rt.GenQuarantine(h.token)
		return nil
	}
	bits := *(*uintptr)(unsafe.Pointer(&tagged))
	t := unsafe.Slice((*uint16)(unsafe.Add(base, 64)), (size+15)/16)
	for i := range t {
		t[i] = uint16(bits >> 48)
	}
	return tagged
}

func genFreeLarge(pages PageProvider, p unsafe.Pointer, size uintptr) {
	if !rt.GenRegisteredLive(p) {
		panic("alloc: Free of memory that was already freed")
	}
	h := (*genLargeHeader)(unsafe.Add(unsafe.Pointer(uintptr(p)), -int(unsafe.Sizeof(genLargeHeader{}))))
	rt.GenFreeHeap(h.token, p, max(size, uintptr(1)))
	raw, n, token := h.raw, h.pages, h.token
	rt.GenUnregister(token)
	pages.FreePages(raw, n)
}
