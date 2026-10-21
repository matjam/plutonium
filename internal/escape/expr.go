package escape

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// ----------------------------------------------------------------------------
// Helpers

func (w *walker) typeOf(e ast.Expr) types2.Type {
	if e == nil {
		return nil
	}
	if tv, ok := w.a.types[e]; ok && tv.Type != nil {
		return tv.Type
	}
	if id, ok := e.(*ast.Ident); ok {
		if obj := w.objOf(id); obj != nil {
			return obj.Type()
		}
	}
	return nil
}

func (w *walker) objOf(id *ast.Ident) types2.Object {
	if obj := w.a.uses[id]; obj != nil {
		return obj
	}
	return w.a.defs[id]
}

// underlying returns T's underlying type, looking through volatile and
// aligned, which change how a value is stored but not what it is.
func underlying(T types2.Type) types2.Type {
	if T == nil {
		return nil
	}
	for {
		u := T.Underlying()
		w, ok := u.(*types2.Wrapper)
		if !ok || (w.Kind() != types2.WrapVolatile && w.Kind() != types2.WrapAligned) {
			return u
		}
		T = w.Elem()
	}
}

func isGlobal(v *types2.Var) bool {
	return v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
}

// argumentHolders records the lifetime of strong values independently of
// their heap contents. A later borrowed use activates it; copying the strong
// value does not. Re-evaluating a named value replaces its previous holder.
func (w *walker) argumentHolders(v tset, x ast.Expr, T types2.Type, identity string) tset {
	var out tbuf
	for _, t := range v {
		if t.kind == tHeap {
			t.holder = nil
			var visit func(types2.Type, string, string)
			visit = func(T types2.Type, tag, path string) {
				if isBoxPtr(T) {
					if t.tag == "" || tag == "" || strings.HasPrefix(t.tag, tag) || strings.HasPrefix(tag, t.tag) {
						u := t
						u.holder = w.argumentHolder(x, T, identity+path)
						out.add(u)
					}
					return
				}
				switch T := underlying(T).(type) {
				case *types2.Struct:
					for i := 0; i < T.NumFields(); i++ {
						visit(T.Field(i).Type(), capFields(tag+fieldStep(i)), path+fieldStep(i))
					}
				case *types2.Array:
					visit(T.Elem(), tag, path+"[]")
				}
			}
			visit(T, "", "")
		}
		out.add(t)
	}
	return out.done()
}

func (w *walker) argumentHolder(x ast.Expr, T types2.Type, path string) *loc {
	if path == "" && isBoxPtr(w.typeOf(x)) {
		for _, t := range w.boxView(x) {
			if t.kind == tFrame {
				return t.loc
			}
		}
		return nil
	}
	var base *loc
	if root := w.frameRoot(x); root != nil {
		base = w.varLoc(root)
	} else if root := w.boxTemporary(x); root != nil || isTuple(w.typeOf(x)) {
		if root == nil {
			root = x
		}
		base = w.tmp(x, lkStmtTemp)
		base.node, base.pos, base.typ = root, root.Pos(), w.typeOf(root)
	} else {
		return nil
	}
	if base.holders == nil {
		base.holders = make(map[holderKey]*loc)
	}
	key := holderKey{x, path}
	if l := base.holders[key]; l != nil {
		return l
	}
	l := *base
	l.holders = nil
	l.object = true
	if base.v != nil {
		l.kind = lkObject
	}
	l.dynamic = types2.NewPointer(pointerElem(T))
	base.holders[key] = &l
	return &l
}

func isTuple(T types2.Type) bool {
	_, ok := T.(*types2.Tuple)
	return ok
}

// borrowHolders turns strong-value metadata into the lifetime of a view.
// Parameter summaries remember this operation for callers whose actual
// strong type is only known after generic instantiation.
func borrowHolders(v tset) tset {
	var out tbuf
	for _, t := range v {
		if t.kind == tHeap && t.holder != nil {
			u := frameTok(t.holder)
			u.tag = t.tag
			out.add(u)
		}
		if t.kind == tParam {
			t.borrowed = true
		}
		out.add(t)
	}
	return out.done()
}

// varLoc returns the location of a local variable, creating it at the
// level of the innermost scope that declares it.
func (w *walker) varLoc(v *types2.Var) *loc {
	if l := w.locs[v]; l != nil {
		return l
	}
	owner := w.top
	for sc := w.sc; sc != nil; sc = sc.parent {
		if sc.contains(v.Pos()) {
			owner = sc
			break
		}
	}
	l := &loc{kind: lkVar, v: v, fn: w.scopeName(owner), pos: v.Pos(), level: owner.level, typ: v.Type()}
	w.locs[v] = l
	return l
}

// tmp returns the frame temporary of kind k the compiler makes for node.
func (w *walker) tmp(node ast.Node, k locKind) *loc {
	key := tmpKey{node, k}
	if l := w.tmps[key]; l != nil {
		return l
	}
	l := &loc{kind: k, node: node, fn: w.scopeName(w.sc), pos: node.Pos(), level: w.sc.level}
	w.tmps[key] = l
	return l
}

// addContents stores v into the whole of l.
func (w *walker) addContents(l *loc, v tset) {
	if l.put("", v) {
		w.grew = true
	}
}

// isBoxPtr reports whether T is owned[T], shared[T] or synced[T], whose
// value points to a heap object that the box keeps alive.
func isBoxPtr(T types2.Type) bool {
	if T == nil {
		return false
	}
	b, ok := T.Underlying().(*types2.Wrapper)
	return ok && b.IsPointer()
}

// pointee returns what the pointer x points to: its value for a pointer,
// and for a box a view of the box's object, bounded by where the box is
// held (boxView).
func (w *walker) pointee(x ast.Expr) tset {
	v := w.value(x)
	if isBoxPtr(w.typeOf(x)) {
		return w.boxView(x)
	}
	return v
}

