package eir

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Expressions. An expression yields a register holding its value, or 0
// when its type holds no pointers (and for constants and nil). Its calls
// are built whatever its type.

// typeOf returns the type of an expression.
func (fb *fnb) typeOf(e ast.Expr) types2.Type {
	if tv, ok := fb.b.types[e]; ok && tv.Type != nil {
		return tv.Type
	}
	if id, ok := e.(*ast.Ident); ok {
		if obj := fb.b.uses[id]; obj != nil {
			return obj.Type()
		}
		if obj := fb.b.defs[id]; obj != nil {
			return obj.Type()
		}
	}
	return nil
}

// exprAs builds e and converts it to T as an assignment would.
func (fb *fnb) exprAs(e ast.Expr, T types2.Type) Reg {
	tv := fb.b.types[e]
	if !tv.Value.IsNil() {
		if T != nil && isInterface(T) {
			from := fb.typeOf(e)
			if b, ok := from.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
				from = types2.Default(from)
			}
			return fb.conv(fb.constReg(from), from, T, e.Pos())
		}
		return fb.constReg(fb.typeOf(e))
	}
	var r Reg
	if from := fb.typeOf(e); T != nil && isBox(from) && isInterface(unwrap(T)) && !types2.IsOwner(T) && (!types2.IsCountedError(T) || !isCountedBox(from)) {
		// An interface that does not own lends the box it holds: its data
		// word is a view of the box's object (section 18.7). (A counted
		// error takes a reference of its own to a shared or synced box.)
		r = fb.pointee(e)
	} else {
		r = fb.expr(e)
	}
	if from := fb.typeOf(e); isInterface(from) && T != nil && isInterface(T) && lendsError(from, T) {
		r = fb.lendView(r) // borrow.go
	}
	return fb.convertReg(r, fb.typeOf(e), T, e.Pos())
}

// constReg is a constant's value: no pointer, a string's included. A string
// constant's bytes are static and read-only: nothing can be stored in them,
// they hold no pointer to load, and they outlive everything, so the
// analysis has nothing to follow (a pointer to them would only be a heap
// destination a widened store could reach, which no store can).
func (fb *fnb) constReg(T types2.Type) Reg {
	return 0
}

// convertReg converts r, of type from, for assignment to a place of type
// to: a concrete value becomes an interface value.
func (fb *fnb) convertReg(r Reg, from, to types2.Type, pos token.Pos) Reg {
	if to == nil || from == nil {
		return r
	}
	to = unwrap(to)
	if isInterface(to) && !isInterface(from) {
		if b, ok := from.Underlying().(*types2.Basic); ok && b.Kind() == types2.UntypedNil {
			return 0
		}
		return fb.conv(r, from, to, pos)
	}
	return r
}

// conv converts r, of concrete type from, to interface I.
func (fb *fnb) conv(r Reg, from, I types2.Type, pos token.Pos) Reg {
	if from == nil {
		return r
	}
	if isInterface(from) {
		fb.b.noteAsserted(I) // an itab found at run time (convI2I)
		return r
	}
	from = unwrap(from)
	if b, ok := from.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
		if b.Kind() == types2.UntypedNil {
			return 0
		}
		from = types2.Default(from)
	}
	fb.f.Facts.Converted = append(fb.f.Facts.Converted, from)
	fb.noteItab(from, I)
	fb.b.describe(from)
	in := Instr{Op: OpConv, X: r, Type: from, To: I, Obj: NoObj, Pos: pos}
	if !fb.isDirect(from) && !zeroSized(from) {
		// (A box of no bytes holds nothing to read once its frame is
		// gone.)
		owner, o := fb.keptObj(Object{Kind: ObjBox, Type: from, Name: "interface box", Pos: pos})
		if owner != fb {
			d := fb.fillKept(owner, o, r, from, nil)
			if isTypeParam(from) {
				return fb.either(d, r)
			}
			return d
		}
		in.Obj = o
	}
	d := fb.emitDst(in)
	if in.Obj != NoObj && len(fb.f.ArenaMem) > 0 && releasesCounted(from) {
		fb.sink(SinkBoxCopy, r, 0, pos) // arena.go
	}
	if isTypeParam(from) {
		// The instance decides: a direct type is its own data word.
		return fb.either(d, r)
	}
	return d
}

