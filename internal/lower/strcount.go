package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// String reference counting ("string drop glue").
//
// A string is an immutable, reference-counted value (language spec section
// 6.10). This file and strscope.go are the whole of it in lowering, kept
// apart from the ownership work (moves and owner drops) on purpose: a
// counted value is copied freely and never moved by the program, and its
// drops are implicit.
//
// A type is counted if a value of it holds a string reference: string
// itself, and structs and arrays with a counted field or element. Slices,
// pointers and interfaces only look at counted values; the memory they look
// at holds the references.
//
// Every place that holds a counted value (a variable, a field, an element,
// an interface box, a result) owns one reference per string in it, or holds
// the zero value. Lowering keeps that invariant:
//
//   - Storing into a place takes a reference for the new value (a retain,
//     or the hand-over of a temporary's) and then releases the old one.
//   - A call's parameters are borrowed: the caller keeps them alive and the
//     callee retains only what it keeps. Results are owned: the callee
//     hands the caller one reference.
//   - A result or other new value that is not stored is a statement
//     temporary, released when the statement ends.
//   - Variables are released when their scope ends, on every path out of
//     it; frame slots that outlive a statement (interface boxes, &T{...}
//     literals, saved defer arguments) when the function returns.
//
// Static strings (literals, constants) have a control word with no counted
// buffer, so the runtime's retain and release do nothing for them; values
// lowering knows to be static skip the calls altogether.

// countKind says who keeps a counted value's references alive.
type countKind uint8

const (
	// cntUnknown: a borrowed value whose source is not known to stay
	// unchanged. Treated as cntUnstable.
	cntUnknown countKind = iota
	// cntStatic: the value holds no counted reference (a constant).
	cntStatic
	// cntOwned: a statement temporary (value.tmp) holds the references.
	cntOwned
	// cntStable: borrowed from a variable or temporary that nothing else
	// can change while the statement runs.
	cntStable
	// cntUnstable: borrowed from memory that a call may change.
	cntUnstable
)

// An ownedTemp is a statement temporary that holds references: a call
// result, a concatenation, a conversion, a composite literal, or an
// unstable value retained to keep it alive (hold).
type ownedTemp struct {
	addr     *ssa.Value
	T        types2.Type
	consumed bool // handed over to a place; nothing to release
}

// counted reports whether a value of T holds string references.
func (l *Lowerer) counted(T types2.Type) bool {
	if T == nil {
		return false
	}
	key := typeString(T)
	if c, ok := l.countedTypes[key]; ok {
		return c
	}
	l.countedTypes[key] = false // break cycles through named types
	c := false
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		c = u.Info()&types2.IsString != 0
	case *types2.Struct:
		for i := 0; i < u.NumFields() && !c; i++ {
			c = l.counted(u.Field(i).Type())
		}
	case *types2.Array:
		c = u.Len() > 0 && l.counted(u.Elem())
	case *types2.Wrapper:
		switch u.Kind() {
		case types2.WrapAligned, types2.WrapVolatile, types2.WrapRing:
			c = l.counted(u.Elem())
		}
	case *types2.TaggedUnion:
		for i := 0; i < u.NumVariants() && !c; i++ {
			c = u.Variant(i).HasPayload() && l.counted(u.Variant(i).Payload())
		}
	}
	l.countedTypes[key] = c
	return c
}

// ----------------------------------------------------------------------------
// Retain and release