// boxView returns what a view of the object of the box x points to
// (language spec section 18.7, rule 5): a view through a box may not outlive
// the box it was made through. A box held in a local variable or a
// parameter of this frame (owned parameters are the callee's own) bounds
// the view by that variable; a temporary box bounds it by the statement
// that drops it. A projected temporary is bounded by its
// containing value's statement too. A box reached any other way (a field of heap
// memory, a package variable) gives a view of heap memory, checked at run
// time.
func (w *walker) boxView(x ast.Expr) tset {
	switch e := ast.Unparen(x).(type) {
	case *ast.Ident:
		v, _ := w.objOf(e).(*types2.Var)
		if v == nil || isGlobal(v) {
			return tset{heapTok}
		}
		return tset{frameTok(w.objLoc(v))}
	case *ast.SelectorExpr, *ast.IndexExpr, *ast.StarExpr:
		if root := w.frameRoot(e); root != nil {
			l := w.tmp(x, lkObject)
			vl := w.varLoc(root)
			l.v, l.fn, l.pos, l.level, l.typ = root, vl.fn, vl.pos, vl.level, root.Type()
			l.dynamic = w.typeOf(x)
			if el := pointerElem(l.dynamic); el != nil {
				l.dynamic = types2.NewPointer(el)
			}
			return tset{frameTok(l)}
		}
		if root := w.boxTemporary(e); root != nil {
			return w.boxTempView(x, root)
		}
		return tset{heapTok}
	}
	return w.boxTempView(x, x)
}

func (w *walker) boxTempView(x, root ast.Expr) tset {
	kind := lkObject
	_, array := underlying(w.typeOf(root)).(*types2.Array)
	if isBoxPtr(w.typeOf(root)) || types2.IsOwner(w.typeOf(root)) || isStructValue(w.typeOf(root)) || array {
		kind = lkStmtTemp
	}
	l := w.tmp(x, kind)
	l.node, l.pos = root, root.Pos()
	l.typ = w.typeOf(root)
	l.object = true
	if el := pointerElem(w.typeOf(x)); el != nil {
		l.dynamic = types2.NewPointer(el)
	}
	return tset{frameTok(l)}
}

// boxTemporary finds the temporary that owns a projected box. Value
// fields and array elements remain part of their owner; a raw pointer
// reached through an object instead follows the runtime heap rules.
func (w *walker) boxTemporary(x ast.Expr) ast.Expr {
	switch e := ast.Unparen(x).(type) {
	case *ast.Ident:
		return nil
	case *ast.SelectorExpr:
		if sel := w.a.selections[e]; sel == nil || sel.Kind() != types2.FieldVal {
			return nil
		}
		if pointerElem(w.typeOf(e.X)) == nil || isBoxPtr(w.typeOf(e.X)) {
			return w.boxTemporary(e.X)
		}
	case *ast.IndexExpr:
		if _, ok := underlying(w.typeOf(e.X)).(*types2.Array); ok {
			return w.boxTemporary(e.X)
		}
	case *ast.StarExpr:
		if isBoxPtr(w.typeOf(e.X)) {
			return w.boxTemporary(e.X)
		}
	default:
		if types2.IsOwner(w.typeOf(x)) || isBoxPtr(w.typeOf(x)) || isStructValue(w.typeOf(x)) {
			return x
		}
		if _, ok := underlying(w.typeOf(x)).(*types2.Array); ok {
			return x
		}
	}
	return nil
}

// frameRoot returns the local variable a field or array element selection
// is part of, without following pointers, or nil.
func (w *walker) frameRoot(e ast.Expr) *types2.Var {
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			v, _ := w.objOf(x).(*types2.Var)
			if v == nil || isGlobal(v) {
				return nil
			}
			return v
		case *ast.SelectorExpr:
			sel := w.a.selections[x]
			if sel == nil || sel.Kind() != types2.FieldVal || sel.Indirect() {
				return nil
			}
			e = x.X
		case *ast.IndexExpr:
			if _, ok := underlying(w.typeOf(x.X)).(*types2.Array); !ok {
				return nil
			}
			e = x.X
		default:
			return nil
		}
	}
}

// objLoc returns the location standing for the object of the box held in
// local variable v.
func (w *walker) objLoc(v *types2.Var) *loc {
	if l := w.objs[v]; l != nil {
		return l
	}
	vl := w.varLoc(v)
	l := &loc{kind: lkObject, v: v, fn: vl.fn, pos: vl.pos, level: vl.level, typ: v.Type()}
	w.objs[v] = l
	return l
}

// reach returns the pointers in ts and everything reachable from them.
func (w *walker) reach(ts tset) tset {
	// The tokens widened are added to out as they are found, and each round
	// loads only the tokens the last one added: loading a token yields the
	// same set whatever else is in out, so the result is the same as loading
	// everything again, in the same order.
	var out tbuf
	next := ts
	for len(next) > 0 {
		from := len(out.set)
		for _, t := range next {
			t = t.untag()
			if t.kind == tParam {
				t = t.widen()
			}
			out.add(t)
		}
		next = w.load(out.set[from:])
	}
	return out.done()
}

// ----------------------------------------------------------------------------
// Expressions

// value walks e and returns what its value may point to.
func (w *walker) value(e ast.Expr) tset {
	if e == nil {
		return nil
	}
	r := w.value1(e)
	if T := w.typeOf(e); T != nil {
		if _, tuple := T.(*types2.Tuple); !tuple && !hasPointers(T) {
			return nil
		}
		if types2.IsCountedError(T) {
			switch x := ast.Unparen(e).(type) {
			case *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr, *ast.StarExpr:
				// An error loaded from unknown heap memory may hold a
				// counted object. Parameter paths still identify a holder.
				r = countedHeap(r)
			case *ast.UnaryExpr:
				if x.Op == token.ARROW {
					r = countedHeap(r)
				}
			case *ast.Ident:
				if obj, ok := w.objOf(x).(*types2.Var); ok && isGlobal(obj) {
					r = countedHeap(r)
				}
			}
		}
	}
	return r
}

