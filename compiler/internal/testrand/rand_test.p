package testrand

import "pluto/testing"

// TestSameAsGo checks the numbers against Go's math/rand/v2 with the same
// seeds (printed by Go 1.27).
func TestSameAsGo(t *testing.T) {
	r := New(1, 2)
	a, b := r.Uint64(), r.Uint64()
	c, d, e, f := r.IntN(6), r.IntN(1000000007), r.Uint64N(1<<40), r.IntN(8)
	if a != 14192431797130687760 || b != 11371241257079532652 || c != 4 || d != 796596584 || e != 656239327568 || f != 4 {
		t.Fatalf("got %d %d %d %d %d %d", a, b, c, d, e, f)
	}
}
