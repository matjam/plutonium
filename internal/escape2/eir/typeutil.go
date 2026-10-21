package eir

import (
	"github.com/matjam/plutonium/internal/types2"
)

// hasPointers reports whether a value of type T may hold a pointer that
// the analysis follows. A type parameter may.
func hasPointers(T types2.Type) bool {
	return hasPointersSeen(T, nil)
}

func hasPointersSeen(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil {
		return false
	}
	switch u := types2.Unalias(T).(type) {
	case *types2.TypeParam:
		return true
	case *types2.Named:
		if seen[u] {
			return false
		}
		if seen == nil {
			seen = map[types2.Type]bool{}
		}
		seen[u] = true
	}
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		return u.Info()&types2.IsString != 0 || u.Kind() == types2.UnsafePointer || u.Kind() == types2.UntypedNil
	case *types2.Pointer, *types2.Slice, *types2.Map, *types2.Chan, *types2.Signature, *types2.Interface:
		return true
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapVolatile, types2.WrapAligned, types2.WrapRing:
			return hasPointersSeen(u.Elem(), seen)
		}
		return true
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if hasPointersSeen(u.Field(i).Type(), seen) {
				return true
			}
		}
		return false
	case *types2.Array:
		return u.Len() > 0 && hasPointersSeen(u.Elem(), seen)
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() && hasPointersSeen(v.Payload(), seen) {
				return true
			}
		}
		return false
	case *types2.Tuple:
		for i := 0; i < u.Len(); i++ {
			if hasPointersSeen(u.At(i).Type(), seen) {
				return true
			}
		}
		return false
	}
	return false
}

// unwrap removes volatile and aligned wrappers, which do not change how a
// value is used (internal/lower value.go has the same rule).
func unwrap(T types2.Type) types2.Type {
	for {
		w, ok := T.Underlying().(*types2.Wrapper)
		if !ok || (w.Kind() != types2.WrapVolatile && w.Kind() != types2.WrapAligned) {
			return T
		}
		T = w.Elem()
	}
}

// isTypeParam reports whether T is a type parameter.
func isTypeParam(T types2.Type) bool {
	_, ok := types2.Unalias(T).(*types2.TypeParam)
	return ok
}

// isInterface reports whether T's underlying type is an interface (a type
// parameter is not).
func isInterface(T types2.Type) bool {
	if T == nil || isTypeParam(T) {
		return false
	}
	_, ok := T.Underlying().(*types2.Interface)
	return ok
}

// pointerElem returns what a pointer-like T points to: *T, owned[T],
// shared[T], synced[T]; nil otherwise.
func pointerElem(T types2.Type) types2.Type {
	if T == nil {
		return nil
	}
	switch u := unwrap(T).Underlying().(type) {
	case *types2.Pointer:
		return u.Elem()
	case *types2.Wrapper:
		if u.IsPointer() {
			return u.Elem()
		}
	}
	return nil
}

// dropMethod returns T's own Drop method, or nil. Drop is never promoted
// from an embedded field: the field is dropped as a field (lower/drop.go
// has the same rule).
func dropMethod(T types2.Type) *types2.Func {
	if _, ok := types2.Unalias(T).(*types2.Named); !ok {
		return nil
	}
	sel := types2.NewMethodSet(types2.NewPointer(T)).Lookup(nil, "Drop")
	if sel == nil || len(sel.Index()) != 1 {
		return nil
	}
	fn, _ := sel.Obj().(*types2.Func)
	return fn
}

// needsDrop reports whether destroying a value of T runs drop glue
// (internal/lower drop.go needsDrop, the same rule): T is an owner with a
// Drop method, or with parts that need destroying (owners, or counted
// references to release). A type parameter that admits owners ([T owner
// any], section 18.5, Generic code) may: its drop is a call on the type
// parameter, which each instance resolves; any other cannot hold an owner.
func (b *builder) needsDrop(T types2.Type) bool {
	if tp, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return types2.IsOwnerTypeParam(tp)
	}
	if v, ok := b.dropNeeds.get(T); ok {
		return v
	}
	b.dropNeeds.set(T, false) // a recursive type needs a pointer, which needs nothing
	need := false
	if types2.IsOwner(T) {
		if A := types2.OwnerBase(T); A != nil {
			need = dropMethod(T) != nil || b.needsDrop(A)
		} else {
			switch u := unwrap(T).Underlying().(type) {
			case *types2.Struct:
				need = dropMethod(T) != nil
				for i := 0; i < u.NumFields() && !need; i++ {
					need = b.needsDrop(u.Field(i).Type()) || b.counted(u.Field(i).Type())
				}
			case *types2.Array:
				need = dropMethod(T) != nil || (u.Len() > 0 && (b.needsDrop(u.Elem()) || b.counted(u.Elem())))
			case *types2.Wrapper:
				need = true // owned[T]
			default:
				need = dropMethod(T) != nil || types2.IsInterface(T) || types2.UnionVariants(T) != nil
			}
		}
	}
	b.dropNeeds.set(T, need)
	return need
}