func (w *walker) value1(e ast.Expr) tset {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return w.value(e.X)

	case *ast.Ident:
		return w.identValue(e)

	case *ast.BasicLit:
		return nil // string literals are static

	case *ast.FuncLit:
		return w.funcLit(e)

	case *ast.CompositeLit:
		return w.compLit(e)

	case *ast.SelectorExpr:
		sel := w.a.selections[e]
		if sel == nil {
			if v, ok := w.a.uses[e.Sel].(*types2.Variant); ok && v.HasPayload() {
				// A variant as a function value, func(P) U: a call through it
				// returns what its argument holds (dynFuncCall).
				if sig, ok := underlying(w.typeOf(e)).(*types2.Signature); ok {
					w.a.noteVariantValue(sig)
				}
				return nil
			}
			return w.identValue(e.Sel) // qualified identifier
		}
		switch sel.Kind() {
		case types2.FieldVal:
			if isTemporary(e.X) {
				// A field of an owner nothing keeps: the owner is dropped at
				// the end of the statement.
				w.discard(e.X, w.value(e.X))
			}
			p, isAddr := w.fieldPlace(e.X, sel.Index())
			// Counted fields keep their children alive. Retain the
			// receiver path so a borrowed result is bound to that holder.
			if f, ok := sel.Obj().(*types2.Var); ok && w.a.isHeapField(f) && !types2.IsCountedError(f.Type()) && !isCountedBox(f.Type()) {
				return tset{heapTok}
			}
			if isAddr {
				return w.load(p)
			}
			return p
		case types2.MethodVal:
			return w.methodValue(e, sel)
		case types2.MethodExpr:
			if fn, ok := sel.Obj().(*types2.Func); ok {
				if recv := fn.Type().(*types2.Signature).Recv(); recv != nil && isInterface(recv.Type()) {
					// I.M calls whatever its first argument holds.
					for _, c := range w.a.ifaceCandidates(fn.Name(), true, fn.Type().(*types2.Signature)) {
						w.a.noteFuncValue(c.f, fvIfaceExpr)
					}
				} else {
					w.a.noteFuncValue(w.a.funcs[fn.Origin()], fvPlain)
				}
			}
			return tset{heapTok}
		}
		return nil

	case *ast.IndexExpr, *ast.IndexListExpr:
		var x ast.Expr
		var index []ast.Expr
		if ix, ok := e.(*ast.IndexExpr); ok {
			x, index = ix.X, []ast.Expr{ix.Index}
		} else {
			ix := e.(*ast.IndexListExpr)
			x, index = ix.X, ix.Indices
		}
		if _, ok := w.typeOf(x).Underlying().(*types2.Signature); ok {
			// A generic function or method instantiated as a value.
			return w.value(x)
		}
		if cx, ci, ok := w.indexSugar(e); ok {
			if res, ok := w.indexCall(e, cx, ci, types2.IndexMethodName, nil, nil, 1); ok {
				return res[0]
			}
		}
		for _, i := range index {
			w.value(i)
		}
		xt := w.typeOf(x)
		if isBoxPtr(xt) {
			return w.load(w.pointee(x)) // an element of a box's array
		}
		if isMap(xt) {
			// A map holds only what may be stored in heap memory (store).
			w.value(x)
			return tset{heapTok}
		}
		switch u := underlying(xt).(type) {
		case *types2.Basic:
			w.value(x)
			return nil // a byte of a string
		case *types2.Array:
			return w.value(x)
		case *types2.Pointer, *types2.Slice:
			_ = u
			return w.load(w.value(x))
		}
		return w.value(x)

	case *ast.SliceExpr:
		for _, i := range []ast.Expr{e.Low, e.High, e.Max} {
			if i != nil {
				w.value(i)
			}
		}
		if _, ok := underlying(w.typeOf(e.X)).(*types2.Array); ok {
			if p, ok := w.place(e.X); ok {
				return p
			}
		}
		return w.pointee(e.X)

	case *ast.StarExpr:
		return w.load(w.pointee(e.X))

	case *ast.UnaryExpr:
		switch e.Op {
		case token.AND:
			return w.addrOf(e.X)
		case token.ARROW:
			w.value(e.X)
			return tset{heapTok} // channels carry no frame pointers
		}
		w.value(e.X)
		return nil

	case *ast.BinaryExpr:
		w.value(e.X)
		w.value(e.Y)
		if T := w.typeOf(e); e.Op == token.ADD && T != nil && isString(T) {
			// A concatenation allocates its result through the default
			// allocator, which may be a local arena (arena.go).
			return w.arenaAt(e.Pos())
		}
		return nil

	case *ast.CallExpr:
		res := w.arenaResult(e, w.call(e))
		if len(res) == 0 {
			return nil
		}
		if len(res) > 1 {
			return union(res...)
		}
		return res[0]

	case *ast.TypeAssertExpr:
		xv := w.value(e.X)
		if e.Type == nil {
			return xv
		}
		to := w.typeOf(e.Type)
		if w.a.countedErrorResult(to) {
			// A counted result holds its own reference. Remove only the
			// bound attached to a borrowed error, retaining frame pointers
			// carried by a non-counted dynamic value.
			var held tset
			for _, t := range xv {
				if t.kind == tFrame && t.loc.kind == lkObject && types2.IsCountedError(t.loc.typ) {
					continue
				}
				held.add(t)
			}
			return w.unbox(held, to)
		}
		if types2.IsCountedError(w.typeOf(e.X)) {
			xv = w.borrowError(e.X, xv)
		}
		return w.unbox(xv, to)

	case *ast.KeyValueExpr:
		w.value(e.Key)
		return w.value(e.Value)

	case *ast.BadExpr, *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType,
		*ast.MapType, *ast.ChanType, *ast.Ellipsis:
		return nil
	}
	w.a.internalf(e.Pos(), "unsupported expression %T", e)
	return nil
}

func (w *walker) identValue(id *ast.Ident) tset {
	switch obj := w.objOf(id).(type) {
	case *types2.Var:
		if isGlobal(obj) {
			return tset{heapTok}
		}
		if isCounted(obj.Type()) {
			return tset{heapTok}
		}
		if w.isTracked(obj) {
			return w.readVar(w.varLoc(obj))
		}
		return w.varLoc(obj).get("")
	case *types2.Func:
		w.a.noteFuncValue(w.a.funcs[obj.Origin()], fvPlain)
		return tset{heapTok} // a static closure record
	}
	return nil
}

// unbox returns what a value of type T taken out of an interface whose
// data word is xv may point to.
func (w *walker) unbox(xv tset, T types2.Type) tset {
	if T == nil {
		return xv
	}
	if isInterface(T) {
		return w.a.filterDyn(xv, T, 0)
	}
	_, known := isDirect(T)
	switch {
	case types2.PointerShaped(T):
		return shapeIn(xv, T)
	case w.a.isDirectType(T):
		return xv
	case !known:
		return union(xv, w.load(xv))
	}
	return w.load(xv)
}

// elemShape is the shape of a pointer to the element e of an array or slice
// (shapeElement), which matters only to a filter that a pointer to a value of
// the element's type can pass: a type with no methods is the receiver of none
// and implements no interface with methods, so a filter drops its pointer
// whether it judges it by the type of the element or of the array.
func (w *walker) elemShape(e *ast.IndexExpr) ptrShape {
	if w.a.mayHaveMethods(w.typeOf(e)) {
		return shapeElement
	}
	return shapeWhole
}

