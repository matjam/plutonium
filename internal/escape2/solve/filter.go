package solve

import (
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/types2"
)

// Type filters (decision D51; rules.go, "Type filters"): what a type
// assertion or a dispatch through an interface lets through, applied to the
// tokens of an interface value's data word.

// FilterAdmitsObj reports whether filter f admits, as the data word of an
// interface value, a pointer to object o at field path steps (ge: or
// anywhere under it): some atom of f admits it (atomAdmitsObj).
func FilterAdmitsObj(o *eir.Object, steps []domain.Step, ge bool, f domain.Filter) bool {
	for _, a := range f.Atoms {
		if atomAdmitsObj(o, steps, ge, a) {
			return true
		}
	}
	return len(f.Atoms) == 0
}

// atomAdmitsObj reports whether atom a admits a pointer to object o at
// field path steps (ge: or anywhere under it) as a data word. Only an
// exact pointer to the root of a typed object (a variable, a parameter's
// slot, a temporary, a box, the array of a slice literal or of variadic
// arguments), or into a box, whose type mentions no type parameter is ever
// rejected: anything else is admitted, as nothing is known of it. (Code
// that reinterprets an object's memory with unsafe.Pointer may make a *V of
// a pointer inside it, review2/m22; a box's memory is only its value's.)
// The pointer points at a value of type U, the type at steps of the
// object's type (subType):
//   - "is *V" admits it only for V of U's underlying type (a *U converts to
//     a *V then; a pointer into a box is one to a V there), and "pointer to
//     an instance of G" only for U whose underlying type may be that of an
//     instance of G (mayInstance);
//   - "is T", for T a type that is not direct, admits it only for U that
//     is or holds a T (contains): the data word of a T points at a T,
//     which may be a field of the value, as pluto/fmt's walk makes one of
//     a field's address (its contract calls the methods on what holds it,
//     eir/fmtwalk.go). A box's value has its own type, and its fields
//     theirs; a variable may be viewed as a T of its underlying type (a
//     conversion), so for one a value of T's underlying type is enough;
//   - "is T" for a direct T admits it (a direct value of a type that is
//     not a pointer may point at any object);
//   - "implements I" admits the root of a box only of a type that
//     implements I, and any other pointer (a direct value may point at any
//     object).
func atomAdmitsObj(o *eir.Object, steps []domain.Step, ge bool, a domain.Atom) bool {
	if ge || o.Type == nil || mentionsTypeParam(o.Type, nil) {
		return true
	}
	switch o.Kind {
	case eir.ObjVar, eir.ObjParam, eir.ObjTemp, eir.ObjBox, eir.ObjSliceLit, eir.ObjVarargs:
	default:
		return true
	}
	box := o.Kind == eir.ObjBox
	if len(steps) > 0 && !box {
		return true
	}
	U, where := subType(o.Type, steps)
	if where == subUnknown {
		return true
	}
	if where == subNone {
		// A place in a box that is no value of its own (inside a string,
		// a slice header): only unsafe code makes a pointer to it. A key's
		// field step applies to the pointers of a value as well as to the
		// value itself, so such a token is what a caller finds where a
		// callee's key names a field of an interface value's data word.
		switch a.Kind {
		case domain.AtomImplements:
			return true
		case domain.AtomIs:
			if p, ok := types2.Unalias(a.T).(*types2.Pointer); ok {
				return mentionsTypeParam(p.Elem(), nil)
			}
			return mentionsTypeParam(a.T, nil) || directValue(a.T)
		}
		return false
	}
	switch a.Kind {
	case domain.AtomImplements:
		return !box || len(steps) > 0 || a.Admits(o.Type)
	case domain.AtomPtrTo:
		n, ok := types2.Unalias(a.T).(*types2.Named)
		return !ok || domain.SameOrigin(U, a.T) || mayInstance(U.Underlying(), n.Origin().Underlying(), 0)
	case domain.AtomIs:
		if p, ok := types2.Unalias(a.T).(*types2.Pointer); ok {
			return mentionsTypeParam(p.Elem(), nil) || sameShape(p.Elem(), U)
		}
		if mentionsTypeParam(a.T, nil) || directValue(a.T) {
			return true
		}
		return contains(U, a.T, box, 0)
	}
	return true
}

// Where a field path of a value leads (subType).
const (
	subValue   = iota // a value of the type found
	subUnknown        // somewhere this package does not know the layout of
	subNone           // nowhere a value of its own is: inside a string, a
	// slice header, a pointer, past an array's elements
)

