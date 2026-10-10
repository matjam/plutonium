package lower

import (
	"fmt"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// builtin lowers a call of a predeclared function or an unsafe function.
func (s *fnState) builtin(e *ast.CallExpr) []value {
	id := identOf(ast.Unparen(e.Fun))
	b, _ := s.info.Uses[id].(*types2.Builtin)
	if b == nil {
		s.l.internalf(e.Pos(), "unknown builtin")
		return nil
	}
	T := s.typeOf(e)
	one := func(v value) []value { return []value{v} }

	switch b.Name() {
	case "len", "cap":
		if tv, ok := s.info.Types[e.Args[0]]; ok && b.Name() == "len" && types2.LenByMethod(tv.Type) {
			return one(s.containerLen(e.Args[0], e.Pos()))
		}
		x := s.expr(e.Args[0])
		xt := unwrap(s.typeOf(e.Args[0]))
		switch u := xt.Underlying().(type) {
		case *types2.Basic: // string
			_, n := s.stringParts(x)
			return one(scalarValue(n, T))
		case *types2.Slice:
			_, n, c := s.sliceParts(x)
			if b.Name() == "cap" {
				return one(scalarValue(c, T))
			}
			return one(scalarValue(n, T))
		case *types2.Array:
			return one(scalarValue(s.constInt(ssa.TI64, u.Len()), T))
		case *types2.Pointer:
			arr := unwrap(u.Elem()).Underlying().(*types2.Array)
			return one(scalarValue(s.constInt(ssa.TI64, arr.Len()), T))
		}

	case "append":
		return one(s.appendCall(e, T))

	case "copy":
		dst := s.expr(e.Args[0])
		src := s.expr(e.Args[1])
		dp, dn, _ := s.sliceParts(dst)
		var sp, sn *ssa.Value
		if isString(s.typeOf(e.Args[1])) {
			sp, sn = s.stringParts(src)
		} else {
			sp, sn, _ = s.sliceParts(src)
		}
		n := s.minInt(dn, sn)
		et := unwrap(s.typeOf(e.Args[0])).Underlying().(*types2.Slice).Elem()
		bytes := s.newValue(ssa.OpMul, ssa.TI64, n, s.constInt(ssa.TI64, s.l.sizeof(s.subst(et))))
		s.callRuntime("memmove", nil, []*ssa.Value{dp, sp, bytes})
		return one(scalarValue(n, T))

	case "clear":
		x := s.expr(e.Args[0])
		p, n, _ := s.sliceParts(x)
		et := unwrap(s.typeOf(e.Args[0])).Underlying().(*types2.Slice).Elem()
		bytes := s.newValue(ssa.OpMul, ssa.TI64, n, s.constInt(ssa.TI64, s.l.sizeof(s.subst(et))))
		s.callRuntime("memclr", nil, []*ssa.Value{p, bytes})
		return nil

	case "min", "max":
		if isString(unwrap(T)) {
			return one(s.minMaxString(b.Name() == "min", e.Args, unwrap(T)))
		}
		vals := make([]*ssa.Value, len(e.Args))
		for i, a := range e.Args {
			vals[i] = s.exprAs(a, unwrap(T)).v
		}
		r := vals[0]
		for _, v := range vals[1:] {
			r = s.minMax(b.Name() == "min", r, v, unwrap(T))
		}
		return one(scalarValue(r, T))

	case "new":
		// new(T) or new(v), either with an optional allocator. new(v)
		// evaluates v before the allocator, then stores it in the new
		// variable.
		elem := unwrap(T).Underlying().(*types2.Pointer).Elem()
		var v value
		isValue := !s.info.Types[e.Args[0]].IsType()
		if isValue {
			v = s.materialize(s.exprAs(e.Args[0], elem))
		}
		ptr := s.allocate(s.constInt(ssa.TI64, s.l.sizeof(elem)), s.l.alignof(elem), e.Args[1:])
		if isValue {
			s.storeTo(ptr, v, elem)
		}
		return one(scalarValue(ptr, T))

	case "make":
		st, isSlice := unwrap(T).Underlying().(*types2.Slice)
		if !isSlice {
			// The checker rejects channel and map types. Channels (roadmap
			// item 15) will allocate here, from the optional final
			// allocator as slices do: make(chan T, n, a).
			s.l.unsupportedf(e.Pos(), "make of %s is not supported", T)
			return nil
		}
		et := s.subst(st.Elem())
		n := s.indexValue(e.Args[1])
		c := n
		rest := e.Args[2:]
		if len(rest) > 0 && isIntegerType(unwrap(s.typeOf(rest[0]))) {
			c = s.indexValue(rest[0])
			rest = rest[1:]
		}
		ok := s.newValue(ssa.OpLequ, ssa.TBool, n, c)
		s.panicUnless(ok, e.Pos(), func() { s.callRuntimeNoReturn("panicMakeLen", n, c) })
		bytes := s.newValue(ssa.OpMul, ssa.TI64, c, s.constInt(ssa.TI64, s.l.sizeof(et)))
		ptr := s.allocate(bytes, s.l.alignof(et), rest)
		return one(s.makeSlice(ptr, n, c, T))

	case "panic":
		arg := s.exprAs(e.Args[0], types2.NewInterfaceType(nil, nil))
		parts := s.leafValues(arg, []leaf{{0, ssa.TPtr}, {8, ssa.TPtr}})
		s.callRuntimeNoReturn("panicValue", parts...)
		dead := s.newBlock(ssa.BlockInvalid)
		s.seal(dead)
		s.startBlock(dead)
		return nil

	case "print", "println":
		// As in Go, every argument is evaluated before anything is printed,
		// so an argument that prints or changes a later argument's operands
		// has finished by the time output starts.
		srcs := s.argSources(e.Args)
		vals := make([]value, len(srcs))
		nodes := make([]ast.Node, len(srcs))
		for i, a := range srcs {
			if a.e != nil {
				vals[i] = s.materialize(s.printOperand(a.e))
				nodes[i] = a.e
			} else {
				vals[i] = s.materialize(a.v)
				nodes[i] = e
			}
		}
		for i, v := range vals {
			if i > 0 && b.Name() == "println" {
				s.callRuntime("printSpace", nil)
			}
			s.printValue(v, unwrap(v.T), nodes[i])
		}
		if b.Name() == "println" {
			s.callRuntime("printNewline", nil)
		}
		return nil

	case "Sizeof", "Alignof", "Offsetof":
		// Constant except in generic code, where the size depends on the
		// type arguments.
		var n int64
		switch b.Name() {
		case "Sizeof":
			n = s.l.sizeof(s.typeOf(e.Args[0]))
		case "Alignof":
			n = s.l.alignof(s.typeOf(e.Args[0]))
		default:
			n = s.offsetof(e.Args[0])
		}
		return one(scalarValue(s.constInt(ssa.TI64, n), T))

	case "Add": // unsafe.Add
		p := s.scalar(e.Args[0])
		n := s.indexValue(e.Args[1])
		return one(scalarValue(s.newValue(ssa.OpAddPtr, ssa.TPtr, p, n), T))
	case "Slice": // unsafe.Slice
		p := s.scalar(e.Args[0])
		n := s.indexValue(e.Args[1])
		return one(s.makeSlice(p, n, n, T))
	case "SliceData":
		p, _, _ := s.sliceParts(s.expr(e.Args[0]))
		return one(scalarValue(p, T))
	case "String": // unsafe.String
		p := s.scalar(e.Args[0])
		n := s.indexValue(e.Args[1])
		return one(s.makeString(p, n, T))
	case "StringData":
		p, _ := s.stringParts(s.expr(e.Args[0]))
		return one(scalarValue(p, T))
	}
	s.l.unsupportedf(e.Pos(), "builtin %s is not supported", b.Name())
	return nil
}

// offsetof computes unsafe.Offsetof(x.f) for a selector, following
// embedded fields.
func (s *fnState) offsetof(e ast.Expr) int64 {
	se, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return 0
	}
	sel := s.info.Selections[se]
	T := unwrap(s.typeOf(se.X))
	if isPointerLike(T) {
		// x.f through a pointer x: the offset is within *x.
		T = unwrap(pointerElem(T))
	}
	var off int64
	for _, idx := range sel.Index() {
		st, ok := T.Underlying().(*types2.Struct)
		if !ok {
			break
		}
		fields := make([]*types2.Var, st.NumFields())
		for i := range fields {
			fields[i] = st.Field(i)
		}
		off += s.l.sizes.Offsetsof(fields)[idx]
		T = unwrap(s.subst(st.Field(idx).Type()))
	}
	return off
}