// place returns what the address of e may point to, if e is addressable.
func (w *walker) place(e ast.Expr) (tset, bool) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return w.place(e.X)
	case *ast.Ident:
		v, _ := w.objOf(e).(*types2.Var)
		if v == nil {
			return nil, false
		}
		if isGlobal(v) {
			return tset{heapTok}, true
		}
		if w.isTracked(v) {
			// Its address is never taken: callers read it as a value.
			return nil, false
		}
		return tset{frameTok(w.varLoc(v))}, true
	case *ast.SelectorExpr:
		sel := w.a.selections[e]
		if sel == nil {
			return w.place(e.Sel)
		}
		if sel.Kind() != types2.FieldVal {
			return nil, false
		}
		return w.fieldPlace(e.X, sel.Index())
	case *ast.IndexExpr:
		w.value(e.Index)
		if isBoxPtr(w.typeOf(e.X)) {
			return asShape(w.pointee(e.X), w.elemShape(e)), true
		}
		if isMap(w.typeOf(e.X)) {
			// A map's table is heap memory that any copy of the map may
			// reach, however long it lives.
			w.value(e.X)
			return tset{heapTok}, true
		}
		switch u := underlying(w.typeOf(e.X)).(type) {
		case *types2.Array:
			// An element is not the array: its address is a pointer of
			// another type than the location's (ptrShape).
			p, ok := w.place(e.X)
			return asShape(p, w.elemShape(e)), ok
		case *types2.Pointer, *types2.Slice:
			_ = u
			return asShape(w.value(e.X), w.elemShape(e)), true
		}
		w.value(e.X)
		return nil, false
	case *ast.StarExpr:
		return w.pointee(e.X), true
	case *ast.CompositeLit:
		return w.litPlace(e), true
	}
	return nil, false
}

// addrOf returns what &e may point to; a non-addressable operand is copied
// to a frame temporary.
func (w *walker) addrOf(e ast.Expr) tset {
	if p, ok := w.place(e); ok {
		return p
	}
	v := w.value(e)
	l := w.tmp(e, lkTemp)
	w.addContents(l, v)
	return tset{frameTok(l)}
}

// fieldPlace follows a field selection's path from x. It returns the
// address of the field when it is addressable (true), or else the value of
// the struct it is part of (false).
func (w *walker) fieldPlace(x ast.Expr, path []int) (tset, bool) {
	xt := w.typeOf(x)
	var cur tset
	isAddr := false
	T := xt
	if el := pointerElem(xt); el != nil {
		cur, isAddr, T = w.pointee(x), true, el
	} else if p, ok := w.place(x); ok {
		cur, isAddr = p, true
	} else {
		cur = w.value(x)
	}
	return w.followFields(cur, isAddr, T, path)
}

// followFields steps through the fields of path, starting from the address
// of a T (isAddr) or a T value, loading the embedded pointers on the way.
// It returns the address of the last field, or the value of the struct
// holding it when no pointer was followed from a non-addressable value.
func (w *walker) followFields(cur tset, isAddr bool, T types2.Type, path []int) (tset, bool) {
	for i, idx := range path {
		F := fieldType(T, idx)
		if F == nil {
			break
		}
		if isAddr {
			cur = offset(cur, idx)
		} else {
			cur = selectField(cur, idx)
		}
		if i == len(path)-1 {
			break
		}
		if el := pointerElem(F); el != nil {
			if isAddr {
				cur = w.load(cur)
			}
			isAddr, T = true, el
		} else {
			T = F
		}
	}
	return cur, isAddr
}

// embeddedType returns the type of the field path leads to from T.
func (w *walker) embeddedType(T types2.Type, path []int) types2.Type {
	for _, idx := range path {
		T = fieldType(T, idx)
		if T == nil {
			return nil
		}
	}
	return T
}

// fieldType returns the type of field idx of struct type T, or nil.
func fieldType(T types2.Type, idx int) types2.Type {
	if el := pointerElem(T); el != nil {
		T = el
	}
	s, ok := underlying(T).(*types2.Struct)
	if !ok || idx >= s.NumFields() {
		return nil
	}
	return s.Field(idx).Type()
}

// conv converts a value of type from to type to, boxing it in the frame
// when to is an interface and the value does not fit in a word.
func (w *walker) conv(v tset, from, to types2.Type, node ast.Node) tset {
	if from == nil || to == nil || !isInterface(to) {
		return v
	}
	if isInterface(from) {
		if types2.IsCountedError(from) && !types2.IsCountedError(to) && !types2.IsOwner(to) {
			if x, ok := node.(ast.Expr); ok {
				return w.borrowError(x, v)
			}
		}
		return v
	}
	if b, ok := from.(*types2.Basic); ok && b.Kind() == types2.UntypedNil {
		return nil
	}
	if b, ok := from.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
		return nil // an untyped constant becomes a word or a static string
	}
	w.a.noteConversion(from)
	countedError := types2.IsCountedError(to) && isCountedBox(from)
	if countedError {
		v = countedHeap(v)
	}
	if types2.IsCountedError(to) {
		if _, generic := types2.Unalias(from).(*types2.TypeParam); generic || countedError {
			// A generic caller decides whether the concrete argument is
			// a strong box. Preserve this conversion in parameter summaries.
			var out tbuf
			for _, t := range v {
				if t.kind == tParam {
					t.counted = true
					t.directCount = t.directCount || countedError
				}
				out.add(t)
			}
			v = out.done()
		}
	}
	if x, ok := node.(ast.Expr); ok && (isBoxPtr(from) || refNoun(from) != "") && !types2.IsOwner(to) && !countedError {
		// An ordinary interface lends: holding a box's pointer, or a
		// map's or channel's, it is a view of the variable converted and
		// counts no reference of its own (language spec section 7.9).
		return w.boxView(x)
	}
	_, known := isDirect(from)
	if w.a.isDirectConv(from) {
		switch {
		case types2.PointerShaped(from):
			if _, ptr := from.Underlying().(*types2.Pointer); ptr {
				return shapeOut(v, from)
			}
			// A struct or array of one pointer: the data word is the
			// pointer, but the value is of the wrapper's type.
			return asShape(shapeOut(v, from), shapeOther)
		case !known:
			// A type parameter whose instances are all direct, some of
			// them perhaps pointer-shaped structs: the data word is the
			// value, whichever field held its pointer.
			var out tbuf
			for _, t := range v {
				out.add(t.untag().as(shapeOther))
			}
			return out.done()
		}
		return v
	}
	if w.static {
		// Package initialisation boxes in static storage.
		w.store(tset{heapTok}, v, node.Pos(), dest{text: "a static interface box"})
		if known {
			return tset{heapTok}
		}
		return union(v, tset{heapTok})
	}
	if x, ok := node.(ast.Expr); ok && !w.noBox && releasesCounted(from) {
		// The box holds a reference to each counted value in the copy
		// until the function returns, and releasing it writes into the
		// arena (language spec section 18.8, Arenas).
		for _, t := range v {
			if t.kind == tFrame && t.loc.kind == lkArena && t.loc.level == w.sc.level && t.loc.v != nil && isArenaType(t.loc.v.Type()) {
				w.report(&viewErr{sink: sinkBox, pos: node.Pos(), expr: x, l: t.loc, fn: w.scopeName(w.sc)}, fmt.Sprintf("box|%p", t.loc))
			}
		}
	}
	box := w.tmp(node, lkBox)
	box.typ, box.box = from, true
	w.addContents(box, v)
	out := tset{frameTok(box)}
	if !known {
		out.addAll(v)
	}
	return out
}