// isDirect reports whether a value of T is its own interface data word
// (internal/lower iface.go isDirect).
func (fb *fnb) isDirect(T types2.Type) bool {
	T = unwrap(T)
	if isTypeParam(T) {
		return false
	}
	return types2.PointerShaped(T) || isScalar(T)
}

// zeroSized reports whether a value of T has no bytes: an empty struct, or
// an array of none, or of zero-sized elements, whatever the instance.
func zeroSized(T types2.Type) bool {
	if T == nil || isTypeParam(T) {
		return false
	}
	switch u := unwrap(T).Underlying().(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if !zeroSized(u.Field(i).Type()) {
				return false
			}
		}
		return true
	case *types2.Array:
		return u.Len() == 0 || zeroSized(u.Elem())
	}
	return false
}

// isScalar reports whether T is a basic type other than a string, which
// fits a word.
func isScalar(T types2.Type) bool {
	if p, ok := T.Underlying().(*types2.Packed); ok {
		// A packed value of one, two, four or eight bytes is held in the
		// data word (section 7.9; lower/layout.go scalarType).
		switch amd64Sizes.Sizeof(p) {
		case 1, 2, 4, 8:
			return true
		}
		return false
	}
	b, ok := T.Underlying().(*types2.Basic)
	return ok && b.Info()&types2.IsString == 0 && b.Kind() != types2.Complex128
}

// noteItab adds the adapters of the itab of from for interface I: the
// functions an interface call on a value converted from from may reach.
func (fb *fnb) noteItab(from, I types2.Type) {
	iface, ok := I.Underlying().(*types2.Interface)
	if !ok {
		return
	}
	if containsTypeParam(from) {
		// Generic code: each instance's type has the itab, as finishItabs
		// makes for the instances' types (itabs.go).
		fb.b.noteGenericConv(I)
		return
	}
	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		fb.b.synth(Synth{Kind: SynthItabAdapter, Recv: from, Method: m})
	}
}

// expr builds an expression and returns its value.
func (fb *fnb) expr(e ast.Expr) Reg {
	saved := fb.pos
	if p := e.Pos(); p.IsValid() {
		fb.pos = p
	}
	defer func() { fb.pos = saved }()
	tv := fb.b.types[e]
	if !tv.Value.IsNil() {
		return fb.constReg(tv.Type)
	}
	T := fb.typeOf(e)
	if fb.b.moves.Temps[e] {
		// An owner nothing takes: dropped when the statement ends.
		r := fb.expr0(e, T)
		return fb.ownerTemp(r, T)
	}
	return fb.expr0(e, T)
}

