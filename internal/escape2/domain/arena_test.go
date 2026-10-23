package domain

import "testing"

// TestArenaAllocated: Allocated counts what Alloc handed out since the last
// Reset, not the chunks the arena keeps for reuse, so the same work measures
// the same after a Reset whatever ran before it.
func TestArenaAllocated(t *testing.T) {
	a := NewArena()
	work := func() {
		for i := 0; i < 1000; i++ {
			a.Alloc(1 + i%50)
		}
		a.Alloc(chunkWords) // a big allocation
	}
	work()
	first := a.Allocated()
	if first == 0 || first > a.Bytes() {
		t.Fatalf("Allocated %d, Bytes %d", first, a.Bytes())
	}
	a.Reset()
	if a.Allocated() != 0 {
		t.Fatalf("Allocated after Reset: %d", a.Allocated())
	}
	for i := 0; i < 300; i++ {
		a.Alloc(7) // leave the current chunk part used
	}
	before := a.Allocated()
	work()
	if got := a.Allocated() - before; got != first {
		t.Fatalf("the same work allocated %d, then %d", first, got)
	}
}
