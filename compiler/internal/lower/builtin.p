// Translated by go2pu from internal/lower/builtin.go at the go-cutover tag; edited by hand since.

package lower

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/ssa"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
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
	one := func(v value) []value { return compat.Clone([]value{v}) }

	switch b.Name() {
	case "len", "cap":
		if tv, ok := s.info.Types[e.Args[0]]; ok && b.Name() == "len" && types2.LenByMethod(tv.Type) {
			return one(s.containerLen(e.Args[0], e.Pos()))
		}
		if mapOf(s.typeOf(e.Args[0])) != nil {
			return one(s.mapLen(e.Args[0], T))
		}
		x := s.expr(e.Args[0])
		xt := unwrap(s.typeOf(e.Args[0]))
		switch u := asPointer(xt.Underlying()).(type) {
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
		case *types2.Chan:
			fn := "chanlen"
			if b.Name() == "cap" {
				fn = "chancap"
			}
			return one(scalarValue(s.callRuntime(fn, compat.Clone([]*ssa.Type{ssa.TI64}), []*ssa.Value{x.v})[0], T))
		}

	case "close":
		s.callRuntime("closechan", nil, []*ssa.Value{s.scalar(e.Args[0])})
		return nil

	case "copy":
		dst := s.expr(e.Args[0])
		src := s.expr(e.Args[1])
		n := s.copySlice(dst, src, isString(s.typeOf(e.Args[1])))
		return one(scalarValue(n, T))

	case "Drop":
		// unsafe.Drop(p): the drop glue of *p. The glue is Plutonium code,
		// which checks its own accesses: p keeps its generation tag, so
		// that the Drop method sees the same pointer as every other
		// method (the runtime's timers compare them).
		p := s.expr(e.Args[0])
		s.dropAt(p.v, pointerElem(s.typeOf(e.Args[0])))
		return nil

	case "take":
		// take(p): the value at p, which then holds the zero value
		// (language spec section 18.5).
		p := s.expr(e.Args[0])
		ptr := s.checked(p.v)
		s.staticWriteType(ptr, T)
		v := s.materialize(s.loadLV(lvalue{ptr: ptr, T: T}))
		s.zero(ptr, s.l.sizeof(T))
		// The references in the place move out with the bytes: the result
		// holds them, and releases them at the end of the statement unless
		// something takes them over.
		return one(s.heldAfterMove(v))

	case "delete":
		s.mapDelete(e)
		return nil

	case "clear":
		if mapOf(s.typeOf(e.Args[0])) != nil {
			s.mapClear(e)
			return nil
		}
		x := s.expr(e.Args[0])
		et := s.subst(unwrap(s.typeOf(e.Args[0])).Underlying().(*types2.Slice).Elem())
		s.clearSlice(x, et)
		return nil

	case "min", "max":
		if isString(unwrap(T)) {
			return one(s.minMaxString(b.Name() == "min", e.Args, unwrap(T)))
		}
		vals := compat.Make[*ssa.Value](len(e.Args))
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
		// The result is an owned[T] (box.go).
		elem := s.subst(unwrap(T).Underlying().(*types2.Wrapper).Elem())
		var v value
		isValue := !s.info.Types[e.Args[0]].IsType()
		if isValue {
			if s.isMove(e.Args[0]) {
				v = s.movedOut(s.lv(e.Args[0]), s.varOfIdent(e.Args[0])) // new(x) moves an owner x in
			} else {
				v = s.materialize(s.exprAs(e.Args[0], elem))
			}
		}
		ptr := s.newBox(types2.WrapOwned, elem, e.Args[1:])
		if isValue {
			s.assign(lvalue{ptr: ptr, T: elem}, v, true)
		}
		return one(scalarValue(ptr, T))

	case "make":
		if _, isChan := unwrap(T).Underlying().(*types2.Chan); isChan {
			return one(s.makeChan(e, T))
		}
		if mapOf(T) != nil {
			return one(s.makeMap(e, T))
		}
		// The checker rejects make of a slice (E2014).
		s.l.unsupportedf(e.Pos(), "make of %s is not supported", T)
		return nil

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
		vals := compat.Make[value](len(srcs))
		nodes := compat.Make[ast.Node](len(srcs))
		for i, a := range srcs {
			if a.e != nil {
				vals[i] = s.materialize(s.printOperand(a.e))
				nodes[i] = a.e
			} else {
				vals[i] = s.materialize(a.v)
				nodes[i] = e
			}
		}
		s.printStatement(b.Name() == "println", len(vals), func(i int, buffered bool) {
			s.printValue(vals[i], unwrap(vals[i].T), nodes[i], buffered)
		})
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
		// An uncounted view of borrowed memory whose bytes nobody has
		// checked: the flags say nothing (strrep.go).
		p := s.scalar(e.Args[0])
		n := s.indexValue(e.Args[1])
		v := s.makeString(p, n, s.constInt(ssa.TI64, strNonASCII), T)
		v.cnt = cntStatic
		return one(v)
	case "StringData":
		p, _ := s.stringParts(s.expr(e.Args[0]))
		return one(scalarValue(p, T))
	}
	s.l.unsupportedf(e.Pos(), "builtin %s is not supported", pu.Box(b.Name()))
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
		fields := compat.Make[*types2.Var](st.NumFields())
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
	// The result is borrowed from one of the operands, each of which stays
	// valid until the end of the statement.
	res := s.temp(T, "minmax")
	s.storeTo(res, s.hold(s.exprAs(args[0], T)), T)
	for _, a := range args[1:] {
		v := s.hold(s.materialize(s.exprAs(a, T)))
		cur := s.loadFrom(res, T)
		c := s.callRuntime("strcmp", compat.Clone([]*ssa.Type{ssa.TI64}), s.stringLeaves(v), s.stringLeaves(cur))[0]
		zero := s.constInt(ssa.TI64, 0)
		var better *ssa.Value
		if isMin {
			better = s.newValue(ssa.OpLess, ssa.TBool, c, zero)
		} else {
			better = s.newValue(ssa.OpLess, ssa.TBool, zero, c)
		}
		s.ifThen(better, func() { s.storeTo(res, v, T) })
	}
	out := s.loadFrom(res, T)
	out.cnt = cntStable
	return out
}