// borrowError bounds a non-counted view by the error holding its reference.
// Borrowed parameters and non-counted heap references (such as static
// sentinels) preserve their provenance. A counted heap reference needs a
// holder, even if an Unwrap call made it while visiting a borrowed root.
func (w *walker) borrowError(x ast.Expr, v tset) tset {
	borrowed := true
	for _, t := range v {
		borrowed = borrowed && (t.kind == tParam || t.kind == tHeap && !t.counted)
	}
	if borrowed {
		return v
	}
	return union(v, w.boxView(x))
}

// releasesCounted reports whether dropping a value of type T releases a
// reference: T is, or holds, a string, a box, a map or a channel (or may,
// for a type parameter).
func releasesCounted(T types2.Type) bool { return releasesSeen(T, map[types2.Type]bool{}) }

func releasesSeen(T types2.Type, seen map[types2.Type]bool) bool {
	if T == nil || seen[T] {
		return false
	}
	seen[T] = true
	if _, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return true
	}
	if isString(T) || isCounted(T) || isChan(T) {
		return true
	}
	switch u := T.Underlying().(type) {
	case *types2.Array:
		return u.Len() > 0 && releasesSeen(u.Elem(), seen)
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if releasesSeen(u.Field(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}

func isCountedBox(T types2.Type) bool {
	w, ok := T.Underlying().(*types2.Wrapper)
	return ok && (w.Kind() == types2.WrapShared || w.Kind() == types2.WrapSynced)
}

func countedHeap(v tset) tset {
	var out tbuf
	for _, t := range v {
		if t.kind == tHeap {
			t.counted = true
		}
		out.add(t)
	}
	return out.done()
}

// countedErrorResult reports whether every instance of T keeps its own
// reference when asserted from an error. Generic bodies have one summary,
// so mixed counted and lending targets must retain the lending bound.
func (a *analyzer) countedErrorResult(T types2.Type) bool {
	if T == nil {
		return false
	}
	if tp, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		args := a.tparamTypes(tp)
		if len(args) == 0 {
			return false
		}
		for _, arg := range args {
			if !types2.IsCountedError(arg) && !isCountedBox(arg) {
				return false
			}
		}
		return true
	}
	return types2.IsCountedError(T) || isCountedBox(T)
}

// elemOrPointee returns what a pointer, slice or string's memory holds:
// the element or pointee type, or byte for a string.
func elemOrPointee(T types2.Type) types2.Type {
	if el := pointerElem(T); el != nil {
		return el
	}
	if el := elemType(T); el != nil {
		return el
	}
	return types2.Typ[types2.Byte]
}

func isUnsafePointer(T types2.Type) bool {
	b, ok := underlying(T).(*types2.Basic)
	return ok && b.Kind() == types2.UnsafePointer
}

func isString(T types2.Type) bool {
	b, ok := underlying(T).(*types2.Basic)
	return ok && (b.Kind() == types2.String || b.Kind() == types2.UntypedString)
}

// explicitConv handles T(x).
func (w *walker) explicitConv(e *ast.CallExpr, to types2.Type) tset {
	v := w.explicitConv1(e, to)
	if len(e.Args) == 1 && retypesPointer(w.typeOf(e.Args[0]), to) {
		if te := pointerElem(to); te == nil || w.a.mayHoldMethods(te) {
			// A pointer to a type with no methods, which has no field or
			// element with some, is no receiver and leads to none: a
			// conversion of it to a type with some marks it again. An element
			// or field of what it points to is no conversion, so a type that
			// holds one with methods is marked here.
			return asShape(v, shapeOther)
		}
	}
	return v
}

// retypesPointer reports whether converting a value of type from to type to
// gives a pointer that is not of the type of the location it points to, though
// the one converted was: a pointer to another type, a pointer made from an
// unsafe.Pointer, or from a slice (ptrShape). An unsafe.Pointer made from a
// pointer is the same pointer, to the same whole location (section 18.3).
func retypesPointer(from, to types2.Type) bool {
	if from == nil || to == nil {
		return false
	}
	if isUnsafePointer(from) {
		return !isUnsafePointer(to)
	}
	if isUnsafePointer(to) {
		return false
	}
	if _, ok := underlying(from).(*types2.Slice); ok {
		return pointerElem(to) != nil
	}
	fe, te := pointerElem(from), pointerElem(to)
	return fe != nil && te != nil && !types2.Identical(fe, te)
}

func (w *walker) explicitConv1(e *ast.CallExpr, to types2.Type) tset {
	if len(e.Args) != 1 {
		return nil
	}
	x := e.Args[0]
	v := w.value(x)
	from := w.typeOf(x)
	if !hasPointers(to) {
		return nil
	}
	if from != nil {
		// string(runes) copies into a counted buffer (spec section 18.8).
		if isString(to) && !isString(from) {
			return nil
		}
		if isString(from) && !isString(to) {
			return tset{heapTok}
		}
		if b, ok := underlying(from).(*types2.Basic); ok && b.Kind() != types2.UnsafePointer && b.Kind() != types2.String {
			// An integer made into a pointer: unsafe code's business. It
			// is treated as pointing outside every frame.
			return tset{heapTok}
		}
		if _, ok := underlying(from).(*types2.Slice); ok {
			if _, ok := underlying(to).(*types2.Array); ok {
				return w.load(v) // the elements are copied
			}
		}
		if isBoxPtr(from) && (isUnsafePointer(to) || pointerElem(to) != nil && !isBoxPtr(to)) {
			// (*T)(p) and unsafe.Pointer(p): a view of the box's object.
			return w.boxView(x)
		}
		if isUnsafePointer(from) || isUnsafePointer(to) {
			// The address keeps its path while no pointers can be read
			// through it under the new type; reinterpreted as memory that
			// holds pointers, its field paths no longer mean anything.
			if isUnsafePointer(to) || !hasPointers(elemOrPointee(to)) {
				return v
			}
			return intoSet(v, shapeOffset)
		}
		if _, ok := underlying(from).(*types2.Struct); ok {
			// T(x) for struct types with the same fields: T's fields hold
			// what x's do.
			w.a.storeConvFields(to, v, 0)
		}
	}
	return w.conv(v, from, to, x)
}

// compLit returns what a composite literal's value may point to.
func (w *walker) compLit(e *ast.CompositeLit) tset {
	T := w.typeOf(e)
	if el := pointerElem(T); el != nil {
		T = el // an elided &T{...} inside another literal
	}
	var val tset
	switch u := underlying(T).(type) {
	case *types2.Struct:
		for i, el := range e.Elts {
			x := el
			var fT types2.Type
			fi := -1
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				x = kv.Value
				if id, ok := kv.Key.(*ast.Ident); ok {
					for j := 0; j < u.NumFields(); j++ {
						if u.Field(j).Name() == id.Name {
							fi, fT = j, u.Field(j).Type()
						}
					}
					if fi < 0 {
						val.addAll(w.promotedElem(T, id, x))
						continue
					}
				}
			} else if i < u.NumFields() {
				fi, fT = i, u.Field(i).Type()
			}
			if fi >= 0 {
				ev := w.elem(x, fT)
				w.a.storeField(u.Field(fi), ev, nil)
				val.addAll(tagField(ev, fi))
			} else {
				val.addAll(w.elem(x, fT))
			}
		}
	case *types2.Array:
		val = w.elems(e.Elts, u.Elem())
	case *types2.TaggedUnion:
		// U.V{...}: a variant with a struct payload. The union holds what
		// the payload's fields do, and the payload's fields are written as
		// a struct literal's are (heap-only fields, fields.go).
		var payload *types2.Struct
		if se, ok := ast.Unparen(e.Type).(*ast.SelectorExpr); ok {
			if v, ok := w.a.uses[se.Sel].(*types2.Variant); ok && v.HasPayload() {
				payload, _ = underlying(v.Payload()).(*types2.Struct)
			}
		}
		for i, el := range e.Elts {
			x := el
			var key *ast.Ident
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				x = kv.Value
				key, _ = kv.Key.(*ast.Ident)
			}
			ev := w.elem(x, w.typeOf(x))
			if f := structField(payload, key, i); f != nil {
				w.a.storeField(f, ev, nil)
			}
			val.addAll(ev)
		}
	case *types2.Map:
		// The keys and elements are stored in the map's table, heap
		// memory that any copy of the map may reach.
		for _, el := range e.Elts {
			if p, ok := el.(*ast.KeyValueExpr); ok {
				w.store(tset{heapTok}, w.elem(p.Key, u.Key()), p.Key.Pos(), dest{text: "a key of the map literal", src: p.Key, inMap: "key"})
				w.store(tset{heapTok}, w.elem(p.Value, u.Elem()), p.Value.Pos(), dest{text: "an element of the map literal", src: p.Value, inMap: "element"})
			}
		}
		return union(tset{heapTok}, w.arenaAt(e.Pos()))
	case *types2.Slice:
		val = w.elems(e.Elts, u.Elem())
		if w.static {
			w.store(tset{heapTok}, val, e.Pos(), dest{text: "a static slice literal"})
			return tset{heapTok}
		}
		l := w.tmp(e, lkSliceLit)
		l.typ = types2.NewArray(u.Elem(), int64(len(e.Elts))) // what a pointer to an element is into (valueType)
		w.addContents(l, val)
		return tset{frameTok(l)}
	default:
		for _, el := range e.Elts {
			w.value(el)
		}
	}
	return val
}

