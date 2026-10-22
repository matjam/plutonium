package summary

import "github.com/matjam/plutonium/internal/escape2/domain"

// Join returns a summary that covers both a and b: their entries together,
// widened (the caps and antichains of Widen). Neither argument is changed;
// nil is the bottom summary.
//
// The SCC iteration is s = Join(s, F(s)) until F(s) is Leq s. It climbs a
// finite order and so stops even where F is not monotone: Paths.Extend cuts
// a path past the limits back to its prefix through the second load, which
// may be shorter than a GE key that covers the uncut path, so solving
// against a wider summary can give a result that does not cover the
// narrower one's (main.(*Tree).Insert of testdata/run/gentypes.p oscillated
// that way). A post-fixed point F(s) ⊑ s is what soundness, and D44's check,
// need.
func Join(ps domain.Paths, a, b *Summary) *Summary {
	if a == nil {
		a = &Summary{}
	}
	if b == nil {
		b = &Summary{}
	}
	j := &Summary{Inputs: max(a.Inputs, b.Inputs)}
	ae, as := reasonsOf(a)
	be, bs := reasonsOf(b)
	j.Esc = append(append([]domain.Key(nil), a.Esc...), b.Esc...)
	j.Store = append(append([]StorePair(nil), a.Store...), b.Store...)
	j.Reasons = append(append(append(append([]Reason(nil), ae...), be...), as...), bs...)
	j.HeapStore = append(append([]domain.Key(nil), a.HeapStore...), b.HeapStore...)
	if n := max(len(a.Res), len(b.Res)); n > 0 {
		j.Res = make([]Result, n)
	}
	for _, s := range []*Summary{a, b} {
		for r, res := range s.Res {
			j.Res[r].From = append(j.Res[r].From, res.From...)
			j.Res[r].Heap = append(j.Res[r].Heap, res.Heap...)
			j.Res[r].CountedHeap = append(j.Res[r].CountedHeap, res.CountedHeap...)
		}
	}
	j.Deferred = append(append([]Deferred(nil), a.Deferred...), b.Deferred...)
	j.Region.Builds = a.Region.Builds || b.Region.Builds
	j.Region.OuterResults = append(append([]int(nil), a.Region.OuterResults...), b.Region.OuterResults...)
	Widen(ps, j)
	return j
}

// reasonsOf splits s's reasons into those of Esc and of Store, making empty
// ones when s has none.
func reasonsOf(s *Summary) (esc, store []Reason) {
	if len(s.Reasons) == len(s.Esc)+len(s.Store) {
		return s.Reasons[:len(s.Esc)], s.Reasons[len(s.Esc):]
	}
	none := make([]Reason, len(s.Esc)+len(s.Store))
	for i := range none {
		none[i] = Reason{Site: ^uint32(0), Callee: ^uint32(0), Entry: ^uint32(0)}
	}
	return none[:len(s.Esc)], none[len(s.Esc):]
}

// NoReason is the reason of an entry no solve against the final summaries
// derives: one a join kept from an earlier iterate of its SCC.
var NoReason = Reason{Site: ^uint32(0), Callee: ^uint32(0), Entry: ^uint32(0)}

// EntryDerivs gives each Esc and Store entry of s the derivations of the
// entries of fresh it covers: fresh is a solve against the final summaries
// of s's SCC (fresh ⊑ s), and derivs its derivations (solve.Result.Derivs),
// by entry of fresh. An entry of s that covers no entry of fresh has none:
// a join kept it from an earlier iterate, and nothing derives it now.
func EntryDerivs(ps domain.Paths, s, fresh *Summary, derivs [][]Reason) [][]Reason {
	if s == nil {
		return nil
	}
	out := make([][]Reason, 0, len(s.Esc)+len(s.Store))
	at := func(i int) []Reason {
		if i < len(derivs) {
			return derivs[i]
		}
		return nil
	}
	for _, k := range s.Esc {
		var rs []Reason
		if fresh != nil {
			for i, f := range fresh.Esc {
				if domain.Covers(ps, k, f) {
					rs = append(rs, at(i)...)
				}
			}
		}
		out = append(out, rs)
	}
	for _, p := range s.Store {
		var rs []Reason
		if fresh != nil {
			for i, f := range fresh.Store {
				if domain.Covers(ps, p.Src, f.Src) && domain.Covers(ps, p.Dst, f.Dst) {
					rs = append(rs, at(len(fresh.Esc)+i)...)
				}
			}
		}
		out = append(out, rs)
	}
	return out
}