// copyElems copies n elements of type et from src to dst, which may
// overlap, as copy does: each copy of a counted element takes a reference,
// and each element overwritten drops its own (all retains come first, so
// overlapping copies are safe).
func (s *fnState) copyElems(dst, src, n *ssa.Value, et types2.Type) {
	bytes := s.newValue(ssa.OpMul, ssa.TI64, n, s.constInt(ssa.TI64, s.l.sizeof(et)))
	s.staticWriteCheck(dst, bytes)
	if s.l.counted(et) {
		s.retainRange(src, n, et)
		s.releaseRange(dst, n, et)
	}
	s.callRuntime("memmove", nil, []*ssa.Value{s.checkedLen(dst, bytes), s.checkedLen(src, bytes), bytes})
}

// clearSlice clears the slice x refers to, whose elements are of type et. Each
// owner is dropped in index order, and its Drop method runs before its place
// becomes the zero value (section 13.3); other counted elements are released.
// Shared by clear(s) and the deferred clear, which loads its argument from the
// defer record when the function returns (section 12.19).
func (s *fnState) clearSlice(x value, et types2.Type) {
	p, n, _ := s.sliceParts(x)
	s.staticWriteCheck(p, s.newValue(ssa.OpMul, ssa.TI64, n, s.constInt(ssa.TI64, s.l.sizeof(et))))
	if s.l.needsDrop(et) {
		// Each owner is dropped in index order, and its Drop method runs
		// before its place becomes the zero value (section 13.3).
		s.countedLoop(n, nil, func(i *ssa.Value) {
			off := s.newValue(ssa.OpMul, ssa.TI64, i, s.constInt(ssa.TI64, s.l.sizeof(et)))
			q := s.newValue(ssa.OpAddPtr, ssa.TPtr, p, off)
			s.dropAt(q, et)
			s.zero(q, s.l.sizeof(et))
		})
		return
	}
	if s.l.counted(et) {
		// Clearing an element drops what it held.
		s.releaseRange(p, n, et)
	}
	bytes := s.newValue(ssa.OpMul, ssa.TI64, n, s.constInt(ssa.TI64, s.l.sizeof(et)))
	s.callRuntime("memclr", nil, []*ssa.Value{s.checkedLen(p, bytes), bytes})
}

