package refsolve

import (
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
)

// Path helpers. Every path is built with domain.Paths.Extend, the one place
// that knows the limits; a cut is reported to the caller, which sets the GE
// bit (rules.go, "off", "pre").

// extend is a followed by step st; past the limits it is a itself, with the
// GE bit to set (rules.go, "Paths past the limits").
func (s *solver) extend(a domain.PathID, st domain.Step) (domain.PathID, bool) {
	q, c := s.ps.Extend(a, st)
	if c {
		// The prefix of a through its last load (a itself without one).
		steps := s.ps.Steps(a)
		i := len(steps)
		for i > 0 && steps[i-1] != domain.StepLoad {
			i--
		}
		if i == 0 || i == len(steps) {
			return a, true
		}
		p := domain.Root
		for _, st := range steps[:i] {
			p, _ = s.ps.Extend(p, st)
		}
		return p, true
	}
	return q, false
}

// concat is a followed by b, stopping at the first step past the limits.
func (s *solver) concat(a, b domain.PathID) (domain.PathID, bool) {
	for _, st := range s.ps.Steps(b) {
		var c bool
		a, c = s.extend(a, st)
		if c {
			return a, true
		}
	}
	return a, false
}

// pathOf is the path of the instruction steps (field indices, ElemStep).
func (s *solver) pathOf(steps []int) (domain.PathID, bool) {
	p, cut := domain.Root, false
	for _, st := range steps {
		step := domain.Step(st)
		if st == eir.ElemStep {
			step = domain.StepElem
		}
		var c bool
		p, c = s.extend(p, step)
		if c {
			return p, true
		}
	}
	return p, cut
}

// strip is t with its prefix q removed. q must be a prefix of t.
func (s *solver) strip(t, q domain.PathID) domain.PathID {
	steps := s.ps.Steps(t)[len(s.ps.Steps(q)):]
	p := domain.Root
	for _, st := range steps {
		p, _ = s.ps.Extend(p, st)
	}
	return p
}

// off(t, Q): pointer t offset by field path Q (rules.go, "Operations on
// values"). qcut says Q itself was cut when it was built.
func (s *solver) off(t tok, q domain.PathID, qcut bool) tok {
	switch t.kind {
	case kObj, kIn:
		if t.ge || t.fge || t.deep {
			return t // anywhere under its path already covers the offset
		}
		if t.kind == kObj {
			p, c := s.concat(t.path, q)
			return tok{kind: kObj, id: t.id, path: p, ge: c || qcut}
		}
		p, c := s.concat(t.path, q)
		return tok{kind: kIn, id: t.id, path: p, ge: c || qcut, lent: t.lent, cnt: t.cnt} // a field of a view is one, of a copy one
	}
	return t // Heap -> Heap; a counted reference stays one
}

// sel(v, Q): field Q of the struct value v. The cases are tried in the order
// of the table, the first that applies being the one used.
func (s *solver) sel(v value, q domain.PathID, qcut bool) value {
	out := value{}
	for x := range v {
		switch {
		case s.ps.HasPrefix(x.tag, q):
			out.add(tv{x.t, s.strip(x.tag, q), x.g || qcut})
		case x.g && s.ps.HasPrefix(q, x.tag):
			out.add(tv{x.t, domain.Root, true})
		case x.t.kind == kIn && x.tag == domain.Root && !x.g:
			out.add(ptr(s.off(x.t, q, qcut)))
		case (x.t.kind == kHeap || x.t.kind == kCHeap) && x.tag == domain.Root && !x.g:
			out.add(ptr(x.t)) // a whole value of heap pointers
		}
	}
	return out
}

// pre(v, Q): v placed at field Q of a struct value.
func (s *solver) pre(v value, q domain.PathID, qcut bool) value {
	out := value{}
	for x := range v {
		p, c := s.concat(q, x.tag)
		out.add(tv{x.t, p, x.g || c || qcut})
	}
	return out
}

// step(v, st): one step of a key path applied to v.
func (s *solver) step(v value, st domain.Step) value {
	if st == domain.StepLoad {
		return s.load(v)
	}
	q, qc := s.extend(domain.Root, st)
	out := s.sel(v, q, qc)
	for x := range v {
		if x.t.kind != kObj {
			continue // In tokens are covered by sel, heap by both
		}
		switch {
		case x.g:
			out.add(tv{s.off(x.t, q, qc), domain.Root, true})
		case x.tag == domain.Root:
			out.add(ptr(s.off(x.t, q, qc)))
		}
	}
	return out
}

// load(v): what the pointers of v point at.
func (s *solver) load(v value) value {
	out := value{}
	for x := range v {
		switch x.t.kind {
		case kObj:
			if s.objs[x.t.id].kind == eir.ObjBoxView {
				out.add(ptr(tok{kind: kHeap})) // heap memory for loads
				continue
			}
			s.loadObj(out, x.t)
		case kIn:
			if x.t.ge || x.t.deep {
				out.add(ptr(x.t)) // kg covers every extension, as Deep does (lent or not)
				continue
			}
			if x.t.fge {
				// The path's pointers at any offset, loaded through: Deep.
				out.add(ptr(tok{kind: kIn, id: x.t.id, path: x.t.path, deep: true}))
				continue
			}
			// Past the limits: Deep on the path through its last load
			// (rules.go, "Paths past the limits").
			p, c := s.extend(x.t.path, domain.StepLoad)
			out.add(ptr(tok{kind: kIn, id: x.t.id, path: p, deep: c}))
		case kCHeap:
			out.add(ptr(tok{kind: kCHeap})) // what a counted reference's memory holds (solve/rules.go, OpLoad)
		default:
			out.add(ptr(tok{kind: kHeap}))
		}
	}
	return out
}

