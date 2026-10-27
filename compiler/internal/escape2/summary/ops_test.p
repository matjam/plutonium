// Ported from internal/escape2/summary/ops_test.go at the go-cutover tag.
// The Go test was in package summary_test; this one is in package summary,
// so the names of the package are not qualified.

package summary

import (
	"pluto/slices"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/summary/summarytest"
	"compiler/internal/testrand"
)

func key(ps *summarytest.Paths, in int, ge bool, steps ...domain.Step) domain.Key {
	return domain.MakeKey(in, ps.Of(steps...), domain.KeyFlags{GE: ge})
}

const (
	L = domain.StepLoad
	E = domain.StepElem
)

func TestLeq(t *testing.T) {
	ps := summarytest.NewPaths()
	k0 := key(ps, 0, false, L)
	k0ge := key(ps, 0, true)
	k0f := key(ps, 0, false, 1, L)
	k1 := key(ps, 1, false)
	a := &Summary{Inputs: 2, Esc: []domain.Key{k0, k0f}}
	b := &Summary{Inputs: 2, Esc: []domain.Key{k0ge}}
	if !Leq(ps, a, b) || Leq(ps, b, a) {
		t.Error("GE root covers the input's keys, not the other way")
	}
	if !Leq(ps, a, a) {
		t.Error("not reflexive")
	}
	c := &Summary{Inputs: 2, Esc: []domain.Key{k1}}
	if Leq(ps, c, b) {
		t.Error("another input's key is covered")
	}
	// Store pairs need both keys covered.
	s1 := &Summary{Inputs: 2, Store: []StorePair{{Src: k1, Dst: k0}}}
	s2 := &Summary{Inputs: 2, Store: []StorePair{{Src: k1, Dst: k0ge}}}
	s3 := &Summary{Inputs: 2, Store: []StorePair{{Src: k0, Dst: k0ge}}}
	if !Leq(ps, s1, s2) || Leq(ps, s1, s3) || Leq(ps, s2, s1) {
		t.Error("store pair coverage")
	}
	// Results: same field, or a GE key at a prefix of the field.
	f1 := ps.Of(1)
	r1 := &Summary{Inputs: 2, Res: []Result{{From: []ResultKey{{Key: k0, Field: f1}}}}}
	r2 := &Summary{Inputs: 2, Res: []Result{{From: []ResultKey{{Key: k0ge, Field: domain.Root}}}}}
	r3 := &Summary{Inputs: 2, Res: []Result{{From: []ResultKey{{Key: k0, Field: domain.Root}}}}}
	if !Leq(ps, r1, r2) || Leq(ps, r1, r3) {
		t.Error("result coverage")
	}
	h := &Summary{Inputs: 2, Res: []Result{{Heap: []domain.PathID{domain.Root}}}}
	if Leq(ps, h, r2) || !Leq(ps, h, h) {
		t.Error("heap results")
	}
	reg := &Summary{Inputs: 2, Region: Region{Builds: true, OuterResults: []int{0}}}
	if Leq(ps, reg, a) || !Leq(ps, reg, reg) || !Leq(ps, &Summary{}, nil) {
		t.Error("region facts")
	}
	if Leq(ps, a, nil) || !Leq(ps, nil, a) {
		t.Error("nil is the bottom summary")
	}
}