// copySlice copies the first min(len(dst), len(src)) elements of src into dst
// and returns that count. If srcStr, src is a string, not a slice. Shared by
// copy(dst, src) and the deferred copy (section 12.19).
func (s *fnState) copySlice(dst, src value, srcStr bool) *ssa.Value {
	dp, dn, _ := s.sliceParts(dst)
	var sp, sn *ssa.Value
	if srcStr {
		sp, sn = s.stringParts(src)
	} else {
		sp, sn, _ = s.sliceParts(src)
	}
	n := s.minInt(dn, sn)
	et := s.subst(unwrap(dst.T).Underlying().(*types2.Slice).Elem())
	s.copyElems(dp, sp, n, et)
	return n
}

// printOperand evaluates an argument of print or println, giving untyped
// constants their default type.
func (s *fnState) printOperand(a ast.Expr) value {
	T := unwrap(s.typeOf(a))
	if tv := s.info.Types[a]; !tv.Value.IsNil() {
		if b, ok := T.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
			T = defaultType(tv.Value)
		}
	}
	return s.exprAs(a, T)
}

// printStatement emits a print or println statement of n values, whose
// i'th printOne prints. The statement is atomic: the output of concurrent
// statements is never mixed within a line. One that writes more than once
// (anything but print of a single value) takes the print lock between
// printBegin and printEnd, and the runtime gathers its output in a buffer
// that printEnd writes in one call when it fits (lib/pluto/rt, "print
// statements").
func (s *fnState) printStatement(ln bool, n int, printOne func(i int, buffered bool)) {
	if n == 0 && !ln {
		return
	}
	buffered := n > 1 || ln && n > 0
	if buffered {
		s.callRuntime("printBegin", nil)
	}
	for i := 0; i < n; i++ {
		if i > 0 && ln {
			s.callRuntime(printFunc("printSpace", buffered), nil)
		}
		printOne(i, buffered)
	}
	if ln {
		s.callRuntime(printFunc("printNewline", buffered), nil)
	}
	if buffered {
		s.callRuntime("printEnd", nil)
	}
}

// printFunc names the runtime function that prints one piece of a print
// statement: the one that writes at once, or, between printBegin and
// printEnd, the one that appends to the statement's buffer.
func printFunc(name string, buffered bool) string {
	if buffered {
		return compat.Concat("stmt", strings.TrimPrefix(name, "print"))
	}
	return name
}

// printValue prints one value for print or println.
func (s *fnState) printValue(v value, T types2.Type, a ast.Node, buffered bool) {
	if _, named := T.(*types2.Named); named && types2.IsOwner(T) {
		// An owner prints nothing, even over an integer, so that printing
		// never raises the question of a move (language spec section 13.12).
		s.unprintable(a, T)
		return
	}
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		info := u.Info()
		switch {
		case info&types2.IsString != 0:
			leaves := s.stringLeaves(v)
			leaves[0] = s.checkedLen(leaves[0], leaves[1])
			s.callRuntime(printFunc("printString", buffered), nil, leaves)
		case info&types2.IsBoolean != 0:
			s.callRuntime(printFunc("printBool", buffered), nil, []*ssa.Value{v.v})
		case info&types2.IsFloat != 0:
			// The runtime takes the bits, so a float32 is formatted as
			// one (shortest digits for its precision). In kernel mode,
			// passing a float is a float use like any other, allowed
			// only in //pu:fpu functions, as for pluto/fmt.
			if s.f.NoSSE {
				s.floatErrorAt(a.Pos(), v.v.Type, "argument")
			}
			size := v.v.Type.Size
			bits := s.newValue(ssa.OpBitcast, ssa.IntType(size), v.v)
			if size < 8 {
				bits = s.newValue(ssa.OpZeroExt, ssa.TI64, bits)
			}
			s.callRuntime(printFunc("printFloat", buffered), nil, []*ssa.Value{bits, s.constInt(ssa.TI64, 8*size)})
		case info&types2.IsUnsigned != 0:
			s.callRuntime(printFunc("printUint", buffered), nil, []*ssa.Value{s.convertScalar(v.v, T, types2.Typ[types2.Uint64])})
		case info&types2.IsInteger != 0:
			s.callRuntime(printFunc("printInt", buffered), nil, []*ssa.Value{s.convertScalar(v.v, T, types2.Typ[types2.Int64])})
		case u.Kind() == types2.UnsafePointer:
			s.callRuntime(printFunc("printPointer", buffered), nil, []*ssa.Value{v.v})
		}
	case *types2.Pointer, *types2.Signature:
		s.callRuntime(printFunc("printPointer", buffered), nil, []*ssa.Value{v.v})
	case *types2.Wrapper:
		if !u.IsBox() {
			s.unprintable(a, T)
			break
		}
		// owned, shared, synced and weak print their address, as pointers do.
		s.callRuntime(printFunc("printPointer", buffered), nil, []*ssa.Value{s.wordOf(v)})
	default:
		s.unprintable(a, T)
	}
}