func (fb *fnb) expr0(e ast.Expr, T types2.Type) Reg {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return fb.expr(e.X)
	case *ast.Ident:
		switch obj := fb.b.uses[e].(type) {
		case *types2.Var:
			return fb.varValue(obj)
		case *types2.Func:
			return fb.funcValue(obj, e)
		case *types2.Nil:
			return 0
		case *types2.Const:
			return fb.constReg(T)
		}
		if e.Name == "_" {
			return 0
		}
		fb.b.errorf(e.Pos(), "unexpected identifier %s", e.Name)
		return 0
	case *ast.BasicLit:
		return fb.constReg(T)
	case *ast.FuncLit:
		r, _ := fb.funcLit(e)
		return r
	case *ast.CompositeLit:
		return fb.compositeLit(e, T)
	case *ast.SelectorExpr:
		return fb.selector(e, T)
	case *ast.IndexExpr, *ast.IndexListExpr:
		var x ast.Expr
		switch ix := e.(type) {
		case *ast.IndexExpr:
			x = ix.X
		case *ast.IndexListExpr:
			x = ix.X
		}
		if signatureOf(fb.typeOf(x)) != nil && !fb.b.types[x].IsType() {
			// A generic function or method instantiated as a value.
			if se, ok := ast.Unparen(x).(*ast.SelectorExpr); ok && fb.b.selections[se] != nil {
				return fb.selector(se, T)
			}
			if id := identOf(x); id != nil {
				if fn, ok := fb.b.uses[id].(*types2.Func); ok {
					return fb.funcValue(fn, id)
				}
			}
		}
		ix := e.(*ast.IndexExpr)
		if isString(fb.typeOf(ix.X)) {
			fb.expr(ix.X)
			fb.expr(ix.Index)
			return 0
		}
		return fb.loadLV(fb.lvalue(ix))
	case *ast.SliceExpr:
		return fb.sliceExpr(e)
	case *ast.StarExpr:
		return fb.loadLV(fb.lvalue(e))
	case *ast.UnaryExpr:
		switch e.Op {
		case token.AND:
			if cl, ok := ast.Unparen(e.X).(*ast.CompositeLit); ok {
				return fb.addrOfLit(cl, fb.typeOf(cl))
			}
			return fb.lvAddr(fb.lvalue(e.X))
		case token.ARROW:
			return fb.recv(e)
		}
		fb.expr(e.X)
		return 0
	case *ast.BinaryExpr:
		return fb.binary(e, T)
	case *ast.CallExpr:
		res, _ := fb.callResults(e)
		if len(res) == 0 {
			return 0
		}
		return res[0]
	case *ast.TypeAssertExpr:
		return fb.typeAssert(e, false)
	case *ast.KeyValueExpr:
		fb.b.errorf(e.Pos(), "key-value outside a composite literal")
		return 0
	case *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.ChanType, *ast.Ellipsis:
		return 0
	}
	fb.b.errorf(e.Pos(), "unknown expression %T", e)
	return 0
}

// isString reports whether T is a string type.
func isString(T types2.Type) bool {
	if T == nil {
		return false
	}
	b, ok := unwrap(T).Underlying().(*types2.Basic)
	return ok && b.Info()&types2.IsString != 0
}

// varValue reads a variable: a local, or a package variable in static
// memory.
func (fb *fnb) varValue(v *types2.Var) Reg {
	if fb.isGlobal(v) {
		if !hasPointers(v.Type()) {
			return 0
		}
		return fb.load(fb.heap(HeapStatic), v.Type())
	}
	return fb.readLocal(v)
}

// isGlobal reports whether v is a package variable.
func (fb *fnb) isGlobal(v *types2.Var) bool {
	return v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
}

func (fb *fnb) binary(e *ast.BinaryExpr, T types2.Type) Reg {
	switch e.Op {
	case token.LAND, token.LOR:
		// Short-circuit: the right operand's calls are conditional.
		yes := fb.newBlock()
		no := fb.newBlock()
		fb.cond(e, yes, no)
		join := fb.newBlock()
		fb.seal(yes)
		fb.startBlock(yes)
		fb.jump(join)
		fb.seal(no)
		fb.startBlock(no)
		fb.jump(join)
		fb.seal(join)
		fb.startBlock(join)
		return 0
	case token.EQL, token.NEQ:
		xT, yT := fb.typeOf(e.X), fb.typeOf(e.Y)
		x := fb.expr(e.X)
		y := fb.expr(e.Y)
		// A comparison of an interface with a concrete value converts the
		// value.
		if isInterface(xT) && !isInterface(yT) && !isNilType(yT) {
			fb.conv(y, yT, xT, e.Pos())
		} else if isInterface(yT) && !isInterface(xT) && !isNilType(xT) {
			fb.conv(x, xT, yT, e.Pos())
		}
		return 0
	}
	x := fb.expr(e.X)
	y := fb.expr(e.Y)
	if isString(T) && e.Op == token.ADD {
		// A concatenation: a new counted string, unless an operand is
		// empty and the other is the result.
		_, _ = x, y
		return fb.heap(HeapCountedCurrent)
	}
	return 0
}