// promotedElem evaluates the element x of a struct literal of type T whose
// key names a field promoted through embedded structs (language spec
// section 11.3), as a store into that field of the embedded field that
// holds it.
func (w *walker) promotedElem(T types2.Type, key *ast.Ident, x ast.Expr) tset {
	var pkg *types2.Package
	if f, ok := w.objOf(key).(*types2.Var); ok {
		pkg = f.Pkg()
	}
	path := types2.LiteralFields(T, pkg, key.Name)
	if len(path) == 0 {
		return w.elem(x, nil)
	}
	f := path[len(path)-1]
	ev := w.elem(x, f.Type())
	outer := make([]*types2.Var, 0, len(path)-1)
	for i := len(path) - 2; i >= 0; i-- {
		outer = append(outer, path[i])
	}
	w.a.storeField(f, ev, outer)
	// Tag the value with the field's place: innermost field first.
	cur := T
	idx := make([]int, len(path))
	for i, g := range path {
		s, _ := underlying(cur).(*types2.Struct)
		for j := 0; s != nil && j < s.NumFields(); j++ {
			if s.Field(j) == g {
				idx[i] = j
			}
		}
		cur = g.Type()
	}
	for i := len(idx) - 1; i >= 0; i-- {
		ev = tagField(ev, idx[i])
	}
	return ev
}

func (w *walker) elems(elts []ast.Expr, T types2.Type) tset {
	var val tset
	for _, el := range elts {
		x := el
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			w.value(kv.Key)
			x = kv.Value
		}
		val.addAll(w.elem(x, T))
	}
	return val
}

// elem evaluates one element of a composite literal of element type T.
func (w *walker) elem(x ast.Expr, T types2.Type) tset {
	if lit, ok := x.(*ast.CompositeLit); ok && lit.Type == nil && T != nil && pointerElem(T) != nil {
		return w.litPlace(lit) // elided &T{...}
	}
	return w.conv(w.value(x), w.typeOf(x), T, x)
}

// litPlace returns what &T{...} points to: a frame temporary, or static
// storage during package initialisation.
func (w *walker) litPlace(lit *ast.CompositeLit) tset {
	val := w.compLit(lit)
	if w.static {
		w.store(tset{heapTok}, val, lit.Pos(), dest{text: "a static composite literal"})
		return tset{heapTok}
	}
	l := w.tmp(lit, lkLit)
	if T := w.typeOf(lit); T != nil {
		if el := pointerElem(T); el != nil {
			T = el
		}
		l.typ = T
	}
	w.addContents(l, val)
	return tset{frameTok(l)}
}

