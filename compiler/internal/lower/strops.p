// Translated by go2pu from internal/lower/strops.go at the go-cutover tag; edited by hand since.

package lower

import (
	"compiler/compat"
	"compiler/internal/constant"
	"compiler/internal/diag"
	"compiler/internal/load"
	"compiler/internal/ssa"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// String operations that build new strings: concatenation and the
// allocating conversions. Each calls the runtime (pluto/rt/str.p), which
// returns the new string's three words holding one reference; lowering
// makes it a statement temporary.

// concat lowers a chain of string additions, a + b + c, as one runtime call
// that copies every operand into one new buffer, as Go does.
func (s *fnState) concat(e *ast.BinaryExpr, T types2.Type) value {
	var operands []ast.Expr
	var collect func(x ast.Expr)
	collect = func(x ast.Expr) {
		x = ast.Unparen(x)
		if b, ok := x.(*ast.BinaryExpr); ok && b.Op == token.ADD && s.info.Types[b].Value.IsNil() && isString(unwrap(s.typeOf(b))) {
			collect(b.X)
			collect(b.Y)
			return
		}
		operands = compat.Append(operands, x)
	}
	collect(e)
	return s.concatValues(s.evalOperands(operands, T), T)
}

// evalOperands evaluates string operands left to right, holding each one
// that a later operand's calls could change.
func (s *fnState) evalOperands(operands []ast.Expr, T types2.Type) []value {
	vals := compat.Make[value](len(operands))
	for i, x := range operands {
		vals[i] = s.holdAcross(s.exprAs(x, unwrap(T)), operands[i+1:]...)
	}
	return vals
}

// concatValues concatenates string values already evaluated.
func (s *fnState) concatValues(vals []value, T types2.Type) value {
	strT := types2.Typ[types2.String]
	arrT := types2.NewArray(strT, int64(len(vals)))
	arr := s.temp(arrT, "concat")
	for i, v := range vals {
		// The operands are borrowed for the call: copies of their words.
		s.move(s.offPtr(arr, int64(i)*strSize), v.v, strSize)
	}
	n := s.constInt(ssa.TI64, int64(len(vals)))
	res := s.callRuntime("concatStrings", compat.Clone([]*ssa.Type{ssa.TPtr, ssa.TI64, ssa.TI64}), []*ssa.Value{arr, n, n})
	return s.newString(res, T)
}

// newString makes a statement temporary of a string the runtime built and
// returned as its words, holding one reference.
func (s *fnState) newString(words []*ssa.Value, T types2.Type) value {
	v := s.makeString(words[0], words[1], words[2], T)
	return s.ownTemp(v.v, T)
}

// concatAssign lowers s += t on a string place.
func (s *fnState) concatAssign(lv lvalue, rhs ast.Expr, pos token.Pos) {
	T := unwrap(lv.T)
	old := s.holdAcross(s.loadLV(lv), rhs)
	y := s.exprAs(rhs, T)
	s.storeCounted(lv, s.concatValues([]value{old, y}, T), false)
}

// stringConversion lowers a conversion to a string that copies, and so
// allocates: string(runes), and string(r) for a rune that is not a constant.
// It reports false for any other conversion. (The checker rejects the
// conversions of a string to a byte or rune slice, and of a byte slice or a
// non-rune integer to a string; language spec section 11.24.)
func (s *fnState) stringConversion(e *ast.CallExpr, to, from types2.Type) (value, bool) {
	if !isString(to) || isString(from) {
		return value{}, false
	}
	fu := unwrap(from).Underlying()
	if sl, ok := fu.(*types2.Slice); ok {
		if b, ok := unwrap(sl.Elem()).Underlying().(*types2.Basic); ok && b.Kind() == types2.Rune {
			p, n, _ := s.sliceParts(s.expr(e.Args[0]))
			res := s.callRuntime("stringFromRunes", compat.Clone([]*ssa.Type{ssa.TPtr, ssa.TI64, ssa.TI64}), []*ssa.Value{p, n})
			return s.newString(res, to), true
		}
	}
	if b, ok := fu.(*types2.Basic); ok && b.Kind() == types2.Int32 {
		r := s.convertScalar(s.scalar(e.Args[0]), from, types2.Typ[types2.Rune])
		res := s.callRuntime("stringFromRune", compat.Clone([]*ssa.Type{ssa.TPtr, ssa.TI64, ssa.TI64}), []*ssa.Value{r})
		return s.newString(res, to), true
	}
	return value{}, false
}

// runeBoundaryCheck makes s[lo:hi] panic unless both bounds are at the
// start of a rune or at the end (language spec section 18.8). A string
// whose flags say it is ASCII needs no check, so the test of the bytes is a
// call made only for others. A bound that is the constant 0, or left out,
// needs none either.
func (s *fnState) runeBoundaryCheck(e *ast.SliceExpr, base, length, lo, hi, ctl *ssa.Value) {
	if tv := s.info.Types[e.X]; !tv.Value.IsNil() && strFlags(constant.StringVal(tv.Value)) == 0 {
		return // an ASCII constant
	}
	if e.Low == nil || isZeroConst(s.info, e.Low) {
		lo = length
	}
	if e.High == nil {
		hi = length
	}
	if lo == length && hi == length {
		return
	}
	nonASCII := s.newValue(ssa.OpNe, ssa.TBool, s.newValue(ssa.OpAnd, ssa.TI64, ctl, s.constInt(ssa.TI64, strNonASCII)), s.constInt(ssa.TI64, 0))
	s.ifThen(nonASCII, func() {
		s.callRuntime("checkRuneBounds", nil, []*ssa.Value{base, length, lo, hi})
	})
}

func isZeroConst(info *types2.Info, e ast.Expr) bool {
	c, ok := constIndex(info, e)
	return ok && c == 0
}

// stringsSlice expands a call of strings.Slice or strings.SliceFrom as the
// slice of a string it stands for: bounds checked against the length, rune
// boundaries checked unless the string is known to be ASCII, and a result
// that shares the buffer and its count (language spec sections 7.3, 11.12
// and 18.8). The type checker rejects the slice expression itself (E2019),
// so this is the only way a program cuts a string.
func (s *fnState) stringsSlice(fn *types2.Func, e *ast.CallExpr) ([]value, bool) {
	if fn.Pkg() == nil || fn.Pkg().Path() != "pluto/strings" || fn.Type().(*types2.Signature).Recv() != nil {
		return nil, false
	}
	var se *ast.SliceExpr
	switch {
	case fn.Name() == "Slice" && len(e.Args) == 3:
		se = &ast.SliceExpr{X: e.Args[0], Low: e.Args[1], High: e.Args[2]}
	case fn.Name() == "SliceFrom" && len(e.Args) == 2:
		se = &ast.SliceExpr{X: e.Args[0], Low: e.Args[1]}
	default:
		return nil, false
	}
	return compat.Clone([]value{s.sliceExpr(se, s.typeOf(e))}), true
}

// countIntrinsics are the generic functions of pluto/rt that give library
// containers the compiler's string drop glue (pluto/rt/str.p).
var countIntrinsics = map[string]bool{
	"ReleaseValues": true, "RetainValues": true, "MoveValues": true, "HoldsStrings": true,
	"ZeroValues": true, "NeedsDrop": true, "CopyValue": true, "MustCopy": true, "InitValue": true, "CopyValues": true, "ViewOf": true,
}

// countIntrinsic expands a call of one of the countIntrinsics.
func (s *fnState) countIntrinsic(fn *types2.Func, e *ast.CallExpr, id *ast.Ident) ([]value, bool) {
	if fn.Pkg() == nil || fn.Pkg().Path() != "pluto/rt" {
		return nil, false
	}
	switch fn.Name() {
	case "SliceBytewise":
		// s[lo:hi] with its bounds checked but not that they are rune
		// boundaries.
		se := &ast.SliceExpr{X: e.Args[0], Low: e.Args[1], High: e.Args[2]}
		was := s.bytewise
		s.bytewise = true
		v := s.sliceExpr(se, types2.Typ[types2.String])
		s.bytewise = was
		return compat.Clone([]value{v}), true
	case "StringControl", "KnownASCII":
		// Read the control word without taking the argument's address.
		ctl := s.stringCtl(s.exprAs(e.Args[0], types2.Typ[types2.String]))
		if fn.Name() == "StringControl" {
			return compat.Clone([]value{scalarValue(ctl, types2.Typ[types2.Uintptr])}), true
		}
		bit := s.newValue(ssa.OpAnd, ssa.TI64, ctl, s.constInt(ssa.TI64, strNonASCII))
		return compat.Clone([]value{scalarValue(s.newValue(ssa.OpEq, ssa.TBool, bit, s.constInt(ssa.TI64, 0)), types2.Typ[types2.Bool])}), true
	}
	if !countIntrinsics[fn.Name()] {
		return nil, false
	}
	targs := s.instanceArgs(id)
	if len(targs) != 1 {
		s.l.internalf(e.Pos(), "rt.%s is not instantiated", pu.Box(fn.Name()))
		return nil, true
	}
	T := targs[0]
	intT := types2.Typ[types2.Int]
	switch fn.Name() {
	case "HoldsStrings":
		return compat.Clone([]value{scalarValue(s.constBool(s.l.counted(T)), types2.Typ[types2.Bool])}), true
	case "NeedsDrop":
		return compat.Clone([]value{scalarValue(s.constBool(s.l.counted(T) || s.l.needsDrop(T)), types2.Typ[types2.Bool])}), true
	case "CopyValue", "ViewOf":
		return s.elementCopy(fn, e, T), true
	case "MustCopy":
		if types2.IsOwner(T) {
			s.reportOwnerCopy(e, T)
		}
		return nil, true
	case "InitValue":
		ptr := s.checked(s.expr(e.Args[0]).v)
		s.assign(lvalue{ptr: ptr, T: T}, s.exprAs(e.Args[1], T), true)
		return nil, true
	case "CopyValues":
		if types2.IsOwner(T) {
			s.reportOwnerCopy(e, T)
			return nil, true
		}
		dst := s.scalar(e.Args[0])
		src := s.scalar(e.Args[1])
		n := s.exprAs(e.Args[2], intT).v
		bytes := s.newValue(ssa.OpMul, ssa.TI64, n, s.constInt(ssa.TI64, s.l.sizeof(T)))
		s.callRuntime("memmove", nil, []*ssa.Value{s.checkedLen(dst, bytes), s.checkedLen(src, bytes), bytes})
		if s.l.counted(T) {
			s.retainRange(dst, n, T)
		}
		return nil, true
	case "ZeroValues":
		p := s.scalar(e.Args[0])
		n := s.exprAs(e.Args[1], intT).v
		bytes := s.newValue(ssa.OpMul, ssa.TI64, n, s.constInt(ssa.TI64, s.l.sizeof(T)))
		s.callRuntime("memclr", nil, []*ssa.Value{s.checkedLen(p, bytes), bytes})
		return nil, true
	case "MoveValues":
		dst := s.scalar(e.Args[0])
		src := s.scalar(e.Args[1])
		n := s.exprAs(e.Args[2], intT).v
		bytes := s.newValue(ssa.OpMul, ssa.TI64, n, s.constInt(ssa.TI64, s.l.sizeof(T)))
		s.callRuntime("memmove", nil, []*ssa.Value{dst, src, bytes})
		return nil, true
	}
	p := s.scalar(e.Args[0])
	n := s.exprAs(e.Args[1], intT).v
	if s.l.counted(T) {
		if fn.Name() == "ReleaseValues" {
			s.releaseRange(p, n, T)
		} else {
			s.retainRange(p, n, T)
		}
	}
	return nil, true
}

// elementCopy lowers rt.CopyValue and rt.ViewOf, which give a container's
// element out of the container's storage at p: a copy of the T there,
// counted as a copy is. rt.ViewOf yields p itself, a view, when T is an
// owner. An owner has no copy, so rt.CopyValue of one is an error: the
// method of the container that called it cannot be used for owners.
func (s *fnState) elementCopy(fn *types2.Func, e *ast.CallExpr, T types2.Type) []value {
	p := s.expr(e.Args[0])
	if types2.IsOwner(T) {
		if fn.Name() == "ViewOf" {
			return compat.Clone([]value{p})
		}
		s.reportOwnerCopy(e, T)
		return compat.Clone([]value{s.zeroValue(T)})
	}
	return compat.Clone([]value{s.loadFrom(s.checked(p.v), T)})
}

// reportOwnerCopy reports a container method that copies elements, called
// for a container of owners.
// The copy is in the library, so the error is reported at the call in the
// program that led to it, found by following the first caller of each
// library function back out of the library.
func (s *fnState) reportOwnerCopy(e *ast.CallExpr, T types2.Type) {
	var from *ssa.Sym
	if s.job != nil {
		from = s.job.sym
	}
	at, method := s.l.programCall(from, e.Pos())
	if s.l.ownerCopyReported[at] {
		return
	}
	s.l.ownerCopyReported[at] = true
	d := pu.New(diag.Diagnostic{Code: diag.CopyOwners, Msg: compat.Sprintf("cannot copy an element of type %s: it is an owner", T)})
	d.Text = "The container method that was called gives an element out by value. For an owner that would be a second owner of what the element owns, and both would release it."
	if method != nil {
		d.Text = compat.Concat("The container method ", method.Name(), " gives an element out by value. For an owner that would be a second owner of what the element owns, and both would release it.")
	}
	if isMapMethod(method) {
		d.Fix = pu.New(diag.Fix{Intro: "To look at a value, use a method that returns a pointer to it", Code: "p := m.Ptr(k)"})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "To move a value out, use a method that removes it", Code: "x, ok := m.Remove(k)"})})
	} else {
		d.Fix = pu.New(diag.Fix{Intro: "To look at an element, use a method that returns a pointer to it", Code: "p := v.Ptr(i)"})
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "To move an element out, use a method that removes it", Code: "x := v.Pop()"})})
	}
	s.l.report(at, d)
}