// firstType returns the type of e, or of its first value when it has two
// (a comma-ok form).
func (fb *fnb) firstType(e ast.Expr) types2.Type {
	T := fb.typeOf(e)
	if tup, ok := T.(*types2.Tuple); ok && tup.Len() > 0 {
		return tup.At(0).Type()
	}
	return T
}

// isNilType reports whether T is the type of nil.
func isNilType(T types2.Type) bool {
	b, ok := T.(*types2.Basic)
	return ok && b.Kind() == types2.UntypedNil
}

// sliceExpr builds x[lo:hi:max]: a view of the same memory.
func (fb *fnb) sliceExpr(e *ast.SliceExpr) Reg {
	xt := unwrap(fb.typeOf(e.X))
	var x Reg
	switch coreType(xt).(type) {
	case *types2.Array:
		// Slicing an array takes its address.
		x = fb.lvAddr(fb.lvalue(e.X))
	default:
		x = fb.expr(e.X)
	}
	for _, i := range []ast.Expr{e.Low, e.High, e.Max} {
		if i != nil {
			fb.expr(i)
		}
	}
	return x
}

// typeAssert builds x.(T).
func (fb *fnb) typeAssert(e *ast.TypeAssertExpr, commaOk bool) Reg {
	x := fb.expr(e.X)
	T := fb.firstType(e)
	if fb.b.moves.AssertMoves[e] {
		// The owner moves out of the interface variable.
		_ = commaOk
	}
	if !hasPointers(T) {
		return 0
	}
	if types2.UnionVariants(fb.typeOf(e.X)) != nil {
		// A union's payload.
		return x
	}
	switch countsTo(fb.typeOf(e.X), T) { // borrow.go
	case lendView:
		return fb.borrowError(e.X, fb.assertTo(x, T, e.Pos()))
	case lendCopy:
		return fb.recount(fb.assertTo(x, T, e.Pos()), T)
	}
	return fb.assertTo(x, T, e.Pos())
}

// assertTo asserts the interface value x to T: the data word, and for a
// concrete type that is not its own data word, the value loaded from the
// box.
func (fb *fnb) assertTo(x Reg, T types2.Type, pos token.Pos) Reg {
	fb.b.noteAsserted(T)
	r := fb.emitDst(Instr{Op: OpAssert, X: x, Type: T, Pos: pos})
	if isTypeParam(unwrap(T)) {
		// The instance decides: the data word is the value (a direct
		// type) or a box holding it.
		return fb.either(r, fb.load(r, T))
	}
	if !isInterface(T) && !fb.isDirect(T) {
		return fb.load(r, T)
	}
	return r
}

// funcValue is a declared function used as a value: a static closure
// record.
func (fb *fnb) funcValue(fn *types2.Func, id *ast.Ident) Reg {
	fid := fb.b.funcOf(fn)
	if fid == NoFunc {
		fb.b.errorf(id.Pos(), "function %s used as a value has no source", fn.Name())
		return 0
	}
	fb.noteInstance(id)
	fb.f.Facts.FuncValues = append(fb.f.Facts.FuncValues, fid)
	return fb.emitDst(Instr{Op: OpClosure, Callee: fid, Obj: NoObj, Pos: id.Pos()})
}

