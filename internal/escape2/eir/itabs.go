package eir

import (
	"github.com/matjam/plutonium/internal/types2"
)

// Itabs found at run time (internal/lower iface.go typeTable.finish, the
// same rule). An interface call reaches the adapter an itab holds, which
// recovers the receiver from the data word. Converting a value to an
// interface makes the itab of that pair, but asserting an interface value
// to another interface (a type switch case, x.(I), an interface
// conversion) finds its itab at run time, for any type with a descriptor:
// every type converted, and the elements, fields, pointees and pointer
// types they describe, which pluto/fmt's walk reaches. So every such type
// that implements an interface something is asserted to gets the adapters
// of that pair, as the lowerer emits them.

// itabState is the builder's record of descriptors and asserted interfaces.
type itabState struct {
	described typeMap[bool]
	concrete  []types2.Type // the described types that are not interfaces
	asserted  typeMap[bool]
	ifaces    []types2.Type // the asserted interfaces with methods
	// The pairs made: of the first doneI interfaces and doneC types.
	doneC, doneI int
}

// noteAsserted records that a value is asserted to T, an interface that
// then needs the itabs of every described type implementing it.
func (b *builder) noteAsserted(T types2.Type) {
	if T == nil || !isInterface(T) || containsTypeParam(T) {
		return
	}
	iface := T.Underlying().(*types2.Interface)
	if iface.Empty() {
		return
	}
	if _, ok := b.itabs.asserted.get(T); ok {
		return
	}
	b.itabs.asserted.set(T, true)
	b.itabs.ifaces = append(b.itabs.ifaces, T)
}

// describe records that T has a type descriptor, and so do the types it
// describes (internal/lower iface.go descOf and describe).
func (b *builder) describe(T types2.Type) {
	if T == nil || containsTypeParam(T) {
		return
	}
	T = unwrap(T)
	if _, ok := b.itabs.described.get(T); ok {
		return
	}
	b.itabs.described.set(T, true)
	if !isInterface(T) {
		b.itabs.concrete = append(b.itabs.concrete, T)
	}
	switch u := T.Underlying().(type) {
	case *types2.Array:
		b.describe(u.Elem())
	case *types2.Slice:
		b.describe(u.Elem())
	case *types2.Pointer:
		b.describe(u.Elem())
	case *types2.Wrapper:
		if u.IsPointer() || u.Kind() == types2.WrapRing {
			b.describe(u.Elem())
		}
	case *types2.Map:
		b.describe(u.Key())
		b.describe(u.Elem())
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			b.describe(u.Field(i).Type())
		}
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants(); i++ {
			if v := u.Variant(i); v.HasPayload() {
				b.describe(v.Payload())
			}
		}
	}
	// pluto/fmt reaches the methods of *T through the descriptor's pointer
	// type when it has the address of a T.
	if n, ok := types2.Unalias(T).(*types2.Named); ok && !isInterface(T) && pointerElem(T) == nil {
		P := types2.NewPointer(T)
		if types2.NewMethodSet(P).Len() > types2.NewMethodSet(n).Len() {
			b.describe(P)
		}
	}
}

// finishItabs synthesizes the adapters of every pair of a described type
// and an asserted interface it implements not yet made, and reports
// whether it made any (whose bodies, queued, may describe more types).
func (b *builder) finishItabs() bool {
	before := len(b.queue)
	st := &b.itabs
	nc, ni := len(st.concrete), len(st.ifaces)
	for i := 0; i < ni; i++ {
		// New interfaces against every type; old interfaces against the
		// new types.
		from := 0
		if i < st.doneI {
			from = st.doneC
		}
		I := st.ifaces[i]
		iface := I.Underlying().(*types2.Interface)
		for _, T := range st.concrete[from:nc] {
			if !types2.Implements(T, iface) {
				continue
			}
			for k := 0; k < iface.NumMethods(); k++ {
				b.synth(Synth{Kind: SynthItabAdapter, Recv: T, Method: iface.Method(k)})
			}
		}
	}
	st.doneC, st.doneI = nc, ni
	return len(b.queue) > before
}