// eachString calls f with the address of the control word of every string
// in the T at ptr, looping over large arrays.
func (s *fnState) eachString(ptr *ssa.Value, T types2.Type, f func(ctl *ssa.Value)) {
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		f(s.offPtr(ptr, strCtlOff))
	case *types2.Struct:
		fields := make([]*types2.Var, u.NumFields())
		for i := range fields {
			fields[i] = u.Field(i)
		}
		offs := s.l.sizes.Offsetsof(fields)
		for i, fld := range fields {
			if ft := s.subst(fld.Type()); s.l.counted(ft) {
				s.eachString(s.offPtr(ptr, offs[i]), ft, f)
			}
		}
	case *types2.Array:
		et := s.subst(u.Elem())
		if !s.l.counted(et) {
			return
		}
		if u.Len() <= 4 {
			esize := s.l.sizeof(et)
			for i := int64(0); i < u.Len(); i++ {
				s.eachString(s.offPtr(ptr, i*esize), et, f)
			}
			return
		}
		s.forEach(ptr, s.constInt(ssa.TI64, u.Len()), et, f)
	case *types2.Wrapper:
		et := s.subst(u.Elem())
		if u.Kind() == types2.WrapRing {
			// {head, tail uint64; buf [N]T}: slots outside the live range
			// are zero (builtin.go, ringOp), so every slot can be counted.
			align := s.l.alignof(et)
			s.eachString(s.offPtr(ptr, (16+align-1)&^(align-1)), types2.NewArray(et, u.N()), f)
			return
		}
		s.eachString(ptr, et, f)
	case *types2.TaggedUnion:
		// Only the payload of the variant the tag names holds references.
		ul := s.l.unionLayoutOf(T, s.pos)
		tag := s.unionTag(ul, aggValue(ptr, T))
		for i := 0; i < u.NumVariants(); i++ {
			v := u.Variant(i)
			if P := s.subst(v.Payload()); v.HasPayload() && s.l.counted(P) {
				s.ifThen(s.newValue(ssa.OpEq, ssa.TBool, tag, s.constInt(ul.tag, int64(i))), func() {
					s.eachString(s.offPtr(ptr, ul.payload), P, f)
				})
			}
		}
	}
}

// forEach calls eachString for each of the n values of type et at ptr, in
// a loop.
func (s *fnState) forEach(ptr, n *ssa.Value, et types2.Type, f func(ctl *ssa.Value)) {
	idx := s.temp(types2.Typ[types2.Int], "i")
	s.store(ssa.TI64, idx, s.constInt(ssa.TI64, 0))
	head := s.newBlock(ssa.BlockInvalid)
	body := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	s.endPlain(head)
	s.startBlock(head)
	i := s.load(ssa.TI64, idx)
	s.endIf(s.newValue(ssa.OpLess, ssa.TBool, i, n), body, done)
	s.seal(body)
	s.startBlock(body)
	i = s.load(ssa.TI64, idx)
	s.eachString(s.elemAddr(ptr, i, et), et, f)
	s.store(ssa.TI64, idx, s.newValue(ssa.OpAdd, ssa.TI64, s.load(ssa.TI64, idx), s.constInt(ssa.TI64, 1)))
	s.endPlain(head)
	s.seal(head)
	s.seal(done)
	s.startBlock(done)
}

// retainAt takes a reference for every string in the T at ptr.
func (s *fnState) retainAt(ptr *ssa.Value, T types2.Type) {
	s.eachString(ptr, T, func(ctl *ssa.Value) {
		s.callRuntime("strRetain", nil, []*ssa.Value{s.load(ssa.TI64, ctl)})
	})
}

// releaseAt drops the reference of every string in the T at ptr.
func (s *fnState) releaseAt(ptr *ssa.Value, T types2.Type) {
	s.eachString(ptr, T, func(ctl *ssa.Value) {
		s.callRuntime("strRelease", nil, []*ssa.Value{s.load(ssa.TI64, ctl)})
	})
}

// releaseRange drops the references held by the n values of type et at
// ptr.
func (s *fnState) releaseRange(ptr, n *ssa.Value, et types2.Type) {
	s.forEach(ptr, n, et, func(ctl *ssa.Value) {
		s.callRuntime("strRelease", nil, []*ssa.Value{s.load(ssa.TI64, ctl)})
	})
}

// retainRange takes a reference for each of the n values of type et at ptr.
func (s *fnState) retainRange(ptr, n *ssa.Value, et types2.Type) {
	s.forEach(ptr, n, et, func(ctl *ssa.Value) {
		s.callRuntime("strRetain", nil, []*ssa.Value{s.load(ssa.TI64, ctl)})
	})
}

// takeRef makes the counted value v's references the caller's to store:
// a statement temporary hands its references over, a static value has
// none, and anything else is retained.
func (s *fnState) takeRef(v value, T types2.Type) {
	switch {
	case v.cnt == cntStatic:
	case v.cnt == cntOwned && v.tmp != nil && !v.tmp.consumed:
		v.tmp.consumed = true
	default:
		s.retainAt(v.v, T)
	}
}