func (s *fnState) minInt(a, b *ssa.Value) *ssa.Value {
	lt := s.newValue(ssa.OpLess, ssa.TBool, a, b)
	return s.newValue(ssa.OpSelect, a.Type, lt, a, b)
}

// minMax returns min(a, b) or max(a, b) with Go's rules: for floats, a NaN
// operand gives NaN.
func (s *fnState) minMax(isMin bool, a, b *ssa.Value, T types2.Type) *ssa.Value {
	if a.Type.IsFloat() {
		// Floats have no conditional move: use branches. A NaN operand
		// gives NaN.
		t := a.Type
		res := s.tempSize(t.Size, t.Size, "minmax")
		s.store(t, res, b)
		var better *ssa.Value
		if isMin {
			better = s.newValue(ssa.OpFLess, ssa.TBool, a, b)
		} else {
			better = s.newValue(ssa.OpFLess, ssa.TBool, b, a)
		}
		s.ifThen(better, func() { s.store(t, res, a) })
		// Equal operands differ only if they are zeros of different
		// signs: min is -0 if either is, max is +0 if either is. ORing
		// (for min) or ANDing (for max) the bits gives that, and leaves
		// any other pair of equal values unchanged.
		s.ifThen(s.newValue(ssa.OpFEq, ssa.TBool, a, b), func() {
			it := ssa.IntType(t.Size)
			op := ssa.OpOr
			if !isMin {
				op = ssa.OpAnd
			}
			bits := s.newValue(op, it, s.newValue(ssa.OpBitcast, it, a), s.newValue(ssa.OpBitcast, it, b))
			s.store(t, res, s.newValue(ssa.OpBitcast, t, bits))
		})
		s.ifThen(s.newValue(ssa.OpFNe, ssa.TBool, a, a), func() { s.store(t, res, a) })
		s.ifThen(s.newValue(ssa.OpFNe, ssa.TBool, b, b), func() { s.store(t, res, b) })
		return s.load(t, res)
	}
	var lt *ssa.Value
	switch {
	case isUnsigned(T):
		lt = s.newValue(ssa.OpLessu, ssa.TBool, a, b)
	default:
		lt = s.newValue(ssa.OpLess, ssa.TBool, a, b)
	}
	var r *ssa.Value
	if isMin {
		r = s.newValue(ssa.OpSelect, a.Type, lt, a, b)
	} else {
		r = s.newValue(ssa.OpSelect, a.Type, lt, b, a)
	}
	return r
}