// funcLit builds a function literal: its function, built now so that its
// captures are known, and its closure record in this frame (or a static
// record when it captures nothing).
func (fb *fnb) funcLit(lit *ast.FuncLit) (Reg, FuncID) {
	id := fb.b.p.Lookup(FuncKey{Lit: lit})
	var lf *fnb
	if id == NoFunc {
		sig, _ := fb.b.types[lit].Type.(*types2.Signature)
		f := fb.b.newFunc(FuncKey{Lit: lit}, "func literal in "+fb.f.Name+" at "+fb.b.fset.Position(lit.Pos()).String(), fb.f.Pkg, sig, lit.Pos(), lit.End())
		lf = fb.b.newFnb(f, fb.pkg, fb)
		lf.bodyNode = lit.Body
		fb.b.buildBody(lf, sig, true, lit.Body)
		id = f.ID
		fb.b.litFns[lit] = lf
	} else {
		lf = fb.b.litFns[lit]
	}
	fb.f.Facts.FuncValues = append(fb.f.Facts.FuncValues, id)
	var args []Reg
	obj := NoObj
	if lf != nil && len(lf.caps) > 0 {
		args = fb.recordArgs(lf)
		life := LifeFrame
		if fb.regionNodes[lit] {
			life = LifeCaller // built in the caller's region (section 11.5)
		}
		owner, o := fb.keptObj(Object{Kind: ObjClosure, Type: fb.b.types[lit].Type, Name: "closure", Pos: lit.Pos(), Life: life})
		if owner != fb {
			return fb.fillKept(owner, o, 0, nil, args), id
		}
		obj = o
		if life == LifeFrame {
			fb.noteRecordCaps(o, lf.caps)
		}
	}
	return fb.emitDst(Instr{Op: OpClosure, Callee: id, Obj: obj, Args: args, Pos: lit.Pos()}), id
}

// compositeLit builds a composite literal's value.
func (fb *fnb) compositeLit(e *ast.CompositeLit, T types2.Type) Reg {
	if e.Type != nil {
		if v := fb.variantOf(e.Type); v != nil {
			// U.V{...}: a union value holding the payload.
			return fb.compositeLitOf(e, v.Payload())
		}
	}
	return fb.compositeLitOf(e, T)
}

// variantOf returns the union variant a type expression names (U.V), or
// nil.
func (fb *fnb) variantOf(e ast.Expr) *types2.Variant {
	if se, ok := ast.Unparen(e).(*ast.SelectorExpr); ok {
		v, _ := fb.b.uses[se.Sel].(*types2.Variant)
		return v
	}
	return nil
}

func (fb *fnb) compositeLitOf(e *ast.CompositeLit, T types2.Type) Reg {
	T = unwrap(T)
	switch u := coreType(T).(type) {
	case *types2.Pointer:
		// &T{...} elided in a nested literal.
		return fb.addrOfLit(e, unwrap(u.Elem()))
	case *types2.Struct:
		var args []Reg
		var paths [][]int
		for i, elt := range e.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				path, ft := fb.literalField(T, kv.Key.(*ast.Ident))
				r := fb.compositeElem(kv.Value, ft)
				if r != 0 {
					args = append(args, r)
					paths = append(paths, path)
				}
				fb.noteFieldWrite(ft, r, kv.Value.Pos())
				continue
			}
			ft := u.Field(i).Type()
			r := fb.compositeElem(elt, ft)
			if r != 0 {
				args = append(args, r)
				paths = append(paths, []int{i})
			}
			fb.noteFieldWrite(ft, r, elt.Pos())
		}
		if !hasPointers(T) {
			return 0
		}
		return fb.emitDst(Instr{Op: OpCompose, Args: args, Fields: paths, Type: T, Pos: e.Pos()})
	case *types2.Array:
		args, paths := fb.elems(e.Elts, u.Elem())
		if !hasPointers(T) {
			return 0
		}
		return fb.emitDst(Instr{Op: OpCompose, Args: args, Fields: paths, Type: T, Pos: e.Pos()})
	case *types2.Slice:
		// The backing array lives in this frame (static memory in a
		// package initializer).
		n := literalLen(fb.b, e.Elts)
		arrT := types2.NewArray(u.Elem(), n)
		if fb.f.Key.Init != nil {
			args, _ := fb.elems(e.Elts, u.Elem())
			p := fb.heap(HeapStatic)
			for _, a := range args {
				fb.store(fb.elem(p), a, u.Elem())
			}
			return p
		}
		owner, o := fb.keptObj(Object{Kind: ObjSliceLit, Type: arrT, Name: "slice literal", Pos: e.Pos()})
		args, _ := fb.elems(e.Elts, u.Elem())
		base := fb.objAddr(owner, o)
		for _, a := range args {
			fb.store(fb.elem(base), a, u.Elem())
		}
		return base
	case *types2.Map:
		return fb.mapLit(e, T, u)
	}
	for _, elt := range e.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			fb.expr(kv.Value)
		} else {
			fb.expr(elt)
		}
	}
	return 0
}

