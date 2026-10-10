package lower

import (
	"fmt"

	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/types2"
)

// maxRegLeaves is the most scalar leaves an aggregate parameter may have and
// still be passed in registers; larger aggregates are passed by reference to a
// copy the caller makes. Results with more leaves than this in total are
// returned through a hidden pointer.
const maxRegLeaves = 4

// A leaf is one scalar component of a value: its offset in memory and type.
type leaf struct {
	off int64
	t   *ssa.Type
}

func (l *Lowerer) sizeof(T types2.Type) int64  { return l.sizes.Sizeof(T) }
func (l *Lowerer) alignof(T types2.Type) int64 { return l.sizes.Alignof(T) }

// scalarType returns the SSA type a value of T is held in, or nil if T is
// an aggregate kept in memory.
func (l *Lowerer) scalarType(T types2.Type) *ssa.Type {
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		switch {
		case u.Kind() == types2.Bool || u.Kind() == types2.UntypedBool:
			return ssa.TBool
		case u.Info()&types2.IsInteger != 0:
			if u.Kind() == types2.UntypedInt || u.Kind() == types2.UntypedRune {
				return ssa.TI64
			}
			return ssa.IntType(l.sizeof(u))
		case u.Info()&types2.IsFloat != 0:
			if u.Kind() == types2.UntypedFloat {
				return ssa.TF64
			}
			return ssa.FloatType(l.sizeof(u))
		case u.Kind() == types2.UnsafePointer || u.Kind() == types2.UntypedNil:
			return ssa.TPtr
		}
		return nil // strings
	case *types2.Pointer, *types2.Signature:
		return ssa.TPtr
	case *types2.Wrapper:
		switch {
		case u.IsPointer():
			return ssa.TPtr
		case u.Kind() == types2.WrapVolatile:
			return l.scalarType(u.Elem())
		case u.Kind() == types2.WrapAligned:
			if l.sizeof(u) == l.sizeof(u.Elem()) {
				return l.scalarType(u.Elem())
			}
		}
		return nil
	case *types2.Packed:
		switch size := l.sizeof(u); size {
		case 1, 2, 4, 8:
			return ssa.IntType(size)
		}
		return nil
	case *types2.TypeParam:
		panic(fmt.Sprintf("lower: unsubstituted type parameter %s", T))
	}
	return nil
}

// leaves returns the scalar components of T, or nil if T is held in memory
// as a unit (it has too many components or contains a ring or a packed byte
// array).
func (l *Lowerer) leaves(T types2.Type) []leaf {
	if t := l.scalarType(T); t != nil {
		return []leaf{{0, t}}
	}
	var out []leaf
	if !l.appendLeaves(&out, T, 0) {
		return nil
	}
	return out
}