// minMaxString returns min(args...) or max(args...) of strings of type T,
// comparing with the runtime's strcmp; the first of equal strings wins.
func (s *fnState) minMaxString(isMin bool, args []ast.Expr, T types2.Type) value {
	res := s.temp(T, "minmax")
	s.storeTo(res, s.exprAs(args[0], T), T)
	for _, a := range args[1:] {
		v := s.materialize(s.exprAs(a, T))
		cur := s.loadFrom(res, T)
		c := s.callRuntime("strcmp", []*ssa.Type{ssa.TI64}, s.stringLeaves(v), s.stringLeaves(cur))[0]
		zero := s.constInt(ssa.TI64, 0)
		var better *ssa.Value
		if isMin {
			better = s.newValue(ssa.OpLess, ssa.TBool, c, zero)
		} else {
			better = s.newValue(ssa.OpLess, ssa.TBool, zero, c)
		}
		s.ifThen(better, func() { s.storeTo(res, v, T) })
	}
	return s.loadFrom(res, T)
}

// allocate returns size zeroed bytes from the allocator given in extra (an
// optional trailing argument of new or make) or the current default.
func (s *fnState) allocate(size *ssa.Value, align int64, extra []ast.Expr) *ssa.Value {
	alignV := s.constInt(ssa.TI64, align)
	if len(extra) > 0 {
		allocT := s.l.rtFunc("allocWith").Type().(*types2.Signature).Params().At(0).Type()
		a := s.exprAs(extra[0], allocT)
		parts := s.leafValues(a, []leaf{{0, ssa.TPtr}, {8, ssa.TPtr}})
		return s.callRuntime("allocWith", []*ssa.Type{ssa.TPtr}, parts, []*ssa.Value{size, alignV})[0]
	}
	return s.callRuntime("alloc", []*ssa.Type{ssa.TPtr}, []*ssa.Value{size, alignV})[0]
}