// Plan invariant I8: widening only widens, and brings every input under its
// caps.
func TestWidenCovers(t *testing.T) {
	stepSet := []domain.Step{L, E, 0, 1, 2, 3}
	for seed := uint64(0); seed < 200; seed++ {
		rng := testrand.New(seed, 9)
		ps := summarytest.NewPaths()
		randKey := func() domain.Key {
			var steps []domain.Step
			for m := rng.IntN(7); m > 0; m-- {
				steps = compat.AppendPlain(steps, stepSet[rng.IntN(6)])
			}
			return key(ps, rng.IntN(3), rng.IntN(8) == 0, steps...)
		}
		s := &Summary{Inputs: 3, Res: compat.Make[Result](1)}
		n := rng.IntN(300)
		for i := 0; i < n; i++ {
			s.Esc = compat.AppendPlain(s.Esc, randKey())
			s.Store = compat.Append(s.Store, StorePair{Src: randKey(), Dst: randKey()})
			s.Res[0].From = compat.Append(s.Res[0].From, ResultKey{Key: randKey(), Field: ps.Of(domain.Step(rng.IntN(2)))})
			s.Reasons = compat.Append(s.Reasons, Reason{Site: uint32(rng.IntN(5))})
		}
		for i := 0; i < n; i++ {
			s.Reasons = compat.Append(s.Reasons, Reason{Site: uint32(rng.IntN(5))})
		}
		orig := &Summary{Inputs: 3, Esc: compat.Clone(s.Esc), Store: compat.Clone(s.Store),
			Res: compat.Append[Result](nil, Result{From: compat.Clone(s.Res[0].From)})}
		Widen(ps, s)
		if !Leq(ps, orig, s) {
			t.Fatalf("seed %d: widened summary does not cover the original", seed)
		}
		count := map[int]int{}
		for _, k := range s.Esc {
			count[k.Input()]++
		}
		for in, c := range count {
			if c > MaxEscPerInput {
				t.Fatalf("seed %d: input %d has %d Esc keys", seed, in, c)
			}
		}
		count = map[int]int{}
		for _, p := range s.Store {
			count[p.Src.Input()]++
		}
		for in, c := range count {
			if c > MaxStorePerInput {
				t.Fatalf("seed %d: input %d has %d store pairs", seed, in, c)
			}
		}
		if len(s.Reasons) != len(s.Esc)+len(s.Store) {
			t.Fatalf("seed %d: %d reasons for %d entries", seed, len(s.Reasons), len(s.Esc)+len(s.Store))
		}
		// Antichains, sorted.
		for i, k := range s.Esc {
			for j, o := range s.Esc {
				if i != j && domain.Covers(ps, o, k) {
					t.Fatalf("seed %d: Esc is not an antichain", seed)
				}
			}
			if i > 0 && s.Esc[i-1] >= k {
				t.Fatalf("seed %d: Esc not sorted", seed)
			}
		}
		// Widening again changes nothing.
		again := *s
		again.Esc = compat.Clone(s.Esc)
		again.Store = compat.Clone(s.Store)
		Widen(ps, &again)
		if !slices.Equal(again.Esc, s.Esc) || !slices.Equal(again.Store, s.Store) {
			t.Fatalf("seed %d: Widen is not idempotent", seed)
		}
	}
}

// Past MaxStorePerInput, store pairs inside the memory an input's second
// load reaches (a store within a closure record's captured variable) are
// first cut through that load: they stay within it instead of covering the
// whole input from its first load.
func TestWidenPairsCutTwo(t *testing.T) {
	ps := summarytest.NewPaths()
	s := &Summary{Inputs: 1}
	for i := domain.Step(0); i < 4; i++ {
		for j := domain.Step(0); j < 4; j++ {
			for k := domain.Step(0); k < 4; k++ {
				for l := domain.Step(0); l < 4; l++ {
					s.Store = compat.Append(s.Store, StorePair{
						Src: key(ps, 0, false, 1, L, 0, L, i, L, j),
						Dst: key(ps, 0, false, 1, L, 0, L, k, L, l)})
					s.Reasons = compat.Append(s.Reasons, Reason{})
				}
			}
		}
	}
	Widen(ps, s)
	want := domain.MakeKey(0, ps.Of(1, L, 0, L), domain.KeyFlags{Deep: true})
	if len(s.Store) != 1 || s.Store[0].Src != want || s.Store[0].Dst != want {
		t.Fatalf("widened to %v, want one pair on %v", s.Store, want)
	}
}

// TestWidenKeysCutTwo checks that escape keys past MaxEscPerInput are cut
// through their second load first: an escape within a closure's record
// field stays within it.
func TestWidenKeysCutTwo(t *testing.T) {
	ps := summarytest.NewPaths()
	s := &Summary{Inputs: 1}
	for i := domain.Step(0); i < 9; i++ {
		for j := domain.Step(0); j < 9; j++ {
			s.Esc = compat.AppendPlain(s.Esc, key(ps, 0, false, 1, L, 0, L, i, L, j))
			s.Reasons = compat.Append(s.Reasons, Reason{})
		}
	}
	Widen(ps, s)
	want := domain.MakeKey(0, ps.Of(1, L, 0, L), domain.KeyFlags{Deep: true})
	if len(s.Esc) != 1 || s.Esc[0] != want {
		t.Fatalf("widened to %v, want %v", s.Esc, want)
	}
}

// TestWidenPairsDstFirst checks that store pairs past MaxStorePerInput
// coarsen their destinations before their source: a source stored into
// many places stays precise.
func TestWidenPairsDstFirst(t *testing.T) {
	ps := summarytest.NewPaths()
	s := &Summary{Inputs: 2}
	src := key(ps, 1, false, L)
	for i := domain.Step(0); i < 12; i++ {
		for j := domain.Step(0); j < 12; j++ {
			s.Store = compat.Append(s.Store, StorePair{Src: src, Dst: key(ps, 0, false, i, L, j, L)})
			s.Reasons = compat.Append(s.Reasons, Reason{})
		}
	}
	Widen(ps, s)
	if len(s.Store) == 0 || len(s.Store) > MaxStorePerInput {
		t.Fatalf("widened to %d pairs", len(s.Store))
	}
	for _, p := range s.Store {
		if p.Src != src {
			t.Errorf("source widened to %v, want %v", p.Src, src)
		}
	}
}
