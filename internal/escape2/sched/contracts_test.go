package sched

import (
	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// TestJoin checks the join of candidate summaries: every entry, the most
// inputs, an antichain, reasons naming the candidate.
func TestJoin(t *testing.T) {
	ps := domain.NewPaths()
	k := func(in int) domain.Key { return domain.MakeKey(in, domain.Root, domain.KeyFlags{}) }
	k0 := k(0)
	sums := map[eir.FuncID]*summary.Summary{
		1: {Inputs: 1, Esc: []domain.Key{k(0)}, Reasons: []summary.Reason{{Site: ^uint32(0), Callee: ^uint32(0), Pos: 77, Sink: uint8(eir.SinkGlobal)}}},
		2: {Inputs: 3, Esc: []domain.Key{k(0), k(2)}, Store: []summary.StorePair{{Src: k(1), Dst: k(0)}},
			Res: []summary.Result{{Heap: []domain.PathID{domain.Root}}}},
	}
	s := join(ps, []eir.FuncID{1, 2, 3}, func(id eir.FuncID) *summary.Summary { return sums[id] })
	if s.Inputs != 3 || len(s.Esc) != 2 || len(s.Store) != 1 || len(s.Res) != 1 || len(s.Res[0].Heap) != 1 {
		t.Fatalf("join: %+v", s)
	}
	if len(s.Reasons) != len(s.Esc)+len(s.Store) {
		t.Errorf("reasons %v do not follow the entries", s.Reasons)
	}
	// The reason of in0's key names member 1 and carries its position and
	// sink kind.
	for e, k := range s.Esc {
		if k == k0 {
			if r := s.Reasons[e]; r.Callee != 1 || r.Pos != 77 || r.Sink != uint8(eir.SinkGlobal) {
				t.Errorf("reason of in0's key: %+v, want member 1 with Pos 77 and Sink global", r)
			}
		}
	}
	if !summary.Leq(ps, sums[1], s) || !summary.Leq(ps, sums[2], s) {
		t.Errorf("the join does not cover its members")
	}
}

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
