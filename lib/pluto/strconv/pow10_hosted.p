//pu:build !kernel

package strconv

import "pluto/rt"

// pow10 returns the 128-bit mantissa of 10**p, p in [-348, 347], as
// hi<<64 - lo with the high bit of hi set: Go's internal/strconv pow10Tab,
// kept once, in package rt, which prints floats from it.
func pow10(p int) (hi, lo uint64) { return rt.Pow10(p) }