// counted reports whether a value of T holds counted references: strings,
// shared, synced and weak references, maps, channels, counted errors
// (internal/lower strcount.go counted, the same rule). Releasing one may run
// code of the program: the last release of a box drops its object.
func (b *builder) counted(T types2.Type) bool {
	if T == nil || types2.IsOwner(T) {
		return false
	}
	if isTypeParam(T) {
		return true
	}
	if v, ok := b.countedT.get(T); ok {
		return v
	}
	b.countedT.set(T, false)
	c := false
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		c = u.Info()&types2.IsString != 0
	case *types2.Struct:
		for i := 0; i < u.NumFields() && !c; i++ {
			c = b.counted(u.Field(i).Type())
		}
	case *types2.Array:
		c = u.Len() > 0 && b.counted(u.Elem())
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapAligned, types2.WrapVolatile, types2.WrapRing:
			c = b.counted(u.Elem())
		default:
			c = u.IsCounted()
		}
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants() && !c; i++ {
			c = u.Variant(i).HasPayload() && b.counted(u.Variant(i).Payload())
		}
	case *types2.Chan, *types2.Map:
		c = true
	case *types2.Interface:
		c = types2.IsCountedError(T)
	}
	b.countedT.set(T, c)
	return c
}

// variantName returns the name of the variant a SynthVariant makes: its
// index is Path[0].
func variantName(s *Synth) string {
	vs := types2.UnionVariants(s.Recv)
	if len(s.Path) == 1 && s.Path[0] < len(vs) {
		return vs[s.Path[0]].Name()
	}
	return "?"
}

// typeMap maps types, by identity, to values. The text of a type only picks
// the bucket; identity decides (two types with one text, from different
// scopes, stay apart).
type typeMap[V any] struct {
	m map[string][]typeEntry[V]
}

type typeEntry[V any] struct {
	T types2.Type
	v V
}

func (tm *typeMap[V]) get(T types2.Type) (V, bool) {
	for _, e := range tm.m[types2.TypeString(T, nil)] {
		if types2.Identical(e.T, T) {
			return e.v, true
		}
	}
	var zero V
	return zero, false
}

func (tm *typeMap[V]) set(T types2.Type, v V) {
	if tm.m == nil {
		tm.m = map[string][]typeEntry[V]{}
	}
	k := types2.TypeString(T, nil)
	for i, e := range tm.m[k] {
		if types2.Identical(e.T, T) {
			tm.m[k][i].v = v
			return
		}
	}
	tm.m[k] = append(tm.m[k], typeEntry[V]{T, v})
}

// coreType returns T's underlying type, or for a type parameter the
// underlying type of the single type its constraint allows, or nil.
func coreType(T types2.Type) types2.Type {
	T = unwrap(T)
	tp, ok := types2.Unalias(T).(*types2.TypeParam)
	if !ok {
		return T.Underlying()
	}
	iface, ok := tp.Constraint().Underlying().(*types2.Interface)
	if !ok {
		return nil
	}
	var core types2.Type
	for i := 0; i < iface.NumEmbeddeds(); i++ {
		e := iface.EmbeddedType(i)
		var u types2.Type
		switch t := e.(type) {
		case *types2.Union:
			if t.Len() != 1 {
				return nil
			}
			u = t.Term(0).Type().Underlying()
		default:
			if isTypeParam(t) {
				u = coreType(t)
			} else {
				u = t.Underlying()
			}
			if _, isIface := u.(*types2.Interface); isIface {
				continue
			}
		}
		if core != nil && !types2.Identical(core, u) {
			return nil
		}
		core = u
	}
	return core
}

// signatureOf returns the signature of a value of type T (its core type),
// or nil.
func signatureOf(T types2.Type) *types2.Signature {
	if T == nil {
		return nil
	}
	s, _ := coreType(T).(*types2.Signature)
	return s
}