// takeRefAt is takeRef for a copy of v already stored at dst, which is
// where a retain is made: v itself need not be in memory.
func (s *fnState) takeRefAt(v value, dst *ssa.Value, T types2.Type) {
	switch {
	case v.cnt == cntStatic:
	case v.cnt == cntOwned && v.tmp != nil && !v.tmp.consumed:
		v.tmp.consumed = true
	default:
		s.retainAt(dst, T)
	}
}

// storeCounted stores v into the place lv, whose type is counted, with the
// semantics of assignment: the place takes a reference for v and drops the
// one it held. init says the place holds the zero value, so there is
// nothing to drop. The new references are taken before the old are
// dropped, so x = x and x = x[1:] are safe.
func (s *fnState) storeCounted(lv lvalue, v value, init bool) {
	T := unwrap(lv.T)
	var oldCtl, oldCopy *ssa.Value
	if !init {
		if isString(T) {
			oldCtl = s.load(ssa.TI64, s.offPtr(lv.ptr, strCtlOff))
		} else {
			oldCopy = s.temp(T, "old")
			s.move(oldCopy, lv.ptr, s.l.sizeof(T))
		}
	}
	s.takeRef(v, T)
	s.storeLV(lv, v)
	switch {
	case oldCtl != nil:
		s.callRuntime("strRelease", nil, []*ssa.Value{oldCtl})
	case oldCopy != nil:
		s.releaseAt(oldCopy, T)
	}
}

// assign stores v into lv, counting if lv's type is counted.
func (s *fnState) assign(lv lvalue, v value, init bool) {
	// A container element (x[k] through SetIndex) is stored by a call,
	// which takes its own reference.
	if lv.bits == nil && lv.elem == nil && s.l.counted(unwrap(lv.T)) {
		s.storeCounted(lv, v, init)
		return
	}
	s.storeLV(lv, v)
}

// ----------------------------------------------------------------------------
// Temporaries

// ownTemp registers the counted value at addr, of type T, as a temporary of
// the current statement, which releases it at its end unless a store takes
// it over, and returns it as a value.
func (s *fnState) ownTemp(addr *ssa.Value, T types2.Type) value {
	t := &ownedTemp{addr: addr, T: T}
	s.addTemp(t)
	return value{v: addr, addr: true, T: T, cnt: cntOwned, tmp: t}
}

// hold returns a counted value that stays valid until the end of the
// statement whatever the calls in it do: a borrow of memory that a call
// could change is copied into a temporary and retained.
func (s *fnState) hold(v value) value {
	if !v.addr || !s.l.counted(v.T) {
		return v
	}
	switch v.cnt {
	case cntStatic, cntOwned, cntStable:
		return v
	}
	tmp := s.temp(v.T, "held")
	s.move(tmp, v.v, s.l.sizeof(v.T))
	from := len(s.callLog)
	s.retainAt(tmp, v.T)
	held := s.ownTemp(tmp, v.T)
	s.startHold(held.tmp, from) // the optimiser may remove it (strelide.go)
	return held
}

// holdOwned is hold, except that a stable borrow is retained too: the
// result is a temporary or static, never a borrow. Assignments with several
// values use it, since storing one value may release another's source.
func (s *fnState) holdOwned(v value) value {
	if !v.addr || !s.l.counted(v.T) || v.cnt == cntStatic || isTemp(v) {
		return v
	}
	tmp := s.temp(v.T, "held")
	s.move(tmp, v.v, s.l.sizeof(v.T))
	s.retainAt(tmp, v.T)
	return s.ownTemp(tmp, v.T)
}

// holdAcross holds v if any of the expressions evaluated before v is used
// calls a function, which could change v's source.
func (s *fnState) holdAcross(v value, later ...ast.Expr) value {
	if v.cnt == cntUnknown || v.cnt == cntUnstable {
		for _, e := range later {
			if e != nil && s.callsFunction(e) {
				return s.hold(v)
			}
		}
	}
	return v
}

// isTemp reports whether v is a statement temporary itself, holding
// references nothing else will release, rather than a value derived from
// one (a substring shares its temporary's reference).
func isTemp(v value) bool {
	return v.cnt == cntOwned && v.tmp != nil && !v.tmp.consumed && v.tmp.addr == v.v
}

// stableFrom returns the count kind of a value read from inside v: the
// field of a temporary or of a stable value is stable as long as v is.
func stableFrom(v value) countKind {
	switch v.cnt {
	case cntStatic:
		return cntStatic
	case cntOwned, cntStable:
		return cntStable
	}
	return cntUnstable
}