// appendCall lowers append, which never allocates: it fills the slice's spare
// capacity and panics if there is not enough.
func (s *fnState) appendCall(e *ast.CallExpr, T types2.Type) value {
	st := unwrap(T).Underlying().(*types2.Slice)
	et := s.subst(st.Elem())
	esize := s.l.sizeof(et)
	sl := s.materialize(s.expr(e.Args[0]))
	ptr, n, c := s.sliceParts(sl)

	if e.Ellipsis.IsValid() {
		src := s.expr(e.Args[1])
		var sp, sn *ssa.Value
		if isString(s.typeOf(e.Args[1])) {
			sp, sn = s.stringParts(src)
		} else {
			sp, sn, _ = s.sliceParts(src)
		}
		newLen := s.newValue(ssa.OpAdd, ssa.TI64, n, sn)
		s.appendCheck(newLen, c, e)
		dst := s.elemAddr(ptr, n, et)
		bytes := s.newValue(ssa.OpMul, ssa.TI64, sn, s.constInt(ssa.TI64, esize))
		s.callRuntime("memmove", nil, []*ssa.Value{dst, sp, bytes})
		return s.makeSlice(ptr, newLen, c, T)
	}

	extra := e.Args[1:]
	vals := make([]value, len(extra))
	for i, a := range extra {
		vals[i] = s.materialize(s.exprAs(a, et))
	}
	newLen := s.newValue(ssa.OpAdd, ssa.TI64, n, s.constInt(ssa.TI64, int64(len(extra))))
	s.appendCheck(newLen, c, e)
	for i, v := range vals {
		idx := s.newValue(ssa.OpAdd, ssa.TI64, n, s.constInt(ssa.TI64, int64(i)))
		s.storeTo(s.elemAddr(ptr, idx, et), v, et)
	}
	return s.makeSlice(ptr, newLen, c, T)
}

func (s *fnState) appendCheck(newLen, c *ssa.Value, e *ast.CallExpr) {
	ok := s.newValue(ssa.OpLequ, ssa.TBool, newLen, c)
	s.panicUnless(ok, e.Pos(), func() { s.callRuntimeNoReturn("panicAppend", newLen, c) })
}

// printOperand evaluates an argument of print or println, giving untyped
// constants their default type.
func (s *fnState) printOperand(a ast.Expr) value {
	T := unwrap(s.typeOf(a))
	if tv := s.info.Types[a]; tv.Value != nil {
		if b, ok := T.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
			T = defaultType(tv.Value)
		}
	}
	return s.exprAs(a, T)
}

// printValue prints one value for print or println.
func (s *fnState) printValue(v value, T types2.Type, a ast.Node) {
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		info := u.Info()
		switch {
		case info&types2.IsString != 0:
			p, n := s.stringParts(v)
			s.callRuntime("printString", nil, []*ssa.Value{p, n})
		case info&types2.IsBoolean != 0:
			s.callRuntime("printBool", nil, []*ssa.Value{v.v})
		case info&types2.IsFloat != 0:
			s.callRuntime("printFloat", nil, []*ssa.Value{s.convertScalar(v.v, T, types2.Typ[types2.Float64])})
		case info&types2.IsUnsigned != 0:
			s.callRuntime("printUint", nil, []*ssa.Value{s.convertScalar(v.v, T, types2.Typ[types2.Uint64])})
		case info&types2.IsInteger != 0:
			s.callRuntime("printInt", nil, []*ssa.Value{s.convertScalar(v.v, T, types2.Typ[types2.Int64])})
		case u.Kind() == types2.UnsafePointer:
			s.callRuntime("printPointer", nil, []*ssa.Value{v.v})
		}
	case *types2.Pointer, *types2.Signature:
		s.callRuntime("printPointer", nil, []*ssa.Value{v.v})
	default:
		s.l.unsupportedf(a.Pos(), "cannot print a value of type %s", T)
	}
}

// ----------------------------------------------------------------------------
// Conversions

