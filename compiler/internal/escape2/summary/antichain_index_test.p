// Ported from internal/escape2/summary/antichain_index_test.go at the go-cutover tag.

package summary

import (
	"pluto/slices"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/testrand"
)

// The antichains as they were made before the index: every two compared.
// The indexed versions must give exactly what these give.

func antichainKeysQuadratic(ps domain.Paths, keys []domain.Key, why []Reason) ([]domain.Key, []Reason) {
	idx := compat.Make[int](len(keys))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return cmpU64(uint64(keys[a]), uint64(keys[b])) })
	var out []domain.Key
	var outWhy []Reason
	for _, i := range idx {
		k := keys[i]
		covered := false
		for j, o := range keys {
			if j != i && domain.Covers(ps, o, k) && (o != k || j < i) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		out = compat.AppendPlain(out, k)
		if why != nil {
			outWhy = compat.Append(outWhy, why[i])
		}
	}
	return out, outWhy
}

func antichainPairsQuadratic(ps domain.Paths, pairs []StorePair, why []Reason) ([]StorePair, []Reason) {
	idx := compat.Make[int](len(pairs))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		if c := cmpU64(uint64(pairs[a].Src), uint64(pairs[b].Src)); c != 0 {
			return c
		}
		return cmpU64(uint64(pairs[a].Dst), uint64(pairs[b].Dst))
	})
	var out []StorePair
	var outWhy []Reason
	for _, i := range idx {
		p := pairs[i]
		covered := false
		for j, o := range pairs {
			if j != i && domain.Covers(ps, o.Src, p.Src) && domain.Covers(ps, o.Dst, p.Dst) && (o != p || j < i) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		out = compat.Append(out, p)
		if why != nil {
			outWhy = compat.Append(outWhy, why[i])
		}
	}
	return out, outWhy
}

// deepEqual is reflect.DeepEqual for slices of comparable elements: the
// same elements, and nil only when the other is nil.
func deepEqual[E comparable](a, b []E) bool {
	return (a == nil) == (b == nil) && slices.Equal(a, b)
}

// TestAntichainIndex checks the indexed antichains against the quadratic
// ones on random keys: inputs, paths with loads and fields, GE, FieldsGE,
// Borrowed and Counts, duplicates, with and without reasons.
func TestAntichainIndex(t *testing.T) {
	steps := []domain.Step{domain.StepLoad, domain.StepElem, 0, 1}
	for seed := int64(1); seed <= 300; seed++ {
		rng := testrand.New(uint64(seed), 0)
		ps := domain.NewPaths()
		paths := compat.AppendPlain[domain.PathID](nil, domain.Root)
		for i := 0; i < 30; i++ {
			q, _ := ps.Extend(paths[rng.IntN(len(paths))], steps[rng.IntN(len(steps))])
			paths = compat.AppendPlain(paths, q)
		}
		key := func() domain.Key {
			f := domain.KeyFlags{GE: rng.IntN(3) == 0, Borrowed: rng.IntN(5) == 0, Counts: rng.IntN(7) == 0}
			if !f.GE {
				f.FieldsGE = rng.IntN(4) == 0
				f.Deep = !f.FieldsGE && rng.IntN(4) == 0
			}
			return domain.MakeKey(rng.IntN(3), paths[rng.IntN(len(paths))], f)
		}
		n := rng.IntN(120)
		keys := compat.Make[domain.Key](n)
		pairs := compat.Make[StorePair](n)
		why := compat.Make[Reason](n)
		for i := range keys {
			keys[i] = key()
			pairs[i] = StorePair{key(), key()}
			if i > 0 && rng.IntN(6) == 0 {
				keys[i] = keys[rng.IntN(i)]
				pairs[i] = pairs[rng.IntN(i)]
			}
			why[i] = Reason{Site: uint32(i)}
		}
		for pass := 0; pass < 2; pass++ {
			var w []Reason
			if pass == 1 {
				w = why
			}
			gk, gw := antichainKeys(ps, compat.Clone(keys), w)
			wk, ww := antichainKeysQuadratic(ps, compat.Clone(keys), w)
			if !deepEqual(gk, wk) || !deepEqual(gw, ww) {
				t.Fatalf("seed %d: antichainKeys differs from the quadratic one", seed)
			}
			gp, gpw := antichainPairs(ps, compat.Clone(pairs), w)
			wp, wpw := antichainPairsQuadratic(ps, compat.Clone(pairs), w)
			if !deepEqual(gp, wp) || !deepEqual(gpw, wpw) {
				t.Fatalf("seed %d: antichainPairs differs from the quadratic one", seed)
			}
		}
	}
}