func (l *Lowerer) appendLeaves(out *[]leaf, T types2.Type, base int64) bool {
	if len(*out) > 16 {
		return false
	}
	if t := l.scalarType(T); t != nil {
		*out = append(*out, leaf{base, t})
		return true
	}
	switch u := T.Underlying().(type) {
	case *types2.Basic: // string
		*out = append(*out, leaf{base, ssa.TPtr}, leaf{base + 8, ssa.TI64})
		return true
	case *types2.Slice:
		*out = append(*out, leaf{base, ssa.TPtr}, leaf{base + 8, ssa.TI64}, leaf{base + 16, ssa.TI64})
		return true
	case *types2.Interface:
		*out = append(*out, leaf{base, ssa.TPtr}, leaf{base + 8, ssa.TPtr})
		return true
	case *types2.Struct:
		fields := make([]*types2.Var, u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := l.sizes.Offsetsof(fields)
		for i, f := range fields {
			if !l.appendLeaves(out, f.Type(), base+offs[i]) {
				return false
			}
		}
		return true
	case *types2.Array:
		if u.Len() > 4 {
			return false
		}
		esize := l.sizeof(u.Elem())
		for i := int64(0); i < u.Len(); i++ {
			if !l.appendLeaves(out, u.Elem(), base+i*esize) {
				return false
			}
		}
		return true
	case *types2.Wrapper:
		if u.Kind() == types2.WrapAligned || u.Kind() == types2.WrapVolatile {
			return l.appendLeaves(out, u.Elem(), base)
		}
	}
	return false
}

// appendPads appends the padding ranges of a T at base: the gaps between
// struct fields and after the last one, and the tail of an aligned[T, N]
// beyond its element. It works from the type's layout alone, not from its
// leaves, so that SROA can check that the leaves account for every byte
// that is not padding (see ssa.Slot.LeavesCover).
func (l *Lowerer) appendPads(out *[]ssa.Pad, T types2.Type, base int64) {
	pad := func(off, end int64) {
		if end > off {
			*out = append(*out, ssa.Pad{Off: off, Size: end - off})
		}
	}
	if l.scalarType(T) != nil {
		return
	}
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		fields := make([]*types2.Var, u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := l.sizes.Offsetsof(fields)
		for i, f := range fields {
			l.appendPads(out, f.Type(), base+offs[i])
			end := l.sizeof(T)
			if i+1 < len(fields) {
				end = offs[i+1]
			}
			pad(base+offs[i]+l.sizeof(f.Type()), base+end)
		}
		if len(fields) == 0 {
			pad(base, base+l.sizeof(T))
		}
	case *types2.Array:
		esize := l.sizeof(u.Elem())
		for i := int64(0); i < u.Len(); i++ {
			l.appendPads(out, u.Elem(), base+i*esize)
		}
	case *types2.Wrapper:
		if u.Kind() == types2.WrapAligned || u.Kind() == types2.WrapVolatile {
			l.appendPads(out, u.Elem(), base)
			pad(base+l.sizeof(u.Elem()), base+l.sizeof(T))
		}
	}
}

// A param describes how one Plutonium-level parameter or result is passed.
type param struct {
	typ    types2.Type
	leaves []leaf // scalar components passed in order
	byRef  bool   // passed as one pointer to a caller-owned copy
}

// An abiSig is a lowered function signature.
type abiSig struct {
	sig         *types2.Signature
	params      []param // receiver first, if any
	results     []param
	sret        bool        // results returned through a hidden first pointer
	flatParams  []*ssa.Type // scalar parameter list as the backend sees it
	flatResults []*ssa.Type
	variadic    bool
}

// lowerSig computes the internal ABI of a signature whose types are concrete.
func (l *Lowerer) lowerSig(sig *types2.Signature) *abiSig {
	a := &abiSig{sig: sig, variadic: sig.Variadic()}
	var resultLeaves int
	if r := sig.Results(); r != nil {
		for i := 0; i < r.Len(); i++ {
			t := r.At(i).Type()
			lv := l.leaves(t)
			p := param{typ: t, leaves: lv}
			if lv == nil {
				resultLeaves = maxRegLeaves + 1 // memory-only result forces sret
			}
			resultLeaves += len(lv)
			a.results = append(a.results, p)
		}
	}
	if resultLeaves > maxRegLeaves {
		a.sret = true
		a.flatParams = append(a.flatParams, ssa.TPtr)
	} else {
		for _, p := range a.results {
			for _, lf := range p.leaves {
				a.flatResults = append(a.flatResults, lf.t)
			}
		}
	}
	add := func(t types2.Type) {
		lv := l.leaves(t)
		p := param{typ: t}
		if lv == nil || len(lv) > maxRegLeaves {
			p.byRef = true
			a.flatParams = append(a.flatParams, ssa.TPtr)
		} else {
			p.leaves = lv
			for _, lf := range lv {
				a.flatParams = append(a.flatParams, lf.t)
			}
		}
		a.params = append(a.params, p)
	}
	if recv := sig.Recv(); recv != nil {
		add(recv.Type())
	}
	ps := sig.Params()
	for i := 0; i < ps.Len(); i++ {
		add(ps.At(i).Type())
	}
	return a
}
