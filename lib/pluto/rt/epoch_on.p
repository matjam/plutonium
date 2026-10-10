//pu:build gencheck

package rt

import "unsafe"

// Arena epochs (memory-safety design D4, built with -gencheck). A pointer's
// tag (bits 63..48) holds a slab generation, 1 to 0x7fff, or an arena
// epoch, 0x8000 to 0xffff. Arenas (pluto/arena) take their epochs from one
// program-wide counter, so that a new arena, which is often given the chunk
// a dropped one had, never reuses a recent epoch; the counter wraps after
// 32768 epochs. EpochEnded records, per epoch, where memory of that epoch
// was last given back, for the panic a stale pointer then causes
// (panicStale).

// epochCounter counts the epochs handed out.
var epochCounter uint32

// epochSites holds, per epoch, the return address of the call that last
// gave back memory of that epoch; bit 63 is set when it was a Free rather
// than a Reset, Release or Drop.
var epochSites [epochCount]uintptr

const epochCount = 1 << 15

// NewEpoch returns an arena epoch not handed out in the last 32767 calls.
func NewEpoch() uint16 {
	return uint16(epochCount | xadd32(&epochCounter, 1)&(epochCount-1))
}

// EpochEnded records that memory of epoch e was given back by the call
// that returns to pc: by Free if freed, by a Reset, Release or Drop
// otherwise.
func EpochEnded(e uint16, pc uintptr, freed bool) {
	if freed {
		pc |= 1 << 63
	}
	epochSites[uintptr(e)&(epochCount-1)] = pc
}

// epochSite returns what EpochEnded last recorded for the epoch tag.
func epochSite(tag uintptr) uintptr { return epochSites[tag&(epochCount-1)] }

// genCheck reports a -gencheck build: pointers carry generation tags.
const genCheck = true

// checkLive panics if p, a pointer the runtime is about to use for a
// reference count or a box's header, carries a tag that its memory no
// longer has: the compiler checks such accesses in Plutonium code, but not
// in the runtime, which handles raw addresses (lib/pluto/alloc/gen.p).
func checkLive(p unsafe.Pointer) {
	raw := *(*uintptr)(unsafe.Pointer(&p))
	tag := raw >> 48
	if tag == 0 {
		return
	}
	a := raw << 16 >> 16
	if uintptr(*(*uint16)(unsafe.Pointer(a&^0xffff + (a&0xffff)>>4*2))) != tag {
		panicStale(p)
	}
}