// checkOwnerViews reports an instance of generic code that the compiler
// cannot compile yet. The code ranges over a container of an owner type
// parameter T and sees the elements as views, *T (spec sections 8.7,
// 12.10). Made with a type argument that is not an owner, the instance
// would call the container's iterator for a value type, which gives the
// elements by value, and the program would be wrong. Keeping the views in
// such an instance is roadmap item 145.
func (l *Lowerer) checkOwnerViews(job *funcJob) {
	m := job.tmap
	if m == nil || job.decl == nil {
		return
	}
	for i, tp := range m.tparams {
		if !types2.OwnerViewTypeParam(tp) || types2.IsOwner(m.targs[i]) {
			continue
		}
		at := job.decl.Pos()
		if c, ok := l.firstCaller[job.sym]; ok {
			at, _ = l.programCall(c.from, c.pos)
		}
		name := job.decl.Name.Name
		if fn := l.symFuncs[job.sym]; fn != nil {
			name = fn.Name()
		}
		var sb compat.Builder
		for j, t := range m.targs {
			if j > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(compat.Sprintf("%s", t))
		}
		inst := compat.Concat(name, "[", sb.String(), "]")
		targ := compat.Sprintf("%s", m.targs[i])
		d := pu.New(diag.Diagnostic{Code: diag.NotYetSupported, Msg: compat.Concat("cannot compile ", inst, " yet: ", tp.Obj().Name(), " is an owner type parameter and ", targ, " is not an owner")})
		d.Text = compat.Concat("The code of ", name, " ranges over a container of ", tp.Obj().Name(), " and sees its elements as views of type *", tp.Obj().Name(), ", because ", tp.Obj().Name(), " may be an owner. With ", targ, ", which is not an owner, the container gives its elements by value, and this compiler does not yet keep the views in such an instance.")
		d.Fix = pu.New(diag.Fix{Intro: "Range over the indexes and take a pointer to each element, which works for both kinds of type", Code: "for i := range v.Len() {\n\tp := v.Ptr(i)\n}"})
		l.report(at, d)
		return
	}
}

