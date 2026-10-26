// Translated by go2pu from internal/escape2/summary/join.go at the go-cutover tag; edited by hand since.

package summary

import (
	"selfhost/compat"
	"selfhost/internal/escape2/domain"
	"selfhost/internal/escape2/eir"
	"selfhost/pu"
)

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
	j := pu.New(Summary{Inputs: max(a.Inputs, b.Inputs)})
	ae, as := reasonsOf(a)
	be, bs := reasonsOf(b)
	j.Esc = compat.AppendPlain(compat.AppendPlain([]domain.Key(nil), a.Esc...), b.Esc...)
	j.Store = compat.Append(compat.Append([]StorePair(nil), a.Store...), b.Store...)
	j.Reasons = compat.Append(compat.Append(compat.Append(compat.Append([]Reason(nil), ae...), be...), as...), bs...)
	j.HeapStore = compat.AppendPlain(compat.AppendPlain([]domain.Key(nil), a.HeapStore...), b.HeapStore...)
	if n := max(len(a.Res), len(b.Res)); n > 0 {
		j.Res = compat.Make[Result](n)
	}
	for _, s := range []*Summary{a, b} {
		for r, res := range s.Res {
			j.Res[r].From = compat.Append(j.Res[r].From, res.From...)
			j.Res[r].Heap = compat.AppendPlain(j.Res[r].Heap, res.Heap...)
			j.Res[r].CountedHeap = compat.AppendPlain(j.Res[r].CountedHeap, res.CountedHeap...)
		}
	}
	j.Deferred = compat.Append(compat.Append([]Deferred(nil), a.Deferred...), b.Deferred...)
	j.Region.Builds = a.Region.Builds || b.Region.Builds
	j.Region.Self = a.Region.Self || b.Region.Self
	j.Region.OuterResults = compat.AppendPlain(compat.AppendPlain([]int(nil), a.Region.OuterResults...), b.Region.OuterResults...)
	j.Region.Lits = compat.AppendPlain(compat.AppendPlain([]eir.FuncID(nil), a.Region.Lits...), b.Region.Lits...)
	Widen(ps, j)
	return j
}

// reasonsOf splits s's reasons into those of Esc and of Store, making empty
// ones when s has none.
func reasonsOf(s *Summary) (esc, store []Reason) {
	if len(s.Reasons) == len(s.Esc)+len(s.Store) {
		return s.Reasons[:len(s.Esc)], s.Reasons[len(s.Esc):]
	}
	none := compat.Make[Reason](len(s.Esc) + len(s.Store))
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
	out := compat.MakeCap[[]Reason](0, len(s.Esc)+len(s.Store))
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
					rs = compat.Append(rs, at(i)...)
				}
			}
		}
		out = compat.Append(out, rs)
	}
	for _, p := range s.Store {
		var rs []Reason
		if fresh != nil {
			for i, f := range fresh.Store {
				if domain.Covers(ps, p.Src, f.Src) && domain.Covers(ps, p.Dst, f.Dst) {
					rs = compat.Append(rs, at(len(fresh.Esc)+i)...)
				}
			}
		}
		out = compat.Append(out, rs)
	}
	return out
}