// elems builds the elements of an array or slice literal.
func (fb *fnb) elems(elts []ast.Expr, et types2.Type) ([]Reg, [][]int) {
	var args []Reg
	var paths [][]int
	for _, elt := range elts {
		v := elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			fb.expr(kv.Key)
			v = kv.Value
		}
		r := fb.compositeElem(v, et)
		fb.noteFieldWrite(et, r, v.Pos())
		if r != 0 {
			args = append(args, r)
			paths = append(paths, []int{ElemStep})
		}
	}
	return args, paths
}

// literalLen returns the length of a slice literal.
func literalLen(b *builder, elts []ast.Expr) int64 {
	var n, i int64
	for _, elt := range elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if c := b.types[kv.Key].Value; !c.IsNil() {
				if k, ok := constInt(c); ok {
					i = k
				}
			}
		}
		i++
		if i > n {
			n = i
		}
	}
	return n
}

// compositeElem builds an element of a composite literal, whose type may be
// elided.
func (fb *fnb) compositeElem(e ast.Expr, T types2.Type) Reg {
	if cl, ok := e.(*ast.CompositeLit); ok && cl.Type == nil {
		return fb.compositeLitOf(cl, T)
	}
	return fb.exprAs(e, T)
}

// addrOfLit builds &T{...}: storage in this frame (static memory in a
// package initializer).
func (fb *fnb) addrOfLit(e *ast.CompositeLit, T types2.Type) Reg {
	v := fb.compositeLit(e, T)
	if fb.f.Key.Init != nil {
		p := fb.heap(HeapStatic)
		fb.store(p, v, T)
		return p
	}
	owner, o := fb.keptObj(Object{Kind: ObjTemp, Type: T, Name: "&T{...}", Pos: e.Pos()})
	a := fb.objAddr(owner, o)
	fb.store(a, v, T)
	return a
}

// literalField returns the field path and type of the field the key of a
// struct literal names, promoted through embedded structs.
func (fb *fnb) literalField(T types2.Type, key *ast.Ident) ([]int, types2.Type) {
	obj, index, _ := types2.LookupFieldOrMethod(T, false, fb.f.Pkg, key.Name)
	if v, ok := obj.(*types2.Var); ok {
		return index, v.Type()
	}
	if v, ok := fb.b.uses[key].(*types2.Var); ok {
		return []int{0}, v.Type()
	}
	fb.b.errorf(key.Pos(), "unknown field %s", key.Name)
	return []int{0}, types2.Typ[types2.Int]
}

// noteFieldWrite records a write of a struct value r of type T into a
// place that holds it by value (plan invariant I6).
func (fb *fnb) noteFieldWrite(T types2.Type, r Reg, pos token.Pos) {
	if T == nil || r == 0 {
		return
	}
	if _, ok := unwrap(T).Underlying().(*types2.Struct); ok && hasPointers(T) {
		fb.f.Facts.FieldWrites = append(fb.f.Facts.FieldWrites, FieldWrite{Type: T, From: r, Pos: pos})
	}
}

