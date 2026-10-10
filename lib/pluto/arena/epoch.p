package arena

import (
	"pluto/rt"
	"unsafe"
)

// Arena epochs (memory-safety design D4; built with -gencheck).
//
// The compiler checks an access through a tagged pointer the same way
// whatever gave the pointer its tag (lib/pluto/alloc/gen.p): the tag in
// bits 63..48 must equal the uint16 that the table at the start of the
// pointer's 64 KiB-aligned chunk holds for the 16-byte granule the address
// falls in. Slabs keep slot generations there, 1 to 0x7fff; a Chunked keeps
// epochs, 0x8000 to 0xffff (rt.NewEpoch), so a stale pointer of one kind
// never matches memory now used by the other.
//
// Under -gencheck a Chunked takes every chunk whose data fits in epochData
// bytes as an epoch chunk: 64 KiB-aligned, with the table in its first
// 8 KiB and the data after it. The chunk header and epochMeta sit in the
// table's first KiB, whose entries would describe the table itself and are
// never used. Allocations in an epoch chunk start on a granule, so that each
// granule belongs to one allocation; their granules hold the arena's
// current epoch, and so does their pointer's tag. Reset, Release, Drop and
// a Free of the most recent allocation zero the granules they take back (no
// pointer has tag 0, so every stale pointer into them fails its check) and
// move the arena to a new epoch for what it hands out next, so that a stale
// pointer does not match the memory once it is handed out again either.
//
// Larger chunks, like the slab's large allocations, are untagged and
// unchecked, and so is the memory of a fixed Arena: its buffer belongs to
// the caller (a frame, static storage or another allocation), and there is
// no table for it.

const (
	epochBlock   = 1 << 16                       // an epoch chunk's alignment and largest size
	epochGranule = 16                            // bytes per table entry
	epochTable   = epochBlock / epochGranule * 2 // bytes of table
	epochData    = epochBlock - epochTable       // the most data an epoch chunk holds
	epochShift   = 48                            // the tag's place in a pointer
)

// epochMeta follows the chunk header in an epoch chunk.
type epochMeta struct {
	// raw is the parent's allocation the chunk was aligned in, or nil if
	// the parent aligned the chunk itself.
	raw unsafe.Pointer
}

// isEpoch reports whether c is an epoch chunk.
func isEpoch(c *chunk) bool { return genCheck && c.size <= epochData }

func (c *chunk) meta() *epochMeta {
	return (*epochMeta)(unsafe.Add(unsafe.Pointer(c), chunkHeader))
}

// epochEntries returns the table of the epoch chunk c.
func epochEntries(c *chunk) *[epochBlock / epochGranule]uint16 {
	return (*[epochBlock / epochGranule]uint16)(unsafe.Pointer(c))
}

// newEpochChunk takes an epoch chunk with size bytes of data from the
// parent, or returns nil if the parent is out of memory.
func (a *Chunked) newEpochChunk(size uintptr) *chunk {
	n := epochTable + size
	p := a.parent.Alloc(n, epochBlock)
	if p == nil {
		return nil
	}
	var raw unsafe.Pointer
	base := uintptr(p) // the address without a tag
	if base&(epochBlock-1) != 0 {
		// The parent does not align this far: align in a larger block.
		a.parent.Free(p, n, epochBlock)
		raw = a.parent.Alloc(n+epochBlock, epochGranule)
		if raw == nil {
			return nil
		}
		base = (uintptr(raw) + epochBlock - 1) &^ (epochBlock - 1)
		n += epochBlock
	}
	a.held += n
	c := (*chunk)(unsafe.Pointer(base))
	c.size = size
	c.meta().raw = raw
	return c
}

// freeEpochChunk returns the epoch chunk c to the parent.
func (a *Chunked) freeEpochChunk(c *chunk) {
	n := epochTable + c.size
	if raw := c.meta().raw; raw != nil {
		a.held -= n + epochBlock
		a.parent.Free(raw, n+epochBlock, epochGranule)
		return
	}
	a.held -= n
	a.parent.Free(unsafe.Pointer(c), n, epochBlock)
}

// tagEpoch gives the size bytes at p, just allocated in the epoch chunk c,
// the arena's epoch, and returns p tagged with it.
func (a *Chunked) tagEpoch(c *chunk, p unsafe.Pointer, size uintptr) unsafe.Pointer {
	if a.epoch == 0 {
		a.epoch = rt.NewEpoch()
	}
	t := epochEntries(c)
	off := uintptr(p) - uintptr(unsafe.Pointer(c))
	for i := off / epochGranule; i < (off+size+epochGranule-1)/epochGranule; i++ {
		t[i] = a.epoch
	}
	return unsafe.Pointer(uintptr(p) | uintptr(a.epoch)<<epochShift)
}

// endEpoch takes back data bytes [from, to) of the epoch chunk c: their
// granules no longer match any pointer, and the arena's next allocation
// starts a new epoch. pc is the return address of the call taking them
// back, and freed says whether that was a Free (for the panic a stale
// pointer then causes, rt.EpochEnded). A granule that from falls inside
// belongs to an allocation made before from, and stays.
func (a *Chunked) endEpoch(c *chunk, from, to, pc uintptr, freed bool) {
	t := epochEntries(c)
	last := uint16(0)
	for i := (epochTable + from + epochGranule - 1) / epochGranule; i < (epochTable+to+epochGranule-1)/epochGranule; i++ {
		if e := t[i]; e != 0 {
			if e != last {
				rt.EpochEnded(e, pc, freed)
				last = e
			}
			t[i] = 0
		}
	}
	a.epoch = 0
}
