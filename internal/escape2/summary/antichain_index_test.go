package summary

import (
	"math/rand"
	"reflect"
	"slices"
	"testing"

	"github.com/matjam/plutonium/internal/escape2/domain"
)

// The antichains as they were made before the index: every two compared.
// The indexed versions must give exactly what these give.

func antichainKeysQuadratic(ps domain.Paths, keys []domain.Key, why []Reason) ([]domain.Key, []Reason) {
	idx := make([]int, len(keys))
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
		out = append(out, k)
		if why != nil {
			outWhy = append(outWhy, why[i])
		}
	}
	return out, outWhy
}

func antichainPairsQuadratic(ps domain.Paths, pairs []StorePair, why []Reason) ([]StorePair, []Reason) {
	idx := make([]int, len(pairs))
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
		out = append(out, p)
		if why != nil {
			outWhy = append(outWhy, why[i])
		}
	}
	return out, outWhy
}

// TestAntichainIndex checks the indexed antichains against the quadratic
// ones on random keys: inputs, paths with loads and fields, GE, FieldsGE,
// Borrowed and Counts, duplicates, with and without reasons.
func TestAntichainIndex(t *testing.T) {
	steps := []domain.Step{domain.StepLoad, domain.StepElem, 0, 1}
	for seed := int64(1); seed <= 300; seed++ {
		rng := rand.New(rand.NewSource(seed))
		ps := domain.NewPaths()
		paths := []domain.PathID{domain.Root}
		for i := 0; i < 30; i++ {
			q, _ := ps.Extend(paths[rng.Intn(len(paths))], steps[rng.Intn(len(steps))])
			paths = append(paths, q)
		}
		key := func() domain.Key {
			f := domain.KeyFlags{GE: rng.Intn(3) == 0, Borrowed: rng.Intn(5) == 0, Counts: rng.Intn(7) == 0}
			if !f.GE {
				f.FieldsGE = rng.Intn(4) == 0
				f.Deep = !f.FieldsGE && rng.Intn(4) == 0
			}
			return domain.MakeKey(rng.Intn(3), paths[rng.Intn(len(paths))], f)
		}
		n := rng.Intn(120)
		keys := make([]domain.Key, n)
		pairs := make([]StorePair, n)
		why := make([]Reason, n)
		for i := range keys {
			keys[i] = key()
			pairs[i] = StorePair{key(), key()}
			if i > 0 && rng.Intn(6) == 0 {
				keys[i] = keys[rng.Intn(i)]
				pairs[i] = pairs[rng.Intn(i)]
			}
			why[i] = Reason{Site: uint32(i)}
		}
		for _, w := range [][]Reason{nil, why} {
			gk, gw := antichainKeys(ps, slices.Clone(keys), w)
			wk, ww := antichainKeysQuadratic(ps, slices.Clone(keys), w)
			if !reflect.DeepEqual(gk, wk) || !reflect.DeepEqual(gw, ww) {
				t.Fatalf("seed %d: antichainKeys differs from the quadratic one", seed)
			}
			gp, gpw := antichainPairs(ps, slices.Clone(pairs), w)
			wp, wpw := antichainPairsQuadratic(ps, slices.Clone(pairs), w)
			if !reflect.DeepEqual(gp, wp) || !reflect.DeepEqual(gpw, wpw) {
				t.Fatalf("seed %d: antichainPairs differs from the quadratic one", seed)
			}
		}
	}
}