// selector builds x.f, x.m, pkg.Name, T.m and U.V.
func (fb *fnb) selector(e *ast.SelectorExpr, T types2.Type) Reg {
	if v, ok := fb.b.uses[e.Sel].(*types2.Variant); ok {
		return fb.variantValue(e, v, T)
	}
	if id, ok := e.X.(*ast.Ident); ok {
		if _, isPkg := fb.b.uses[id].(*types2.PkgName); isPkg {
			switch obj := fb.b.uses[e.Sel].(type) {
			case *types2.Var:
				return fb.varValue(obj)
			case *types2.Func:
				return fb.funcValue(obj, e.Sel)
			}
			return fb.constReg(T)
		}
	}
	sel := fb.b.selections[e]
	if sel == nil {
		fb.b.errorf(e.Pos(), "unresolved selector %s", e.Sel.Name)
		return 0
	}
	switch sel.Kind() {
	case types2.FieldVal:
		return fb.loadLV(fb.lvalue(e))
	case types2.MethodVal:
		return fb.methodValue(e, sel)
	case types2.MethodExpr:
		return fb.methodExpr(e, sel)
	}
	return 0
}

// variantValue is U.V used as a value: a union value for a variant without
// a payload, or the function func(P) U for one with.
func (fb *fnb) variantValue(e *ast.SelectorExpr, v *types2.Variant, T types2.Type) Reg {
	if signatureOf(T) == nil {
		return 0
	}
	U := fb.typeOf(e.X)
	id := fb.b.synth(Synth{Kind: SynthVariant, Recv: U, Path: []int{v.Index()}})
	fb.f.Facts.FuncValues = append(fb.f.Facts.FuncValues, id)
	return fb.emitDst(Instr{Op: OpClosure, Callee: id, Obj: NoObj, Pos: e.Pos()})
}

// mapLit builds a map literal: mapMake, then mapAssign of each entry.
func (fb *fnb) mapLit(e *ast.CompositeLit, T types2.Type, m *types2.Map) Reg {
	res := fb.mapCall("mapMake", m, []Reg{0}, e.Pos(), ImplNone)
	var mv Reg
	if len(res) > 0 {
		mv = res[0]
	}
	for _, el := range e.Elts {
		kv := el.(*ast.KeyValueExpr)
		k := fb.compositeElem(kv.Key, m.Key())
		v := fb.compositeElem(kv.Value, m.Elem())
		fb.mapCall("mapAssign", m, []Reg{mv, k, v}, kv.Pos(), ImplNone)
	}
	return mv
}

// mapOf returns T's map type, or nil.
func mapOf(T types2.Type) *types2.Map {
	if T == nil {
		return nil
	}
	m, _ := coreType(T).(*types2.Map)
	return m
}

// sliceElem returns the element type of a slice type.
func sliceElem(T types2.Type) types2.Type {
	if s, ok := coreType(T).(*types2.Slice); ok {
		return s.Elem()
	}
	return nil
}

// containsTypeParam reports whether T mentions a type parameter.
func containsTypeParam(T types2.Type) bool {
	return containsTP(T, map[types2.Type]bool{})
}

func containsTP(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil || seen[T] {
		return false
	}
	seen[T] = true
	switch t := types2.Unalias(T).(type) {
	case *types2.TypeParam:
		return true
	case *types2.Named:
		if ta := t.TypeArgs(); ta != nil {
			for i := 0; i < ta.Len(); i++ {
				if containsTP(ta.At(i), seen) {
					return true
				}
			}
		}
		return false
	case *types2.Pointer:
		return containsTP(t.Elem(), seen)
	case *types2.Slice:
		return containsTP(t.Elem(), seen)
	case *types2.Array:
		return containsTP(t.Elem(), seen)
	case *types2.Map:
		return containsTP(t.Key(), seen) || containsTP(t.Elem(), seen)
	case *types2.Chan:
		return containsTP(t.Elem(), seen)
	case *types2.Wrapper:
		return containsTP(t.Elem(), seen)
	case *types2.Signature:
		for i := 0; i < t.Params().Len(); i++ {
			if containsTP(t.Params().At(i).Type(), seen) {
				return true
			}
		}
		for i := 0; i < t.Results().Len(); i++ {
			if containsTP(t.Results().At(i).Type(), seen) {
				return true
			}
		}
		return false
	case *types2.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if containsTP(t.Field(i).Type(), seen) {
				return true
			}
		}
		return false
	case *types2.Tuple:
		for i := 0; i < t.Len(); i++ {
			if containsTP(t.At(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}
