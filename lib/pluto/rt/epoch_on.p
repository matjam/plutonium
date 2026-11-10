//pu:build gencheck

package rt

import "unsafe"

// Arena epochs (memory-safety design D4, built with -gencheck). A pointer's
// tag (bits 63..48) holds a slab generation, 1 to 0x7fff, or an arena
// epoch, 0x8000 to 0xffff. Compatibility epochs never wrap. Arena allocations
// now use persistent address history (gen_arena_on.p). EpochEnded records where
// memory of a compatibility epoch
// was last given back, for the panic a stale pointer then causes
// (panicStale).

// epochCounter counts the epochs handed out.
var epochCounter uint32

// epochSites holds, per epoch, the return address of the call that last
// gave back memory of that epoch; bit 63 is set when it was a Free rather
// than a Reset, Release or Drop.
var epochSites [epochCount]uintptr

const epochCount = 1 << 15

// NewEpoch returns a never-reissued compatibility epoch, or fails loudly.
func NewEpoch() uint16 {
	for {
		old := load32(&epochCounter)
		if old >= epochCount {
			panic("arena: incarnation exhausted")
		}
		if cas32(&epochCounter, old, old+1) {
			return uint16(epochCount | old)
		}
	}
}

// EpochEnded records that memory of epoch e was given back by the Reset,
// Release or Drop that returns to pc.
func EpochEnded(e uint16, pc uintptr) {
	epochSites[uintptr(e)&(epochCount-1)] = pc
}

// epochSite returns what EpochEnded last recorded for the epoch tag.
func epochSite(tag uintptr) uintptr { return epochSites[tag&(epochCount-1)] }

// genCheck reports a -gencheck build: pointers carry generation tags.
const genCheck = true

// checkLive panics if p, a pointer the runtime is about to use for a
// reference count or a box's header, carries a tag that its memory no
// longer has. Header and atomic count helpers validate their actual count
// address before handing its untagged address to assembly.
func checkLive(p unsafe.Pointer) {
	raw := *(*uintptr)(unsafe.Pointer(&p))
	// Untagged: the bits equal their sign extension from bit 47 (genTagOf).
	// This is inlined into every count and header use, so it spells the
	// test out rather than adding an inlined call to each site's trace
	// records.
	if raw == uintptr(int(raw<<16)>>16) {
		return
	}
	if !GenRegisteredLive(p) {
		panicStale(p)
	}
}