// loadObj is the Obj(o, p, pg) case of load: every place q = <o, r, rg> of o
// that is the same memory.
func (s *solver) loadObj(out value, t tok) {
	for q, pt := range s.places {
		if q.obj != t.id {
			continue
		}
		if s.ps.HasPrefix(q.path, t.path) { // r = p+R
			r := s.strip(q.path, t.path)
			for x := range s.pre(pt, r, false) {
				x.g = x.g || q.ge || t.ge
				if t.ge {
					x.tag = domain.Root // with pg, the tag is <_, Root, true> (rules.go, load)
				}
				out.add(x)
			}
		}
		if s.ps.HasPrefix(t.path, q.path) && !s.ps.HasPrefix(q.path, t.path) { // p = r+R, R not empty (solve's linkSub: r = p is the first case)
			r := s.strip(t.path, q.path)
			for x := range s.sel(pt, r, false) {
				x.g = x.g || t.ge
				if t.ge {
					x.tag = domain.Root
				}
				out.add(x)
			}
		}
	}
}

// normal makes a tagged token of a reach closure: tag Root with g, and the
// token itself covering every extension.
func normal(x tv) tv {
	x.tag, x.g = domain.Root, true
	if x.t.kind == kIn && !x.t.deep { // (Deep is closed under reach already)
		x.t.ge, x.t.fge = true, false
	}
	return x
}

// reach(v): the closure of v under load and every field step, every tag made
// <_, Root, true> and every In token made kg. Closing under every field step
// of a token is the same as making it cover its extensions (Obj tokens get pg
// for that, as In tokens get kg).
func (s *solver) reach(v value) value {
	out := value{}
	var work []tv
	push := func(x tv) {
		x = normal(x)
		if out.add(x) {
			work = append(work, x)
		}
	}
	for x := range v {
		push(x)
	}
	for len(work) > 0 {
		x := work[len(work)-1]
		work = work[:len(work)-1]
		for y := range s.load(single(x)) {
			push(y)
		}
	}
	return out
}

// valueOfKey is val(k, β): the value key k names at a call whose input i is
// bound to bind(i): for a GE key reach of its path's value, for a FieldsGE
// key its path's value at any offset, for a Deep key reach of what a load at
// any offset of its path's value reads.
func (s *solver) valueOfKey(k domain.Key, bind func(int) value) value {
	v := bind(k.Input())
	if !k.Borrowed() && !k.Counts() && s.bindLent != nil && s.bindLent(k.Input()) {
		// A counted error lent with OpLend: its holder's view is for
		// Borrowed keys alone (solve/rules.go, OpLend).
		w := value{}
		for x := range v {
			if x.t.kind == kObj && x.t.id < len(s.f.Objects) && s.f.Objects[x.t.id].ErrHolder {
				continue
			}
			w.add(x)
		}
		v = w
	}

	for _, st := range s.ps.Steps(k.Path()) {
		v = s.step(v, st)
	}
	switch {
	case k.GE():
		v = s.reach(v)
	case k.FieldsGE():
		v = s.anyOffset(v)
	case k.Deep():
		v = s.reach(s.load(s.anyOffset(v)))
	}
	if k.Borrowed() {
		w := value{}
		for x := range v {
			if x.t.kind == kIn {
				x.t.lent, x.t.cnt = true, false
			}
			w.add(x)
		}
		v = w
	}
	if k.Counts() {
		w := value{}
		for x := range v {
			if x.t.kind == kObj && x.t.id < len(s.f.Objects) && s.f.Objects[x.t.id].ErrHolder {
				continue
			}
			if x.t.kind == kIn {
				x.t.lent, x.t.cnt = false, true
			}
			w.add(x)
		}
		v = w
	}
	return v
}

// valueOfDst is val(k, β) for a store's destination key: a FieldsGE one is
// its path's value, the store landing anywhere under it (anywhere).
func (s *solver) valueOfDst(k domain.Key, bind func(int) value) value {
	if k.FieldsGE() {
		k = k.WithPath(k.Path(), false)
	}
	return s.valueOfKey(k, bind)
}

// anyOffset is v's pointers offset anywhere in their objects (eir.AnyStep,
// rules.go OpField): Obj(o, Root, true); an In token at the path back to its
// last load (the start of the object it points into), FieldsGE (GE for a GE
// token, whose path may end inside a field of what it stands for); a Deep
// token stays; Heap stays.
func (s *solver) anyOffset(v value) value {
	out := value{}
	for x := range v {
		t := x.t
		switch t.kind {
		case kObj:
			t.path, t.ge = domain.Root, true
		case kIn:
			if t.deep {
				break
			}
			steps := s.ps.Steps(t.path)
			i := len(steps)
			for i > 0 && steps[i-1] != domain.StepLoad {
				i--
			}
			p := domain.Root
			for _, st := range steps[:i] {
				p, _ = s.ps.Extend(p, st)
			}
			t.path = p
			if !t.ge {
				t.fge = true
			}
		}
		out.add(ptr(t))
	}
	return out
}