// subType is the type of the value at field path steps of a value of type
// T, and whether there is one (subValue).
func subType(T types2.Type, steps []domain.Step) (types2.Type, int) {
	for _, st := range steps {
		switch u := T.Underlying().(type) {
		case *types2.Struct:
			if st < 0 || int(st) >= u.NumFields() {
				return nil, subNone
			}
			T = u.Field(int(st)).Type()
		case *types2.Array:
			if st != domain.StepElem {
				return nil, subNone
			}
			T = u.Elem()
		case *types2.Basic, *types2.Pointer, *types2.Slice, *types2.Map, *types2.Chan, *types2.Signature, *types2.Interface:
			return nil, subNone
		default:
			return nil, subUnknown
		}
		if mentionsTypeParam(T, nil) {
			return nil, subUnknown
		}
	}
	return T, subValue
}

// sameShape reports whether a pointer to a U converts to a pointer to a T:
// their underlying types are identical, struct tags aside.
func sameShape(U, T types2.Type) bool {
	return types2.IdenticalIgnoreTags(U.Underlying(), T.Underlying())
}

// mayInstance reports whether type x may be pattern pat with its type
// parameters replaced: identical where pat mentions none, of pat's form
// where it does; a named type or another form that mentions a type
// parameter may be anything.
func mayInstance(x, pat types2.Type, depth int) bool {
	if !mentionsTypeParam(pat, nil) {
		return types2.IdenticalIgnoreTags(x, pat)
	}
	if depth > 16 {
		return true
	}
	x = types2.Unalias(x)
	switch p := types2.Unalias(pat).(type) {
	case *types2.Pointer:
		u, ok := x.(*types2.Pointer)
		return ok && mayInstance(u.Elem(), p.Elem(), depth+1)
	case *types2.Slice:
		u, ok := x.(*types2.Slice)
		return ok && mayInstance(u.Elem(), p.Elem(), depth+1)
	case *types2.Array:
		u, ok := x.(*types2.Array)
		return ok && u.Len() == p.Len() && mayInstance(u.Elem(), p.Elem(), depth+1)
	case *types2.Map:
		u, ok := x.(*types2.Map)
		return ok && mayInstance(u.Key(), p.Key(), depth+1) && mayInstance(u.Elem(), p.Elem(), depth+1)
	case *types2.Struct:
		u, ok := x.(*types2.Struct)
		if !ok || u.NumFields() != p.NumFields() {
			return false
		}
		for i := 0; i < p.NumFields(); i++ {
			f, g := u.Field(i), p.Field(i)
			if f.Name() != g.Name() || f.Embedded() != g.Embedded() || !mayInstance(f.Type(), g.Type(), depth+1) {
				return false
			}
		}
		return true
	}
	return true
}

// contains reports whether a value of type U is, or holds in place (a field,
// an element, at any depth), a value of type T, or with exact false of T's
// underlying type (sameShape). A type whose layout is not known here (a
// Plutonium wrapper or packed type, a type parameter's) may hold anything.
func contains(U, T types2.Type, exact bool, depth int) bool {
	if types2.Identical(U, T) || !exact && sameShape(U, T) {
		return true
	}
	if depth > 32 {
		return true
	}
	switch u := U.Underlying().(type) {
	case *types2.Basic, *types2.Pointer, *types2.Slice, *types2.Map, *types2.Chan, *types2.Signature, *types2.Interface:
		return false
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if contains(u.Field(i).Type(), T, exact, depth+1) {
				return true
			}
		}
		return false
	case *types2.Array:
		return u.Len() > 0 && contains(u.Elem(), T, exact, depth+1)
	}
	return true
}

// AssertFilter is the filter an assertion to type T makes (OpAssert): "is
// T" for a concrete type other than a pointer, "implements T" for an
// interface, nothing for a pointer type or a type that mentions a type
// parameter.
func AssertFilter(T types2.Type) (domain.Filter, bool) {
	if T == nil || mentionsTypeParam(T, nil) {
		return domain.Filter{}, false
	}
	if _, ok := T.Underlying().(*types2.Interface); ok {
		return domain.One(domain.AtomImplements, T), true
	}
	if _, ok := T.Underlying().(*types2.Pointer); ok {
		// An assertion to a pointer type makes none: a type switch over
		// pointer types (the compiler's own, case after case) would give
		// each case's value a filtered form of every input it reaches,
		// which cost the translated compiler half again its solve steps
		// and none of its findings (D51).
		return domain.Filter{}, false
	}
	return domain.One(domain.AtomIs, T), true
}

