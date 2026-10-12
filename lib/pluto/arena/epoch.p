package arena

import (
	"pluto/rt"
	"unsafe"
)

// Chunked uses a contiguous payload preceded by metadata, whatever its
// size. Stable runtime shadow tables outlive returned backing storage;
// saved original parent pointers preserve nested generation checks.
const (
	epochBlock   = 1 << 16
	epochGranule = 16
	epochTable   = 8192
	epochData    = epochBlock - epochTable
	epochShift   = 48
)

type epochMeta struct {
	raw       unsafe.Pointer
	token     uintptr
	dataOff   uintptr
	held      uintptr
	alignment uintptr
}

func isEpoch(c *chunk) bool { return genCheck }
func (c *chunk) meta() *epochMeta {
	return (*epochMeta)(unsafe.Add(unsafe.Pointer(c), chunkHeader))
}
func epochEntries(c *chunk) []uint16 {
	return unsafe.Slice((*uint16)(unsafe.Add(unsafe.Pointer(c), 64)), (c.size+15)/16)
}

func (a *Chunked) newEpochChunk(size uintptr) *chunk {
	if size > ^uintptr(0)/4 {
		return nil
	}
	off := uintptr(epochTable)
	if size > epochData {
		off = (64 + (size+15)/16*2 + 15) &^ 15
	}
	n := off + size
	p := a.parent.Alloc(n, epochBlock)
	if p == nil {
		return nil
	}
	raw, alignment := p, uintptr(epochBlock)
	base := uintptr(p)
	if base&(epochBlock-1) != 0 {
		a.parent.Free(p, n, epochBlock)
		n += epochBlock
		raw = a.parent.Alloc(n, epochGranule)
		if raw == nil {
			return nil
		}
		alignment = epochGranule
		base = (uintptr(raw) + epochBlock - 1) &^ (epochBlock - 1)
	}
	// Keep the parent's tag on accesses to this chunk's own header/table.
	bits := *(*uintptr)(unsafe.Pointer(&raw))
	c := (*chunk)(unsafe.Pointer(base | bits>>48<<48))
	c.size = size
	m := c.meta()
	m.raw, m.dataOff, m.held, m.alignment = raw, off, n, alignment
	m.token = rt.GenRegister(unsafe.Pointer(base+off), size, raw, 0)
	if m.token == 0 {
		a.parent.Free(raw, n, alignment)
		return nil
	}
	a.held += n
	return c
}

func (a *Chunked) freeEpochChunk(c *chunk) {
	m := c.meta()
	raw, n, alignment, token := m.raw, m.held, m.alignment, m.token
	rt.GenUnregister(token)
	a.held -= n
	a.parent.Free(raw, n, alignment)
}

func (a *Chunked) tagEpoch(c *chunk, p unsafe.Pointer, size uintptr) unsafe.Pointer {
	if a.epoch == 0 {
		a.epoch = rt.NewEpoch()
	}
	start := uintptr(p) - uintptr(c.data())
	t := epochEntries(c)
	for i := start / 16; i < (start+size+15)/16; i++ {
		t[i] = a.epoch
	}
	rt.GenSetEpoch(c.meta().token, p, size, a.epoch)
	return unsafe.Pointer(uintptr(p) | uintptr(a.epoch)<<epochShift)
}

func (a *Chunked) endEpoch(c *chunk, from, to, pc uintptr, freed bool) {
	t := epochEntries(c)
	last := uint16(0)
	for i := (from + 15) / 16; i < (to+15)/16; i++ {
		if e := t[i]; e != 0 {
			if e != last {
				rt.EpochEnded(e, pc, freed)
				last = e
			}
			t[i] = 0
		}
	}
	base := uintptr(c.data())
	rt.GenInvalidate(c.meta().token, base+from, base+to)
	a.epoch = 0
}
