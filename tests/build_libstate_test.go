package tests

import (
	"strings"
	"testing"
)

// mapRebuildPanicSource grows a hash.Map whose hash function panics while
// the entries move to the bigger table. The deferred call then reads the
// map: spec section 17.2 says the library's types restore their
// invariants before a check that can panic, so every entry is still there.
const mapRebuildPanicSource = `package main

import "pluto/hash"

var m hash.Map[int, int]
var bad bool

func hashInt(k int) uint64 {
	if bad && k == 3 {
		panic("hash of 3")
	}
	return uint64(k)
}

func main() {
	m.InitFunc(0, hashInt)
	defer func() {
		bad = false
		sum := 0
		for k := 0; k < 6; k++ {
			v, ok := m.Get(k)
			if ok {
				sum += v
			}
		}
		println("len", m.Len(), "sum", sum)
	}()
	for k := 0; k < 6; k++ {
		m.Set(k, k+1)
	}
	bad = true
	m.Set(100, 0)
	println("unreachable")
}
`

// TestMapRebuildPanic checks that a panic in the hash function during a
// rebuild leaves the map as it was.
func TestMapRebuildPanic(t *testing.T) {
	t.Parallel()
	out, status := compileAndRun(t, []byte(mapRebuildPanicSource), 2)
	if status != 2 || !strings.Contains(out, "len 6 sum 21\n") || !strings.Contains(out, "hash of 3") || strings.Contains(out, "unreachable") {
		t.Errorf("exit status %d, output %q; want 2, the panic, and len 6 sum 21", status, out)
	}
}