// exprText returns the source form of an expression node.
func exprText(n ast.Node) string {
	if e, ok := n.(ast.Expr); ok {
		return types2.ExprString(e)
	}
	return "x"
}

// unprintable reports an argument of print or println of a type they cannot
// write: a type that is not boolean, integer, floating-point, string or an
// address, and any owner type, even over an integer (language spec section
// 13.12).
func (s *fnState) unprintable(a ast.Node, T types2.Type) {
	d := pu.New(diag.Diagnostic{Code: diag.UnprintableArgument, Msg: compat.Sprintf("cannot print a value of type %s", T)})
	d.Text = "print and println write a boolean, an integer (an enum too), a floating-point number, a string, or the address of a pointer or function. They have no form for other types, so a program that prints them gets the same text from every compiler, and they work in a program that has no standard library."
	d.Fix = pu.New(diag.Fix{Intro: "To print a value of another type, use pluto/fmt", Code: "import \"pluto/fmt\"\n\nfmt.Println(x)"})
	if types2.IsOwner(T) {
		d.Text = "An owner cannot be printed, even when its underlying type is an integer: printing it would raise the question of whether the value moves. print and println write a boolean, an integer, a floating-point number, a string, or an address."
		if b, ok := T.Underlying().(*types2.Basic); ok && b.Info()&types2.IsInteger != 0 {
			d.Fix = pu.New(diag.Fix{Intro: "To print the number inside an owner, convert it; the conversion reads the value without moving it", Code: compat.Sprintfb("println(%s(%s))", compat.ArgS(b.Name()), compat.ArgS(exprText(a)))})
			d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "To print other values, use pluto/fmt", Code: "import \"pluto/fmt\"\n\nfmt.Println(x)"})})
		}
	}
	s.l.report(a.Pos(), d)
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

	if w, ok := tu.(*types2.Wrapper); ok && w.IsCounted() {
		// weak[P](p) takes a weak reference (box.go).
		if _, ok := fu.(*types2.Wrapper); ok && w.Kind() == types2.WrapWeak {
			return s.weakFrom(s.expr(arg), to)
		}
		// A counted box made from an unsafe.Pointer adopts the reference
		// the pointer stands for (alloc.Shared and friends): it is a new
		// value that the statement holds until something takes it.
		if b, ok := fu.(*types2.Basic); ok && b.Kind() == types2.UnsafePointer {
			return s.wordTemp(s.scalar(arg), to, "box")
		}
	}

	// string(runes) and string(r) copy, so they allocate (strops.go).
	if isString(to) != isString(from) {
		if v, ok := s.stringConversion(e, to, from); ok {
			return v
		}
		s.l.internalf(e.Pos(), "unexpected conversion from %s to %s", from, to)
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
	if types2.IsRingIterator(fn.Name()) {
		return compat.Clone([]value{s.ringIterator(fn.Name(), w, recv.v, s.typeOf(e))})
	}
	var args []value
	if fn.Name() == "Push" {
		args = compat.Append(args, s.materialize(s.exprAs(e.Args[0], s.subst(w.Elem()))))
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
func (l *Lowerer) ringMethodFunc(fn *types2.Func, w *types2.Wrapper, pos token.Pos, what string) (*ssa.Sym, *types2.Signature) {
	ptr := types2.NewPointer(w)
	obj, _, _ := types2.LookupFieldOrMethod(ptr, false, nil, fn.Name())
	full := obj.(*types2.Func).Type().(*types2.Signature)
	name := compat.Sprintfb("builtin.(*ring[%s,%d]).%s", compat.ArgS(typeString(w.Elem())), compat.ArgI(w.N()), compat.ArgS(fn.Name()))
	if sym := l.funcs[name]; sym != nil {
		return sym, full
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	l.queue = compat.Append(l.queue, pu.New(funcJob{sym: sym, sig: full, build: pu.New(goClosure_ringMethodFunc_1{full: full, fn: fn, l: l, w: w})}))
	return sym, full
}

// ringSlot returns the address of element idx, taken modulo the capacity,
// of the ring of type w that p points at.
func (s *fnState) ringSlot(p *ssa.Value, w *types2.Wrapper, idx *ssa.Value) *ssa.Value {
	et := s.subst(w.Elem())
	align := s.l.alignof(et)
	dataOff := (16 + align - 1) &^ (align - 1)
	i := s.newValue(ssa.OpAnd, ssa.TI64, idx, s.constInt(ssa.TI64, w.N()-1))
	return s.elemAddr(s.offPtr(p, dataOff), i, et)
}

// ringIterator lowers a call of ring method name (All or Values) on the ring
// of type w that p points at: the iterator's closure record, built in this
// frame. T is the iterator's func type.
func (s *fnState) ringIterator(name string, w *types2.Wrapper, p *ssa.Value, T types2.Type) value {
	T = s.subst(T)
	cw := types2.NewWrapper(types2.WrapRing, s.subst(w.Elem()), w.N())
	sym := s.l.ringIterFunc(name, cw, T.Underlying().(*types2.Signature))
	return scalarValue(s.closureRecord(sym, []*ssa.Value{p}), T)
}

// ringIterFunc returns the function of the iterator that ring method name
// (All or Values) of ring type w returns, of func type sig. The iterator is
// a closure record holding the code pointer and the address of the ring,
// built where All or Values is called, or, when ringMethodFunc's function
// is called, in the region its caller provides (language spec section 7.17).
//
// The loop reads the ring as it goes. It keeps a position in the ring's
// push count: each step skips to the oldest element if the body popped the
// position's element, and stops once the position reaches the push count.
func (l *Lowerer) ringIterFunc(name string, w *types2.Wrapper, sig *types2.Signature) *ssa.Sym {
	symName := compat.Sprintfb("builtin.(*ring[%s,%d]).%s.func1", compat.ArgS(typeString(w.Elem())), compat.ArgI(w.N()), compat.ArgS(name))
	if sym := l.funcs[symName]; sym != nil {
		return sym
	}
	sym := l.out.Sym(symName, ssa.SymFunc)
	l.funcs[symName] = sym
	l.funcValues[sym] = sig
	l.queue = compat.Append(l.queue, pu.New(funcJob{sym: sym, sig: sig, build: pu.New(goClosure_ringIterFunc_1{sig: sig, w: w, name: name})}))
	return sym
}

// ringOp expands ring method name on the ring that p points at, with the
// arguments already evaluated.
func (s *fnState) ringOp(name string, p *ssa.Value, w *types2.Wrapper, args []value, pos token.Pos) []value {
	et := s.subst(w.Elem())
	if name == "Reset" || name == "Push" || name == "Pop" {
		s.staticWriteType(p, w)
	}
	// A ring of counted values keeps every slot outside [tail, head) zero,
	// so dropping or copying the whole ring counts only the live ones.
	counted := s.l.counted(et)
	n := w.N()
	headPtr := p
	tailPtr := s.offPtr(p, 8)
	slot := func(idx *ssa.Value) *ssa.Value { return s.ringSlot(p, w, idx) }
	boolT := types2.Typ[types2.Bool]
	intT := types2.Typ[types2.Int]

	switch name {
	case "Len":
		d := s.newValue(ssa.OpSub, ssa.TI64, s.load(ssa.TI64, headPtr), s.load(ssa.TI64, tailPtr))
		return compat.Clone([]value{scalarValue(d, intT)})
	case "Cap":
		return compat.Clone([]value{scalarValue(s.constInt(ssa.TI64, n), intT)})
	case "Full":
		d := s.newValue(ssa.OpSub, ssa.TI64, s.load(ssa.TI64, headPtr), s.load(ssa.TI64, tailPtr))
		return compat.Clone([]value{scalarValue(s.newValue(ssa.OpEq, ssa.TBool, d, s.constInt(ssa.TI64, n)), boolT)})
	case "Empty":
		eq := s.newValue(ssa.OpEq, ssa.TBool, s.load(ssa.TI64, headPtr), s.load(ssa.TI64, tailPtr))
		return compat.Clone([]value{scalarValue(eq, boolT)})
	case "Reset":
		if s.l.needsDrop(et) {
			// Drop the owners it holds (section 7.17), oldest first.
			tail := s.load(ssa.TI64, tailPtr)
			live := s.newValue(ssa.OpSub, ssa.TI64, s.load(ssa.TI64, headPtr), tail)
			s.countedLoop(live, nil, func(i *ssa.Value) {
				s.destroyAt(slot(s.newValue(ssa.OpAdd, ssa.TI64, tail, i)), et)
			})
		} else if counted {
			// Drop the live elements and leave their slots zero.
			data := s.ringSlot(p, w, s.constInt(ssa.TI64, 0))
			s.releaseAt(data, types2.NewArray(et, n))
			s.zero(data, n*s.l.sizeof(et))
		}
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
			s.assign(lvalue{ptr: slot(head), T: et}, v, false)
			s.store(ssa.TI64, headPtr, s.newValue(ssa.OpAdd, ssa.TI64, head, s.constInt(ssa.TI64, 1)))
			s.store(ssa.TBool, res, s.constBool(true))
		})
		return compat.Clone([]value{scalarValue(s.load(ssa.TBool, res), boolT)})
	case "Peek":
		if types2.IsOwner(et) {
			// A view of the oldest owner element (section 7.17): its
			// address, or nil when the ring is empty.
			pt := types2.NewPointer(et)
			out := s.temp(pt, "view")
			s.store(ssa.TPtr, out, s.constInt(ssa.TPtr, 0))
			res := s.temp(boolT, "ok")
			s.store(ssa.TBool, res, s.constBool(false))
			tail := s.load(ssa.TI64, tailPtr)
			nonEmpty := s.newValue(ssa.OpNe, ssa.TBool, s.load(ssa.TI64, headPtr), tail)
			s.ifThen(nonEmpty, func() {
				s.store(ssa.TPtr, out, slot(tail))
				s.store(ssa.TBool, res, s.constBool(true))
			})
			return compat.Clone([]value{scalarValue(s.load(ssa.TPtr, out), pt), scalarValue(s.load(ssa.TBool, res), boolT)})
		}
		fallthrough
	case "Pop":
		out := s.temp(et, "elem")
		s.zero(out, s.l.sizeof(et))
		res := s.temp(boolT, "ok")
		s.store(ssa.TBool, res, s.constBool(false))
		tail := s.load(ssa.TI64, tailPtr)
		nonEmpty := s.newValue(ssa.OpNe, ssa.TBool, s.load(ssa.TI64, headPtr), tail)
		s.ifThen(nonEmpty, func() {
			switch {
			case counted && name == "Pop":
				// The element's references move out with it, and its
				// slot is left zero.
				s.move(out, slot(tail), s.l.sizeof(et))
				s.zero(slot(tail), s.l.sizeof(et))
			case counted:
				s.assign(lvalue{ptr: out, T: et}, s.loadFrom(slot(tail), et), true)
			default:
				s.storeTo(out, s.loadFrom(slot(tail), et), et)
			}
			if name == "Pop" {
				s.store(ssa.TI64, tailPtr, s.newValue(ssa.OpAdd, ssa.TI64, tail, s.constInt(ssa.TI64, 1)))
			}
			s.store(ssa.TBool, res, s.constBool(true))
		})
		if counted {
			return compat.Clone([]value{s.ownTemp(out, et), scalarValue(s.load(ssa.TBool, res), boolT)})
		}
		return compat.Clone([]value{s.loadFrom(out, et), scalarValue(s.load(ssa.TBool, res), boolT)})
	}
	s.l.internalf(pos, "unknown ring method %s", pu.Box(name))
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

// varOfIdent returns the variable the expression names, or nil.
func (s *fnState) varOfIdent(e ast.Expr) *types2.Var {
	if id, ok := ast.Unparen(e).(*ast.Ident); ok {
		v, _ := s.info.Uses[id].(*types2.Var)
		return v
	}
	return nil
}

// goClosure_ringMethodFunc_1 is the function literal at builtin.go:658, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_ringMethodFunc_1 struct {
	full *types2.Signature
	fn   *types2.Func
	l    *Lowerer
	w    *types2.Wrapper
}

func (goSelf *goClosure_ringMethodFunc_1) Call(s *fnState) {
	in := s.incomingArgs(goSelf.full)
	if types2.IsRingIterator(goSelf.fn.Name()) {
		// The iterator's closure record, in the region the caller
		// provides (regions.go).
		T := goSelf.full.Results().At(0).Type()
		iter := goSelf.l.ringIterFunc(goSelf.fn.Name(), goSelf.w, T.Underlying().(*types2.Signature))
		rec := s.regionRecord(16, true)
		s.storeRecord(rec, iter, []*ssa.Value{in[0].v})
		s.returnValues([]value{scalarValue(rec, T)})
		return
	}
	s.returnValues(s.ringOp(goSelf.fn.Name(), in[0].v, goSelf.w, in[1:], token.NoPos))
}

// goClosure_ringIterFunc_1 is the function literal at builtin.go:712, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_ringIterFunc_1 struct {
	sig  *types2.Signature
	w    *types2.Wrapper
	name string
}