// isMapMethod reports whether fn is a method of hash.Map.
func isMapMethod(fn *types2.Func) bool {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "pluto/hash" {
		return false
	}
	recv := fn.Type().(*types2.Signature).Recv()
	if recv == nil {
		return false
	}
	T := recv.Type()
	if p, ok := T.(*types2.Pointer); ok {
		T = p.Elem()
	}
	n, ok := types2.Unalias(T).(*types2.Named)
	return ok && n.Obj().Name() == "Map"
}

// callerSite is the first direct call of a function that lowering met.
type callerSite struct {
	from *ssa.Sym
	pos  token.Pos
}

// noteCaller records the first direct call of callee, made at pos in from.
func (l *Lowerer) noteCaller(callee, from *ssa.Sym, pos token.Pos) {
	if from == nil || !pos.IsValid() {
		return
	}
	if _, ok := l.firstCaller[callee]; ok {
		return
	}
	l.firstCaller[callee] = callerSite{from: from, pos: pos}
}

// programCall follows the first callers of sym, starting from pos in its
// body, while the position is in the standard library. It returns the first
// position outside it and the function called there, or pos and nil when
// the chain ends in the library (a call through a function value, or the
// library's own code).
func (l *Lowerer) programCall(sym *ssa.Sym, pos token.Pos) (token.Pos, *types2.Func) {
	at := pos
	for i := 0; i < 64; i++ {
		if sym == nil || !l.inStdlib(at) {
			break
		}
		c, ok := l.firstCaller[sym]
		if !ok {
			break
		}
		if !l.inStdlib(c.pos) {
			return c.pos, l.symFuncs[sym]
		}
		sym, at = c.from, c.pos
	}
	return pos, nil
}

// inStdlib reports whether pos is in a file of the standard library.
func (l *Lowerer) inStdlib(pos token.Pos) bool {
	return strings.HasPrefix(l.fset.Position(pos).Filename, load.StdPrefix)
}
