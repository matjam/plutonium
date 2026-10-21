package refsolve

import "github.com/matjam/plutonium/internal/escape2/domain"

// The abstract values of solve/rules.go ("Abstract values"), as plain Go
// maps. Nothing here is interned or shared.

type tokKind uint8

const (
	kObj   tokKind = iota + 1 // Obj(o, p, pg)
	kIn                       // In(i, P, kg)
	kHeap                     // Heap
	kCHeap                    // CHeap
)

// tok is a token. id is the object (kObj) or the input (kIn); path and ge are
// p and pg, or P and kg.
type tok struct {
	kind tokKind
	id   int
	path domain.PathID
	ge   bool
}

// tv is a tagged token <t, T, g>.
type tv struct {
	t   tok
	tag domain.PathID
	g   bool
}

// value is a set of tagged tokens: what a register or a place holds.
type value map[tv]bool

// add puts x in v and reports whether it was new.
func (v value) add(x tv) bool {
	if v[x] {
		return false
	}
	v[x] = true
	return true
}

// addAll puts every element of w in v and reports whether any was new.
func (v value) addAll(w value) bool {
	changed := false
	for x := range w {
		if v.add(x) {
			changed = true
		}
	}
	return changed
}

// ptr is the pointer value <t, Root, false>.
func ptr(t tok) tv { return tv{t: t, tag: domain.Root} }

func single(x tv) value { return value{x: true} }

// place is <o, p, pg>: a place in frame object o.
type place struct {
	obj  int
	path domain.PathID
	ge   bool
}
