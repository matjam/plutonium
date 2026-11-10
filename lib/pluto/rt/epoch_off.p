//pu:build !gencheck

package rt

import "unsafe"

// Without -gencheck, pointers carry no tag and arenas have no epochs
// (epoch_on.p).

// NewEpoch returns 0: there are no epochs.
func NewEpoch() uint16 { return 0 }

// EpochEnded does nothing.
func EpochEnded(e uint16, pc uintptr) {}

func epochSite(tag uintptr) uintptr { return 0 }

// genCheck reports a -gencheck build.
const genCheck = false

func checkLive(p unsafe.Pointer) {}
