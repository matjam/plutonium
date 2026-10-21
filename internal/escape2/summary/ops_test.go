package summary_test

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/escape2/summary/summarytest"
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
	a := &summary.Summary{Inputs: 2, Esc: []domain.Key{k0, k0f}}
	b := &summary.Summary{Inputs: 2, Esc: []domain.Key{k0ge}}
	if !summary.Leq(ps, a, b) || summary.Leq(ps, b, a) {
		t.Error("GE root covers the input's keys, not the other way")
	}
	if !summary.Leq(ps, a, a) {
		t.Error("not reflexive")
	}
	c := &summary.Summary{Inputs: 2, Esc: []domain.Key{k1}}
	if summary.Leq(ps, c, b) {
		t.Error("another input's key is covered")
	}
	// Store pairs need both keys covered.
	s1 := &summary.Summary{Inputs: 2, Store: []summary.StorePair{{Src: k1, Dst: k0}}}
	s2 := &summary.Summary{Inputs: 2, Store: []summary.StorePair{{Src: k1, Dst: k0ge}}}
	s3 := &summary.Summary{Inputs: 2, Store: []summary.StorePair{{Src: k0, Dst: k0ge}}}
	if !summary.Leq(ps, s1, s2) || summary.Leq(ps, s1, s3) || summary.Leq(ps, s2, s1) {
		t.Error("store pair coverage")
	}
	// Results: same field, or a GE key at a prefix of the field.
	f1 := ps.Of(1)
	r1 := &summary.Summary{Inputs: 2, Res: []summary.Result{{From: []summary.ResultKey{{Key: k0, Field: f1}}}}}
	r2 := &summary.Summary{Inputs: 2, Res: []summary.Result{{From: []summary.ResultKey{{Key: k0ge, Field: domain.Root}}}}}
	r3 := &summary.Summary{Inputs: 2, Res: []summary.Result{{From: []summary.ResultKey{{Key: k0, Field: domain.Root}}}}}
	if !summary.Leq(ps, r1, r2) || summary.Leq(ps, r1, r3) {
		t.Error("result coverage")
	}
	h := &summary.Summary{Inputs: 2, Res: []summary.Result{{Heap: []domain.PathID{domain.Root}}}}
	if summary.Leq(ps, h, r2) || !summary.Leq(ps, h, h) {
		t.Error("heap results")
	}
	reg := &summary.Summary{Inputs: 2, Region: summary.Region{Builds: true, OuterResults: []int{0}}}
	if summary.Leq(ps, reg, a) || !summary.Leq(ps, reg, reg) || !summary.Leq(ps, &summary.Summary{}, nil) {
		t.Error("region facts")
	}
	if summary.Leq(ps, a, nil) || !summary.Leq(ps, nil, a) {
		t.Error("nil is the bottom summary")
	}
}

// Plan invariant I8: widening only widens, and brings every input under its
// caps.
func TestWidenCovers(t *testing.T) {
	for seed := range uint64(200) {
		rng := rand.New(rand.NewPCG(seed, 9))
		ps := summarytest.NewPaths()
		randKey := func() domain.Key {
			var steps []domain.Step
			for range rng.IntN(7) {
				steps = append(steps, []domain.Step{L, E, 0, 1, 2, 3}[rng.IntN(6)])
			}
			return key(ps, rng.IntN(3), rng.IntN(8) == 0, steps...)
		}
		s := &summary.Summary{Inputs: 3, Res: make([]summary.Result, 1)}
		n := rng.IntN(300)
		for range n {
			s.Esc = append(s.Esc, randKey())
			s.Store = append(s.Store, summary.StorePair{Src: randKey(), Dst: randKey()})
			s.Res[0].From = append(s.Res[0].From, summary.ResultKey{Key: randKey(), Field: ps.Of(domain.Step(rng.IntN(2)))})
			s.Reasons = append(s.Reasons, summary.Reason{Site: uint32(rng.IntN(5))})
		}
		for range n {
			s.Reasons = append(s.Reasons, summary.Reason{Site: uint32(rng.IntN(5))})
		}
		orig := &summary.Summary{Inputs: 3, Esc: slices.Clone(s.Esc), Store: slices.Clone(s.Store),
			Res: []summary.Result{{From: slices.Clone(s.Res[0].From)}}}
		summary.Widen(ps, s)
		if !summary.Leq(ps, orig, s) {
			t.Fatalf("seed %d: widened summary does not cover the original", seed)
		}
		count := map[int]int{}
		for _, k := range s.Esc {
			count[k.Input()]++
		}
		for in, c := range count {
			if c > summary.MaxEscPerInput {
				t.Fatalf("seed %d: input %d has %d Esc keys", seed, in, c)
			}
		}
		count = map[int]int{}
		for _, p := range s.Store {
			count[p.Src.Input()]++
		}
		for in, c := range count {
			if c > summary.MaxStorePerInput {
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
		again.Esc = slices.Clone(s.Esc)
		again.Store = slices.Clone(s.Store)
		summary.Widen(ps, &again)
		if !slices.Equal(again.Esc, s.Esc) || !slices.Equal(again.Store, s.Store) {
			t.Fatalf("seed %d: Widen is not idempotent", seed)
		}
	}
}
