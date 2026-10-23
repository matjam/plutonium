package refsolve

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// Canon renders a result in a form that does not depend on how paths are
// numbered or on the order of entries, so two solvers with separate path
// interners can be compared by string equality, and a difference can be read.
// A finding is compared by code, position, function and object: the rules
// report each (code, object, position) once, and which call or entry is named
// when several reach it is not fixed by the table.
func Canon(ps domain.Paths, r solve.Result) string {
	var b strings.Builder
	if r.Summary == nil {
		b.WriteString("summary: nil\n")
	} else {
		canonSummary(&b, ps, r.Summary)
	}
	fs := make([]string, 0, len(r.Findings))
	for _, f := range r.Findings {
		fs = append(fs, fmt.Sprintf("finding %v pos=%d func=%d obj=%d", f.Code, f.Pos, f.Func, f.Obj))
	}
	sort.Strings(fs)
	for _, s := range fs {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	return b.String()
}

func canonSummary(b *strings.Builder, ps domain.Paths, s *summary.Summary) {
	fmt.Fprintf(b, "inputs %d\n", s.Inputs)
	section := func(name string, items []string) {
		sort.Strings(items)
		for _, it := range items {
			fmt.Fprintf(b, "%s %s\n", name, it)
		}
	}
	var esc, hs, st, res []string
	for _, k := range s.Esc {
		esc = append(esc, CanonKey(ps, k))
	}
	for _, p := range s.Store {
		st = append(st, CanonKey(ps, p.Src)+" -> "+CanonKey(ps, p.Dst))
	}
	for _, k := range s.HeapStore {
		hs = append(hs, CanonKey(ps, k))
	}
	for i, r := range s.Res {
		for _, rk := range r.From {
			ge := ""
			if rk.FieldGE {
				ge = "+"
			}
			res = append(res, fmt.Sprintf("%d: %s at %s%s", i, CanonKey(ps, rk.Key), CanonPath(ps, rk.Field), ge))
		}
		for _, p := range r.Heap {
			res = append(res, fmt.Sprintf("%d: heap at %s", i, CanonPath(ps, p)))
		}
		for _, p := range r.CountedHeap {
			res = append(res, fmt.Sprintf("%d: counted-heap at %s", i, CanonPath(ps, p)))
		}
	}
	var def []string
	for _, d := range s.Deferred {
		x := fmt.Sprintf("f%d@%d %s(", d.Origin, d.Site, CanonKey(ps, d.Fn))
		for i, a := range d.Args {
			if i > 0 {
				x += "; "
			}
			var ks []string
			for _, k := range a.Keys {
				ks = append(ks, CanonKey(ps, k))
			}
			sort.Strings(ks)
			x += strings.Join(ks, " ")
			if a.Heap {
				x += " heap"
			}
		}
		def = append(def, x+")")
	}
	section("deferred", def)
	section("esc", esc)
	section("store", st)
	section("heapstore", hs)
	section("res", res)
	fmt.Fprintf(b, "region builds=%v outer=%v\n", s.Region.Builds, s.Region.OuterResults)
}

// CanonPath renders a path as its steps.
func CanonPath(ps domain.Paths, p domain.PathID) string {
	var b strings.Builder
	b.WriteByte('.')
	for _, st := range ps.Steps(p) {
		switch st {
		case domain.StepLoad:
			b.WriteString("*")
		case domain.StepElem:
			b.WriteString("[]")
		default:
			fmt.Fprintf(&b, "f%d.", int32(st))
		}
	}
	return b.String()
}

// CanonKey renders a key as input, steps and flags.
func CanonKey(ps domain.Paths, k domain.Key) string {
	f := k.Flags()
	s := fmt.Sprintf("in%d%s", k.Input(), CanonPath(ps, k.Path()))
	if f.GE {
		s += "+"
	}
	if f.FieldsGE {
		s += "~"
	}
	if f.Deep {
		s += "*"
	}
	if f.Borrowed {
		s += "b"
	}
	if f.Counts {
		s += "c"
	}
	if f.DirectCount {
		s += "d"
	}
	return s
}
