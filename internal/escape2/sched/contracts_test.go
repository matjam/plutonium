package sched

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
)

// TestContractSummaries checks that contract-table entries become the
// summaries their effects state.
func TestContractSummaries(t *testing.T) {
	ps := domain.NewPaths()
	c := newContractSummaries(ps)
	load0, _ := ps.Extend(domain.Root, domain.StepLoad)

	// InitValue(p, x) stores x into *p.
	s, ok := c.lookup("pluto/rt.InitValue")
	if !ok || len(s.Store) != 1 || s.Inputs != 2 {
		t.Fatalf("InitValue: %+v %v", s, ok)
	}
	if p := s.Store[0]; p.Src != domain.MakeKey(1, domain.Root, domain.KeyFlags{}) || p.Dst != domain.MakeKey(0, domain.Root, domain.KeyFlags{}) {
		t.Errorf("InitValue store %+v", p)
	}
	// CopyValue(p) returns *p.
	s, _ = c.lookup("pluto/rt.CopyValue")
	if len(s.Res) != 1 || len(s.Res[0].From) != 1 || s.Res[0].From[0].Key != domain.MakeKey(0, load0, domain.KeyFlags{}) {
		t.Errorf("CopyValue: %+v", s)
	}
	// errors.New's result is static memory.
	s, _ = c.lookup("pluto/errors.New")
	if len(s.Res) != 1 || len(s.Res[0].Heap) != 1 {
		t.Errorf("errors.New: %+v", s)
	}
	if _, ok := c.lookup("pluto/rt.noSuchEntry"); ok {
		t.Errorf("an unknown name has a summary")
	}
	if s2, _ := c.lookup("pluto/errors.New"); s2 != s {
		t.Errorf("summaries are not made once per entry")
	}
}