// conversion lowers T(x).
func (s *fnState) conversion(e *ast.CallExpr) value {
	to := s.typeOf(e)
	arg := e.Args[0]
	from := s.typeOf(arg)
	tu := unwrap(to).Underlying()
	fu := unwrap(from).Underlying()

	if isInterface(to) {
		return s.assignConvert(s.expr(arg), to, e)
	}

	// []byte(s), string(b) and friends copy, so they would allocate.
	if isString(to) != isString(from) {
		s.l.errorf(e.Pos(), diag.AllocatingConv, "conversion between %s and %s is not permitted in Plutonium (it allocates a copy); use unsafe.String or unsafe.StringData", from, to)
		return s.zeroValue(to)
	}

	// Slice to array or array pointer.
	if sl, ok := fu.(*types2.Slice); ok {
		var arr *types2.Array
		ptrResult := false
		switch t := tu.(type) {
		case *types2.Array:
			arr = t
		case *types2.Pointer:
			arr, _ = unwrap(t.Elem()).Underlying().(*types2.Array)
			ptrResult = true
		}
		if arr != nil {
			v := s.expr(arg)
			p, n, _ := s.sliceParts(v)
			ok := s.newValue(ssa.OpLequ, ssa.TBool, s.constInt(ssa.TI64, arr.Len()), n)
			s.panicUnless(ok, e.Pos(), func() { s.callRuntimeNoReturn("panicSliceConvert", n, s.constInt(ssa.TI64, arr.Len())) })
			if ptrResult {
				return scalarValue(p, to)
			}
			_ = sl
			tmp := s.temp(to, "arr")
			s.move(tmp, p, s.l.sizeof(to))
			return aggValue(tmp, to)
		}
	}

	// nil converted to a slice (or another aggregate) is its zero value,
	// not a one-word nil.
	if b, ok := fu.(*types2.Basic); ok && b.Kind() == types2.UntypedNil && s.l.scalarType(to) == nil {
		return s.zeroValue(to)
	}

	v := s.expr(arg)
	if v.addr {
		if s.l.scalarType(unwrap(to)) != nil {
			// A byte array to a packed type held in a register.
			return s.loadFrom(v.v, to)
		}
		// Aggregates convert between identical layouts unchanged.
		v.T = to
		return v
	}
	if s.l.scalarType(unwrap(to)) == nil {
		// A packed value held in a register to its byte array.
		tmp := s.temp(to, "conv")
		s.storeTo(tmp, v, from)
		return aggValue(tmp, to)
	}
	return scalarValue(s.convertScalar(v.v, from, to), to)
}

// ----------------------------------------------------------------------------
// ring[T, N]

// ringCall expands a call of a built-in ring method inline.
func (s *fnState) ringCall(e *ast.CallExpr, se *ast.SelectorExpr, fn *types2.Func, path []int, w *types2.Wrapper) []value {
	recv := s.methodReceiver(se.X, fn, path) // pointer to the ring
	var args []value
	if fn.Name() == "Push" {
		args = append(args, s.materialize(s.exprAs(e.Args[0], s.subst(w.Elem()))))
	}
	return s.ringOp(fn.Name(), recv.v, w, args, e.Pos())
}

// ringOf returns the ring type of fn if it is a built-in ring method, or
// nil.
func (s *fnState) ringOf(fn *types2.Func) *types2.Wrapper {
	return ringMethodRing(fn, s.subst)
}

// ringMethodRing returns the ring type of fn if it is a built-in ring
// method, or nil. subst instantiates the type of fn's receiver.
func ringMethodRing(fn *types2.Func, subst func(types2.Type) types2.Type) *types2.Wrapper {
	recv := fn.Type().(*types2.Signature).Recv()
	if fn.Pkg() != nil || recv == nil {
		return nil
	}
	p, ok := subst(recv.Type()).(*types2.Pointer)
	if !ok {
		return nil
	}
	if w, ok := p.Elem().Underlying().(*types2.Wrapper); ok && w.Kind() == types2.WrapRing {
		return w
	}
	return nil
}

