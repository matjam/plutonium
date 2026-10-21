package solve

import (
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
)

// Tokens and tagged tokens (rules.go, "Abstract values"). A tagged token is
// interned in the solver's token table; sets hold token numbers.
//
// Word 0: kind (bits 0-1), the token's own GE (pg or kg, bit 2), the tag's
// GE (bit 3), base (bits 8-39: object or input). Word 1: path (bits 0-31),
// tag (bits 32-63).

type tokKind uint8

const (
	tObj   tokKind = iota // Obj(base, path, ge)
	tIn                   // In(base, path, ge)
	tHeap                 // Heap
	tCHeap                // a counted heap reference
)

type tok struct {
	kind  tokKind
	ge    bool // pg for Obj, kg for In
	tagGE bool
	base  uint32
	path  domain.PathID
	tag   domain.PathID
}

func (t tok) words() (uint64, uint64) {
	w0 := uint64(t.kind) | uint64(t.base)<<8
	if t.ge {
		w0 |= 4
	}
	if t.tagGE {
		w0 |= 8
	}
	return w0, uint64(t.path) | uint64(t.tag)<<32
}

func tokOf(w0, w1 uint64) tok {
	return tok{
		kind:  tokKind(w0 & 3),
		ge:    w0&4 != 0,
		tagGE: w0&8 != 0,
		base:  uint32(w0 >> 8),
		path:  domain.PathID(uint32(w1)),
		tag:   domain.PathID(uint32(w1 >> 32)),
	}
}

// heapTok is the one heap token; tags do not matter for heap pointers.
var heapTok = tok{kind: tHeap}

// intern returns the number of a tagged token.
func (s *solver) intern(t tok) uint64 {
	if t.kind == tHeap || t.kind == tCHeap {
		t.base, t.path, t.ge = 0, domain.Root, false // heap tokens keep their tag only
	}
	w0, w1 := t.words()
	if id, ok := s.tokIdx.get(w0, w1); ok {
		return id
	}
	id := uint64(len(s.toks.s) / 2)
	s.toks.push(s.a, w0)
	s.toks.push(s.a, w1)
	s.tokIdx.put(s.a, w0, w1, id)
	return id
}

func (s *solver) tokAt(id uint64) tok {
	return tokOf(s.toks.s[2*id], s.toks.s[2*id+1])
}

// key is the summary key of an In token.
func key(t tok) domain.Key {
	return domain.MakeKey(int(t.base), t.path, domain.KeyFlags{GE: t.ge})
}

// extend appends one step to p; ge reports that the step went past the
// limits, and then the result is p itself (rules.go, "Paths past the
// limits"): with GE it covers p+st, and it is narrower than the domain's cut
// back to an earlier prefix, which could cover the pointer p was found
// through (vec.Collect: the vector itself, not only what it holds).
func (s *solver) extend(p domain.PathID, st domain.Step) (domain.PathID, bool) {
	q, cut := s.ps.Extend(p, st)
	if cut {
		return p, true
	}
	return q, false
}

// extendAll appends steps to p, stopping at the first past the limits.
func (s *solver) extendAll(p domain.PathID, steps []domain.Step) (domain.PathID, bool) {
	for _, st := range steps {
		q, cut := s.extend(p, st)
		if cut {
			return q, true
		}
		p = q
	}
	return p, false
}

// suffix returns the steps of p after prefix (prefix must be a prefix of p).
func (s *solver) suffix(p, prefix domain.PathID) []domain.Step {
	return s.ps.Steps(p)[len(s.ps.Steps(prefix)):]
}

// pathOf returns the path of steps.
func (s *solver) pathOf(steps []domain.Step) (domain.PathID, bool) {
	return s.extendAll(domain.Root, steps)
}

// eirSteps converts an EIR field path to domain steps (eir.ElemStep is
// domain.StepElem).
func eirSteps(path []int, buf []domain.Step) []domain.Step {
	buf = buf[:0]
	for _, x := range path {
		if x == eir.ElemStep {
			buf = append(buf, domain.StepElem)
		} else {
			buf = append(buf, domain.Step(x))
		}
	}
	return buf
}

// off offsets pointer t by steps (rules.go, off).
func (s *solver) off(t tok, steps []domain.Step) tok {
	switch t.kind {
	case tObj, tIn:
		if t.ge || len(steps) == 0 {
			return t
		}
		p, cut := s.extendAll(t.path, steps)
		t.path = p
		t.ge = t.ge || cut
	}
	return t
}

// anyOffset is pointer t offset by an unknown amount within its object
// (eir.AnyStep): Obj(o, Root, true); for an In token, the path back to its
// last load (the start of the object it points into), GE; Heap stays.
func (s *solver) anyOffset(t tok) tok {
	t = asPointer(t)
	switch t.kind {
	case tObj:
		t.path, t.ge = domain.Root, true
	case tIn:
		steps := s.ps.Steps(t.path)
		i := len(steps)
		for i > 0 && steps[i-1] != domain.StepLoad {
			i--
		}
		t.path, _ = s.pathOf(steps[:i])
		t.ge = true
	}
	return t
}

// sel selects field steps Q of the value holding t (rules.go, sel). ok is
// false when t is in another field.
func (s *solver) sel(t tok, q domain.PathID) (tok, bool) {
	if q == domain.Root {
		return t, true
	}
	switch {
	case s.ps.HasPrefix(t.tag, q):
		t.tag, _ = s.pathOf(s.suffix(t.tag, q))
		return t, true
	case t.tagGE && s.ps.HasPrefix(q, t.tag):
		t.tag = domain.Root
		return t, true
	case t.kind == tIn && t.tag == domain.Root && !t.tagGE:
		return s.off(t, s.ps.Steps(q)), true
	case (t.kind == tHeap || t.kind == tCHeap) && t.tag == domain.Root && !t.tagGE:
		return t, true // a whole value of heap pointers
	}
	return t, false
}

// pre places t at field Q of a struct value (rules.go, pre).
func (s *solver) pre(t tok, q domain.PathID, ge bool) tok {
	if q != domain.Root {
		p, cut := s.extendAll(q, s.ps.Steps(t.tag))
		t.tag = p
		t.tagGE = t.tagGE || cut
	}
	t.tagGE = t.tagGE || ge
	return t
}

// geify makes a token's tag unknown and an In token cover its extensions
// (rules.go, reach).
func geify(t tok) tok {
	t.tag, t.tagGE = domain.Root, true
	if t.kind == tIn {
		t.ge = true
	}
	return t
}

// asPointer is a token as a pointer value: its own tag dropped.
func asPointer(t tok) tok {
	t.tag, t.tagGE = domain.Root, false
	return t
}
