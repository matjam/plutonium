// Ported from internal/escape2/sched/overlay_test.go at the go-cutover tag.

package sched

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/testrand"
)

// overlayPair is a path of the overlay and the same path of the reference
// interner.
type overlayPair struct{ o, r domain.PathID }

func stepsEqual(a, b []domain.Step) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestOverlayPaths checks an overlay against the interner: random
// extensions of interned and own paths give the same steps, loads, cuts,
// prefixes and Cut as the interner would, the interner is never changed
// before the commit, and the commit makes the same paths in it.
//
// The Go test drew from math/rand's source seeded 1 to 20; testrand is
// math/rand/v2's PCG, seeded with the same numbers, so the cases differ.
func TestOverlayPaths(t *testing.T) {
	steps := []domain.Step{domain.StepLoad, domain.StepElem, 0, 1, 2}
	for seed := int64(1); seed <= 20; seed++ {
		rng := testrand.New(uint64(seed), 0)
		base := domain.NewPaths()
		ref := domain.NewPaths()
		// Some interned paths, in both.
		var known []domain.PathID // base IDs; ref IDs are equal for these
		known = compat.Append(known, domain.Root)
		for i := 0; i < 40; i++ {
			p := known[rng.IntN(len(known))]
			s := steps[rng.IntN(len(steps))]
			q, _ := base.Extend(p, s)
			r, _ := ref.Extend(p, s)
			if q != r {
				t.Fatalf("seed %d: the two interners differ", seed)
			}
			known = compat.Append(known, q)
		}
		ix := newPathIndex(base)
		ov := newOverlay(ix)
		baseLen := base.Len()
		// Extensions through the overlay, mirrored on ref.
		var pairs []overlayPair
		for _, k := range known {
			pairs = compat.Append(pairs, overlayPair{k, k})
		}
		for i := 0; i < 300; i++ {
			pp := pairs[rng.IntN(len(pairs))]
			s := steps[rng.IntN(len(steps))]
			oq, ocut := ov.Extend(pp.o, s)
			rq, rcut := ref.Extend(pp.r, s)
			if ocut != rcut || !stepsEqual(ov.Steps(oq), ref.Steps(rq)) || ov.Loads(oq) != ref.Loads(rq) {
				t.Fatalf("seed %d: Extend(%v, %d): overlay %v cut %v, interner %v cut %v", seed, ov.Steps(pp.o), s, ov.Steps(oq), ocut, ref.Steps(rq), rcut)
			}
			if !stepsEqual(ov.Steps(ov.Cut(oq)), ref.Steps(ref.Cut(rq))) {
				t.Fatalf("seed %d: Cut of %v differs", seed, ref.Steps(rq))
			}
			pairs = compat.Append(pairs, overlayPair{oq, rq})
		}
		if base.Len() != baseLen {
			t.Fatalf("seed %d: the interner grew during the round", seed)
		}
		for i := 0; i < 300; i++ {
			a, b := pairs[rng.IntN(len(pairs))], pairs[rng.IntN(len(pairs))]
			if ov.HasPrefix(a.o, b.o) != ref.HasPrefix(a.r, b.r) {
				t.Fatalf("seed %d: HasPrefix(%v, %v) differs", seed, ref.Steps(a.r), ref.Steps(b.r))
			}
		}
		rename := ov.commit()
		for _, pp := range pairs {
			q := pp.o
			if rename != nil {
				q = rename(pp.o)
			}
			if int(q) >= base.Len() || !stepsEqual(base.Steps(q), ref.Steps(pp.r)) {
				t.Fatalf("seed %d: committed path %v is %v", seed, ref.Steps(pp.r), base.Steps(q))
			}
		}
		if ix.n != base.Len() {
			t.Fatalf("seed %d: the index is not synced", seed)
		}
	}
}