// ringMethodFunc returns a function performing ring method fn of ring
// type w on the ring its receiver points at, for a ring method used as a
// value or called through an interface; calls name the method directly
// and are expanded inline instead.
func (l *Lowerer) ringMethodFunc(fn *types2.Func, w *types2.Wrapper) (*ssa.Sym, *types2.Signature) {
	ptr := types2.NewPointer(w)
	obj, _, _ := types2.LookupFieldOrMethod(ptr, false, nil, fn.Name())
	full := obj.(*types2.Func).Type().(*types2.Signature)
	name := fmt.Sprintf("builtin.(*ring[%s,%d]).%s", typeString(w.Elem()), w.N(), fn.Name())
	if sym := l.funcs[name]; sym != nil {
		return sym, full
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	l.queue = append(l.queue, &funcJob{sym: sym, sig: full, build: func(s *fnState) {
		in := s.incomingArgs(full)
		s.returnValues(s.ringOp(fn.Name(), in[0].v, w, in[1:], token.NoPos))
	}})
	return sym, full
}

// ringOp expands ring method name on the ring that p points at, with the
// arguments already evaluated.
func (s *fnState) ringOp(name string, p *ssa.Value, w *types2.Wrapper, args []value, pos token.Pos) []value {
	et := s.subst(w.Elem())
	n := w.N()
	mask := s.constInt(ssa.TI64, n-1)
	align := s.l.alignof(et)
	dataOff := (16 + align - 1) &^ (align - 1)
	headPtr := p
	tailPtr := s.offPtr(p, 8)
	slot := func(idx *ssa.Value) *ssa.Value {
		i := s.newValue(ssa.OpAnd, ssa.TI64, idx, mask)
		return s.elemAddr(s.offPtr(p, dataOff), i, et)
	}
	boolT := types2.Typ[types2.Bool]
	intT := types2.Typ[types2.Int]

	switch name {
	case "Len":
		d := s.newValue(ssa.OpSub, ssa.TI64, s.load(ssa.TI64, headPtr), s.load(ssa.TI64, tailPtr))
		return []value{scalarValue(d, intT)}
	case "Cap":
		return []value{scalarValue(s.constInt(ssa.TI64, n), intT)}
	case "Full":
		d := s.newValue(ssa.OpSub, ssa.TI64, s.load(ssa.TI64, headPtr), s.load(ssa.TI64, tailPtr))
		return []value{scalarValue(s.newValue(ssa.OpEq, ssa.TBool, d, s.constInt(ssa.TI64, n)), boolT)}
	case "Empty":
		eq := s.newValue(ssa.OpEq, ssa.TBool, s.load(ssa.TI64, headPtr), s.load(ssa.TI64, tailPtr))
		return []value{scalarValue(eq, boolT)}
	case "Reset":
		s.store(ssa.TI64, headPtr, s.constInt(ssa.TI64, 0))
		s.store(ssa.TI64, tailPtr, s.constInt(ssa.TI64, 0))
		return nil
	case "Push":
		v := args[0]
		res := s.temp(boolT, "ok")
		s.store(ssa.TBool, res, s.constBool(false))
		head := s.load(ssa.TI64, headPtr)
		d := s.newValue(ssa.OpSub, ssa.TI64, head, s.load(ssa.TI64, tailPtr))
		hasRoom := s.newValue(ssa.OpLessu, ssa.TBool, d, s.constInt(ssa.TI64, n))
		s.ifThen(hasRoom, func() {
			s.storeTo(slot(head), v, et)
			s.store(ssa.TI64, headPtr, s.newValue(ssa.OpAdd, ssa.TI64, head, s.constInt(ssa.TI64, 1)))
			s.store(ssa.TBool, res, s.constBool(true))
		})
		return []value{scalarValue(s.load(ssa.TBool, res), boolT)}
	case "Pop", "Peek":
		out := s.temp(et, "elem")
		s.zero(out, s.l.sizeof(et))
		res := s.temp(boolT, "ok")
		s.store(ssa.TBool, res, s.constBool(false))
		tail := s.load(ssa.TI64, tailPtr)
		nonEmpty := s.newValue(ssa.OpNe, ssa.TBool, s.load(ssa.TI64, headPtr), tail)
		s.ifThen(nonEmpty, func() {
			s.storeTo(out, s.loadFrom(slot(tail), et), et)
			if name == "Pop" {
				s.store(ssa.TI64, tailPtr, s.newValue(ssa.OpAdd, ssa.TI64, tail, s.constInt(ssa.TI64, 1)))
			}
			s.store(ssa.TBool, res, s.constBool(true))
		})
		return []value{s.loadFrom(out, et), scalarValue(s.load(ssa.TBool, res), boolT)}
	}
	s.l.internalf(pos, "unknown ring method %s", name)
	return nil
}

// ifThen emits "if cond { body }".
func (s *fnState) ifThen(cond *ssa.Value, body func()) {
	then := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	s.endIf(cond, then, done)
	s.seal(then)
	s.startBlock(then)
	body()
	s.endPlain(done)
	s.seal(done)
	s.startBlock(done)
}