// funcLit walks a function literal in a scope of its own and returns its
// closure value.
func (w *walker) funcLit(lit *ast.FuncLit) tset {
	fn := w.a.litFunc(lit, w.f.pkg, w.f.name)
	w.a.noteFuncValue(fn, fvPlain)
	sc := w.scopes[lit]
	if sc == nil {
		sc = &scope{fn: fn, parent: w.sc, level: w.sc.level + 1, node: lit}
		w.scopes[lit] = sc
	}
	// The body runs whenever the closure is called: it starts with its own
	// parameters, and its tracked locals are its own.
	saved, savedStatic, savedEnv, savedTargets := w.sc, w.static, w.env, w.targets
	w.sc, w.static, w.env, w.targets = sc, false, env{}, nil
	w.bindParams(sc)
	w.block(lit.Body)
	w.dropScopeVars(sc, lit)
	w.finishScope(sc)
	w.sc, w.static, w.env, w.targets = saved, savedStatic, savedEnv, savedTargets

	captures := w.captured(lit)
	if len(captures) == 0 {
		return tset{heapTok} // a static closure record
	}
	rec := w.tmp(lit, lkClosure)
	rec.captures = captures
	w.recLit[rec] = lit
	for _, v := range captures {
		w.addContents(rec, tset{frameTok(w.varLoc(v))})
	}
	return tset{frameTok(rec)}
}

// captured lists the local variables declared outside lit that it uses.
func (w *walker) captured(lit *ast.FuncLit) []*types2.Var {
	return w.capturedIn(lit.Body, lit.Pos(), lit.End())
}

// methodValue returns x.M used as a value: a closure record in the frame
// holding the receiver.
func (w *walker) methodValue(e *ast.SelectorExpr, sel *types2.Selection) tset {
	fn, _ := sel.Obj().(*types2.Func)
	var recv tset
	if fn != nil && fn.Type().(*types2.Signature).Recv() != nil && isInterface(fn.Type().(*types2.Signature).Recv().Type()) {
		recv = w.receiver(e.X, sel)
		_, byName := types2.Unalias(w.typeOf(e.X)).(*types2.TypeParam)
		for _, c := range w.a.ifaceCandidates(fn.Name(), byName, fn.Type().(*types2.Signature)) {
			w.a.noteFuncValue(c.f, fvBoundIface)
		}
	} else {
		recv = w.receiver(e.X, sel)
		if fn != nil {
			w.a.noteFuncValue(w.a.funcs[fn.Origin()], fvBound)
		}
	}
	if w.static {
		// Package initialisation keeps method value records in static
		// storage.
		w.store(tset{heapTok}, recv, e.Pos(), dest{text: "a static method value"})
		return tset{heapTok}
	}
	rec := w.tmp(e, lkMethodValue)
	w.addContents(rec, recv)
	return tset{frameTok(rec)}
}

// receiver evaluates the receiver of a method call or method value,
// following embedded fields and taking the address or loading the value as
// the method's receiver type requires.
func (w *walker) receiver(x ast.Expr, sel *types2.Selection) tset {
	return w.receiverOf(x, sel.Obj().(*types2.Func), sel.Index())
}

// receiverOf is receiver for method fn, selected from x by path.
func (w *walker) receiverOf(x ast.Expr, fn *types2.Func, path []int) tset {
	recv := fn.Type().(*types2.Signature).Recv()
	wantPtr := false
	if recv != nil {
		_, wantPtr = recv.Type().(*types2.Pointer)
	}
	xt := w.typeOf(x)
	var cur tset
	isAddr := false
	T := xt
	if el := pointerElem(xt); el != nil {
		cur, isAddr, T = w.pointee(x), true, el
	} else if p, ok := w.place(x); ok {
		cur, isAddr = p, true
	} else {
		cur = w.value(x)
	}
	if len(path) > 1 {
		cur, isAddr = w.followFields(cur, isAddr, T, path[:len(path)-1])
		// The method's receiver is the last embedded field; if it is a
		// pointer, that pointer is the receiver's address.
		if F := w.embeddedType(T, path[:len(path)-1]); F != nil && pointerElem(F) != nil {
			if isAddr {
				cur = w.load(cur)
			}
			isAddr = true
		}
	}
	switch {
	case wantPtr && isAddr:
		return cur
	case wantPtr:
		kind := lkRecv
		if types2.IsOwner(xt) {
			// The owner temporary itself is the receiver, for the
			// statement (language spec section 11.14).
			kind = lkStmtTemp
		}
		l := w.tmp(x, kind)
		l.typ = xt
		l.method = fn.Name()
		w.addContents(l, cur)
		if kind == lkStmtTemp && !w.static {
			// Dropped at the end of the statement (language spec section
			// 18.6).
			w.dropAt(tset{frameTok(l)}, xt, exprText(x), x, x.End())
		}
		return tset{frameTok(l)}
	case isAddr:
		return w.load(cur)
	}
	return cur
}

// multi evaluates an expression that yields n values: a call, or a
// comma-ok form.
func (w *walker) multi(e ast.Expr, n int) ([]tset, []types2.Type) {
	vals := make([]tset, n)
	types := make([]types2.Type, n)
	if tup, ok := w.typeOf(e).(*types2.Tuple); ok {
		for i := 0; i < tup.Len() && i < n; i++ {
			types[i] = tup.At(i).Type()
		}
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		res := w.arenaResult(x, w.call(x))
		copy(vals, res)
		return vals, types
	case *ast.TypeAssertExpr:
		vals[0] = w.value1(x)
		types[0] = w.typeOf(x.Type)
	case *ast.UnaryExpr:
		vals[0] = w.value1(x)
		if ch, ok := underlying(w.typeOf(x.X)).(*types2.Chan); ok {
			types[0] = ch.Elem()
		}
	case *ast.IndexExpr:
		if cx, ci, ok := w.indexSugar(x); ok && n == 2 {
			if res, ok := w.indexCall(x, cx, ci, types2.IndexOkMethodName, nil, nil, 2); ok {
				copy(vals, res)
				return vals, types
			}
		}
		vals[0] = w.value1(x)
	default:
		vals[0] = w.value1(x)
	}
	if n > 1 {
		types[1] = types2.Typ[types2.Bool]
	}
	return vals, types
}

// ----------------------------------------------------------------------------
// Where pointers go

// A dest describes memory being stored into, for messages.
type dest struct {
	text   string // "n.next", "package variable G"
	global bool   // a package variable
	inMap  string // a map's "key" or "element" a store goes to, or ""

	src ast.Expr // the value stored, as written, if known
	// Set when a callee does the store: the callee and what it does.
	callee    string
	calleeWhy *reason
}

