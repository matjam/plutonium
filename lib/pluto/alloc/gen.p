package alloc

import "unsafe"

// Generational references (prototype; see internal/opt/gencheck.go).
//
// With the gencheck tag, the slab takes its chunks 64 KiB-aligned. The
// first 8 KiB of a chunk hold one uint16 generation for every 16-byte
// granule of the chunk; the slots that overlap them are never handed out.
// Every granule of a slot holds the slot's generation, so a pointer into
// the middle of an allocation finds it as easily as one to its start. A
// small allocation's pointer carries the generation in bits 63..48;
// generations start at 1, so a zero tag means "not from the slab". Free
// checks that the pointer is current and moves the slot to the next
// generation, and retires a slot whose generation would wrap rather than
// let an old pointer match again.

const (
	genChunkSize = 1 << 16
	genGranule   = 16
	genTagShift  = 48
	genHeader    = genChunkSize / genGranule * 2 // bytes of generations per chunk
	genMax       = 0x8000                        // the first tag that is an arena epoch
)

// genAt returns the generation of granule i of a chunk. It is a pointer to
// the element rather than to the whole table, so that an access needs no
// nil check (language spec section 17.5): chunk is never nil.
func genAt(chunk, i uintptr) *uint16 {
	return (*uint16)(unsafe.Add(unsafe.Pointer(chunk), i*2))
}

// genFirst returns the index of the first slot of size slot past the
// generation array of its chunk.
func genFirst(slot uintptr) uintptr {
	return (genHeader + slot - 1) / slot
}

// genInit gives every usable granule of a fresh chunk generation 1.
func genInit(chunk unsafe.Pointer) {
	for i := uintptr(genHeader / genGranule); i < genChunkSize/genGranule; i++ {
		*genAt(uintptr(chunk), i) = 1
	}
}

// genTag returns p, a plain slot address, tagged with the slot's
// generation.
func genTag(p unsafe.Pointer) unsafe.Pointer {
	a := uintptr(p)
	g := uintptr(*genAt(a&^(genChunkSize-1), (a&(genChunkSize-1))/genGranule))
	return unsafe.Pointer(a | g<<genTagShift)
}

// genRelease checks that p, a tagged pointer to a slot of size slot being
// freed, is current, and moves the slot to the next generation. It reports
// false when the slot is worn out and must not be reused. It is on every
// Free's path, so it is inlined into the slabs' Free; the compiler knows it
// by name (opt.genFreeFuncs), as what makes memory stale.
//
//pu:inline
func genRelease(p unsafe.Pointer, slot uintptr) bool {
	raw := *(*uintptr)(unsafe.Pointer(&p)) // the bits, tag included
	tag := raw >> genTagShift
	if tag == 0 {
		return true
	}
	a := uintptr(p) // the plain address
	chunk := a &^ (genChunkSize - 1)
	first := (a & (genChunkSize - 1)) / genGranule
	if uintptr(*genAt(chunk, first)) != tag {
		panic("alloc: Free of memory that was already freed")
	}
	next := uint16(tag + 1)
	if next == genMax {
		next = 0 // worn out: no pointer matches
	}
	for i := first; i < first+slot/genGranule; i++ {
		*genAt(chunk, i) = next
	}
	return next != 0
}

// genChunk returns a 64 KiB-aligned chunk of zeroed pages from pages with
// its generations set up.
func genChunk(pages PageProvider) unsafe.Pointer {
	chunk := alignedPages(pages, genChunkSize, genChunkSize)
	if chunk != nil {
		genInit(chunk)
	}
	return chunk
}