func (goSelf *goClosure_ringIterFunc_1) Call(s *fnState) {
	ctx := s.newValue(ssa.OpClosurePtr, ssa.TPtr)
	in := s.incomingArgs(goSelf.sig)
	yield := in[0].v
	yieldSig := goSelf.sig.Params().At(0).Type().Underlying().(*types2.Signature)
	et := goSelf.w.Elem()
	intT := types2.Typ[types2.Int]
	p := s.load(ssa.TPtr, s.offPtr(ctx, 8))
	headPtr, tailPtr := p, s.offPtr(p, 8)

	pos := s.temp(types2.Typ[types2.Uint64], "pos")
	s.store(ssa.TI64, pos, s.load(ssa.TI64, tailPtr))
	head := s.newBlock(ssa.BlockInvalid)
	body := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	s.endPlain(head)
	s.startBlock(head)
	tail := s.load(ssa.TI64, tailPtr)
	s.ifThen(s.newValue(ssa.OpLessu, ssa.TBool, s.load(ssa.TI64, pos), tail), func() {
		s.store(ssa.TI64, pos, tail)
	})
	cur := s.load(ssa.TI64, pos)
	s.endIf(s.newValue(ssa.OpLessu, ssa.TBool, cur, s.load(ssa.TI64, headPtr)), body, done)
	s.seal(body)
	s.startBlock(body)
	s.store(ssa.TI64, pos, s.newValue(ssa.OpAdd, ssa.TI64, cur, s.constInt(ssa.TI64, 1)))
	var args []value
	if goSelf.name == "All" {
		idx := s.newValue(ssa.OpSub, ssa.TI64, cur, s.load(ssa.TI64, tailPtr))
		args = compat.Append(args, scalarValue(idx, intT))
	}
	// The body may pop or overwrite the element it is given, so a
	// counted one is held for the call (strcount.go), in an entry of
	// its own that releases it each time round.
	step := s.pushCleanup(nil, false)
	if types2.IsOwner(et) {
		// A view of an owner element (section 7.17): its address.
		args = compat.Append(args, scalarValue(s.ringSlot(p, goSelf.w, cur), types2.NewPointer(et)))
	} else {
		args = compat.Append(args, s.hold(s.loadFrom(s.ringSlot(p, goSelf.w, cur), et)))
	}
	code := s.load(ssa.TPtr, yield)
	res := s.emitCall(callee{code: code, ctx: yield}, yieldSig, nil, args, token.NoPos)
	more := res[0].v
	s.popCleanup(step)
	s.endIf(more, head, done)
	s.seal(head)
	s.seal(done)
	s.startBlock(done)
	s.returnValues(nil)
}
