// Translated by go2pu from internal/escape2/sched/retype.go at the go-cutover tag; edited by hand since.

package sched

import (
	"selfhost/compat"
	"selfhost/internal/escape2/eir"
	"selfhost/internal/types2"
	"selfhost/pu"
)

// retypes are the pointer conversions of the program (eir.Facts.Retypes):
// a pointer to a value of type U is a pointer to a V only where a chain of
// them leads from U to V (section 7.9: a conversion of a pointer keeps the
// address and changes the type it points to). A conversion that mentions a
// type parameter may be any of its instances, so with one the set is open.
type retypes struct {
	tt    *typeTable
	edges map[typeID][]typeID
	open  bool
	memo  map[[2]typeID]bool
}

func newRetypes(p *eir.Program, tt *typeTable) *retypes {
	r := pu.New(retypes{tt: tt, edges: map[typeID][]typeID{}, memo: map[[2]typeID]bool{}})
	for _, f := range p.Funcs {
		for _, c := range f.Facts.Retypes {
			if tt.hasTypeParam(c.From) || tt.hasTypeParam(c.To) {
				r.open = true
				continue
			}
			from, to := tt.intern(c.From), tt.intern(c.To)
			r.edges[from] = compat.AppendPlain(r.edges[from], to)
		}
	}
	return r
}

// viewedAs reports whether a pointer to a value of type u may be a pointer
// to a v: u and v are identical, or conversions lead from one to the other.
func (r *retypes) viewedAs(u, v types2.Type) bool {
	if r.open || types2.Identical(u, v) {
		return true
	}
	from, ok := r.tt.lookup(u)
	if !ok {
		return false
	}
	to, ok := r.tt.lookup(v)
	if !ok {
		return false
	}
	k := [2]typeID{from, to}
	if v, ok := r.memo[k]; ok {
		return v
	}
	seen := map[typeID]bool{from: true}
	work := []typeID{from}
	found := false
	for len(work) > 0 && !found {
		n := work[len(work)-1]
		work = work[:len(work)-1]
		for _, m := range r.edges[n] {
			if m == to {
				found = true
				break
			}
			if !seen[m] {
				seen[m] = true
				work = compat.AppendPlain(work, m)
			}
		}
	}
	r.memo[k] = found
	return found
}