func (w *walker) destOf(lhs ast.Expr) dest {
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
		if v, ok := w.objOf(id).(*types2.Var); ok && isGlobal(v) {
			return dest{text: "package variable " + v.Name(), global: true}
		}
	}
	if se, ok := ast.Unparen(lhs).(*ast.SelectorExpr); ok && w.a.selections[se] == nil {
		if v, ok := w.objOf(se.Sel).(*types2.Var); ok && isGlobal(v) {
			return dest{text: "package variable " + types2.ExprString(se), global: true}
		}
	}
	return dest{text: types2.ExprString(lhs)}
}

// why phrases, for a summary reason, a store of desc into d at pos.
func (d dest) why(desc string, pos token.Position) string {
	if d.callee != "" {
		return fmt.Sprintf("passes %s to %s (%s), which %s", desc, d.callee, shortPos(pos), d.calleeWhy.String())
	}
	return fmt.Sprintf("stores %s in %s (%s)", desc, d.text, shortPos(pos))
}

// store records that the pointers in vals are stored into the memory the
// pointers in targets point to.
func (w *walker) store(targets, vals tset, pos token.Pos, d dest) {
	// No targets means the pointer is nil: every pointer the analysis cannot
	// trace (globals, allocations, integers made into pointers) carries the
	// heap token.
	if len(vals) == 0 || len(targets) == 0 {
		return
	}
	escape := func(v tok) {
		w.escapeParam(v, func() string { return d.why(v.sc.fn.paramDesc(keyOf(v)), w.a.fset.Position(pos)) })
	}
	frameErr := func(v tok, where storeKind, via string, owner *scope) {
		e := &viewErr{sink: sinkStore, pos: pos, expr: d.src, l: v.loc, fn: w.scopeName(owner), dst: d.text, where: where, via: via, inMap: d.inMap}
		if d.global {
			e.where = storeGlobal
		}
		if d.callee != "" {
			e.sink, e.callee, e.calleeWhy = sinkCall, d.callee, d.calleeWhy
		}
		w.report(e, fmt.Sprintf("store|%p", v.loc))
	}
	for _, t := range targets {
		if t.kind == tFrame && (t.loc.kind == lkObject || t.loc.kind == lkArena || t.loc.object) {
			t = heapTok // a box's object, or an arena's memory, is not the frame's
		}
		switch t.kind {
		case tFrame:
			var add tset
			for _, v := range vals {
				switch v.kind {
				case tFrame:
					if st := w.stmtTempKept(v.loc, t.loc); st != nil {
						frameErr(frameTok(st), storeStmt, "", w.sc)
						continue
					}
					if v.loc.level > t.loc.level {
						frameErr(v, storeOuter, "", w.sc)
						continue
					}
					if L := w.keptPast(t.loc, v.loc); L != nil {
						w.loopErr(v.loc, L, d, pos)
					}
				case tParam:
					if v.sc.level > t.loc.level && !v.sc.holds(t.loc) {
						// A literal's parameter kept in an enclosing frame: the
						// literal's callers must not pass frame pointers.
						escape(v)
						add.add(heapTok)
						continue
					}
				}
				add.add(v)
			}
			if t.loc.put(t.path, add) {
				w.grew = true
			}
		case tParam:
			for _, v := range vals {
				switch v.kind {
				case tFrame:
					frameErr(v, storeParam, t.sc.fn.paramName(t.idx), t.sc)
				case tHeap:
					// The caller's memory there holds heap pointers from now
					// on (summary.heapStores).
					if t.sc.fn.sum.addHeapStore(keyOf(t)) {
						w.a.grew = true
					}
				case tParam:
					if v.sc != t.sc {
						escape(v)
						continue
					}
					if v.idx == t.idx && selfSafe(v, t) {
						continue
					}
					sum := &t.sc.fn.sum
					e := edge{src: keyOf(v), dst: keyOf(t)}
					if _, ok := sum.stores[e]; !ok {
						sum.addStore(e, &reason{build: func() string { return d.why("it", w.a.fset.Position(pos)) }})
						w.a.grew = true
					}
				}
			}
		case tHeap:
			for _, v := range vals {
				switch v.kind {
				case tFrame:
					frameErr(v, storeHeap, "", w.sc)
				case tParam:
					escape(v)
				}
			}
		}
	}
}

// selfSafe reports whether storing pointer v through pointer t, both
// derived from the same parameter, is safe. Memory never points to younger
// memory, so a pointer found by more loads outlives memory found by fewer:
// storing it there cannot leave it dangling.
func selfSafe(v, t tok) bool {
	return !t.ge && pathDepth(v.path) >= pathDepth(t.path)
}

// sink records that the pointers in v are kept beyond the frame: sent on a
// channel, handed to a task statement or to a callee that keeps it. e
// describes the sink for an error; its l is filled in for each frame
// location. act is a format for summary reasons, with %s for what escapes,
// and tail follows the position: "passes %s to g" and ", which stores it
// in G (a.p:3)".
func (w *walker) sink(v tset, e *viewErr, act, tail string) {
	w.sinkTail(v, e, act, func() string { return tail })
}

// sinkTail is sink with a tail that is made only when the text is.
func (w *walker) sinkTail(v tset, e *viewErr, act string, tail func() string) {
	for _, t := range v {
		switch t.kind {
		case tFrame:
			ve := *e
			ve.l = t.loc
			w.report(&ve, fmt.Sprintf("sink|%p", t.loc))
		case tParam:
			if w.escapeKnown(t) {
				continue
			}
			w.escapeParam(t, func() string {
				return fmt.Sprintf(act, t.sc.fn.paramDesc(keyOf(t))) + " (" + shortPos(w.a.fset.Position(e.pos)) + ")" + tail()
			})
		}
	}
}

// escapeKnown reports whether escapeParam would find nothing new in t.
func (w *walker) escapeKnown(t tok) bool {
	if t.sc.level == 0 && t.sc.fn.resultHolds[t.idx] {
		return true
	}
	return t.sc.fn.sum.hasEscape(keyOf(t))
}

// escapeParam records that a parameter-derived pointer escapes from the
// function that owns the parameter. why is called only when that is new.
func (w *walker) escapeParam(t tok, why func() string) {
	if t.sc.level == 0 && t.sc.fn.resultHolds[t.idx] {
		// Kept only while the result, which holds it, lives.
		return
	}
	sum := &t.sc.fn.sum
	if sum.hasEscape(keyOf(t)) {
		return
	}
	sum.addEscape(keyOf(t), reason{build: why})
	w.a.grew = true
}