// RecvFilter is the filter of the receiver (input 0) of candidate cand of a
// dynamic call (iface: a call through an interface, whose receiver is the
// data word): an interface adapter (eir.SynthItabAdapter) is reached only
// by a data word of its dynamic type, "is Recv"; a method of pointer
// receiver *T only by a *T, "is *T" (its receiver is a pointer however it
// is called), or for a generic T a pointer to an instance of it; a method of
// a value receiver of a direct type T, "is T". Through an interface, a
// method of a value receiver of a type T that is not direct (a candidate
// standing for its adapter, cands.allNamed) is given the data word, which
// points at a T: "is T" too. Other candidates have none: a value receiver
// given the value itself (a call on a type parameter) is a struct value,
// whose pointers say nothing of its type.
func RecvFilter(p *eir.Program, cand eir.FuncID, iface bool) (domain.Filter, bool) {
	if cand == eir.NoFunc || int(cand) >= len(p.Funcs) {
		return domain.Filter{}, false
	}
	fn := p.Funcs[cand]
	if sy := fn.Key.Synth; sy != nil {
		if sy.Kind != eir.SynthItabAdapter || sy.Recv == nil || mentionsTypeParam(sy.Recv, nil) {
			return domain.Filter{}, false
		}
		return domain.One(domain.AtomIs, sy.Recv), true
	}
	if fn.Key.Obj == nil || len(fn.Inputs) == 0 {
		return domain.Filter{}, false
	}
	sig, ok := fn.Key.Obj.Type().(*types2.Signature)
	if !ok {
		return domain.Filter{}, false
	}
	r := sig.Recv()
	if r == nil || fn.Inputs[0].Var != r {
		return domain.Filter{}, false
	}
	pt, ok := types2.Unalias(r.Type()).(*types2.Pointer)
	if !ok {
		// A value receiver of a direct type (a pointer-shaped struct, a
		// scalar): the receiver is the data word itself, however the
		// method is called.
		if T := r.Type(); !mentionsTypeParam(T, nil) && directValue(T) && !types2.IsInterface(T) {
			return domain.One(domain.AtomIs, T), true
		}
		if T := r.Type(); iface && !mentionsTypeParam(T, nil) && !types2.IsInterface(T) {
			return domain.One(domain.AtomIs, T), true
		}
		return domain.Filter{}, false
	}
	if !mentionsTypeParam(pt, nil) {
		return domain.One(domain.AtomIs, pt), true
	}
	// A method of a generic type, one body for every instance: a pointer
	// to some instance of it.
	if n, ok := types2.Unalias(pt.Elem()).(*types2.Named); ok && n.Origin() != nil {
		return domain.One(domain.AtomPtrTo, n.Origin()), true
	}
	return domain.Filter{}, false
}

// filterOf returns filter id of the table, memoized in the solve (the table
// is shared by parallel solves and locks).
func (s *solver) filterOf(id domain.FilterID) (domain.Filter, bool) {
	if f, ok := s.filters[id]; ok {
		return f, true
	}
	f, ok := s.ps.Filters().Get(id)
	if ok {
		if s.filters == nil {
			s.filters = map[domain.FilterID]domain.Filter{}
		}
		s.filters[id] = f
	}
	return f, ok
}

type recvKey struct {
	p     *eir.Program
	cand  eir.FuncID
	iface bool
}

type assertKey struct{ T types2.Type }

// RecvFilterID is RecvFilter interned in table fs (0: none), memoized
// there: a join applies it to its members each time it is made.
func RecvFilterID(fs *domain.Filters, p *eir.Program, cand eir.FuncID, iface bool) domain.FilterID {
	return fs.Memo(recvKey{p, cand, iface}, func() domain.FilterID {
		if f, ok := RecvFilter(p, cand, iface); ok {
			return fs.Intern(f)
		}
		return 0
	})
}

// AssertFilterID is AssertFilter interned in table fs (0: none), memoized
// there.
func AssertFilterID(fs *domain.Filters, T types2.Type) domain.FilterID {
	if T == nil {
		return 0
	}
	return fs.Memo(assertKey{T}, func() domain.FilterID {
		if f, ok := AssertFilter(T); ok {
			return fs.Intern(f)
		}
		return 0
	})
}
