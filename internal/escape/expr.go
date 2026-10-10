package escape

import (
	"fmt"
	"strconv"
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

// load returns what the memory the pointers in ts point to may hold.
func (w *walker) load(ts tset) tset {
	var out tset
	for _, t := range ts {
		switch t.kind {
		case tHeap:
			out.add(heapTok)
		case tFrame:
			out.addAll(t.loc.get(t.path))
		case tParam:
			out.add(t.untag().extend("*"))
		}
	}
	return out
}

// offset returns pointers to field i of what the pointers in ts point to.
// Fields are told apart two levels deep: a field of a field of a field
// keeps the path of its enclosing one, which stands for everything inside.
func offset(ts tset, i int) tset {
	var out tset
	for _, t := range ts {
		t = t.untag()
		if t.kind != tHeap && fieldTail(t.path) >= maxFieldNest {
			out.add(t)
			continue
		}
		switch t.kind {
		case tFrame:
			t.path += fieldStep(i)
		case tParam:
			t = t.extend(fieldStep(i))
		}
		out.add(t)
	}
	return out
}

// collapse forgets the field paths of pointers that pass through
// unsafe.Pointer, where they may be reinterpreted as another type.
func collapse(ts tset) tset {
	var out tset
	for _, t := range ts {
		t = t.untag()
		switch t.kind {
		case tFrame:
			t.path = ""
		case tParam:
			t = t.widen()
		}
		out.add(t)
	}
	return out
}

// walkKey follows k's path from the pointers in ts, applying its type
// filter where the path reaches the interface value it constrains.
func (w *walker) walkKey(ts tset, k pkey) tset {
	cur := ts
	at := 0
	for _, step := range splitPath(k.path) {
		if k.filt != nil && at == k.filtAt {
			cur = w.a.filterDyn(cur, k.filt, at)
		}
		at += len(step)
		if step == "*" {
			cur = w.load(cur)
			continue
		}
		i, _ := strconv.Atoi(strings.TrimSuffix(step, "."))
		cur = offset(cur, i)
	}
	if k.filt != nil && at == k.filtAt {
		cur = w.a.filterDyn(cur, k.filt, at)
	}
	if !k.ge {
		return cur
	}
	return w.reach(cur)
}

// filterDyn applies a type assertion to interface I to the data words in
// ts: a frame box or a pointer to a frame variable whose type does not
// implement I cannot be there. Parameter tokens carry the filter on to
// the caller.
//
// I may also be a concrete type, the receiver of a method a dynamic call
// may reach: then only data words whose dynamic type has that method as its
// own (I itself, or a pointer to I) are kept.
func (a *analyzer) filterDyn(ts tset, I types2.Type, _ int) tset {
	iface, _ := I.Underlying().(*types2.Interface)
	exact := iface == nil
	if exact && containsTypeParam(I) {
		return ts
	}
	if !exact && iface.Empty() {
		return ts
	}
	var out tset
	for _, t := range ts {
		switch t.kind {
		case tFrame:
			if t.path == "" && t.loc.typ != nil && !containsTypeParam(t.loc.typ) {
				dyn := t.loc.typ
				if !t.loc.box {
					dyn = types2.NewPointer(dyn)
				}
				if exact && !types2.Identical(dyn, I) && !types2.Identical(dyn, types2.NewPointer(I)) {
					continue
				}
				if !exact && !types2.Implements(dyn, iface) {
					continue
				}
			}
		case tParam:
			if t.filt == nil && !t.ge {
				t.filt, t.filtAt = I, len(t.path)
			}
		}
		out.add(t)
	}
	return out
}

// reach returns the pointers in ts and everything reachable from them.
func (w *walker) reach(ts tset) tset {
	widen := func(ts tset) tset {
		var out tset
		for _, t := range ts {
			t = t.untag()
			if t.kind == tParam {
				t = t.widen()
			}
			out.add(t)
		}
		return out
	}
	out := widen(ts)
	for {
		if !out.addAll(widen(w.load(out))) {
			return out
		}
	}
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
			return w.identValue(e.Sel) // qualified identifier
		}
		switch sel.Kind() {
		case types2.FieldVal:
			p, isAddr := w.fieldPlace(e.X, sel.Index())
			if f, ok := sel.Obj().(*types2.Var); ok && w.a.isHeapField(f) {
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
		for _, i := range index {
			w.value(i)
		}
		xt := w.typeOf(x)
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
		return w.value(e.X)

	case *ast.StarExpr:
		return w.load(w.value(e.X))

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
		return nil

	case *ast.CallExpr:
		res := w.call(e)
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
		return w.unbox(xv, w.typeOf(e.Type))

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
		switch u := underlying(w.typeOf(e.X)).(type) {
		case *types2.Array:
			return w.place(e.X)
		case *types2.Pointer, *types2.Slice:
			_ = u
			return w.value(e.X), true
		}
		w.value(e.X)
		return nil, false
	case *ast.StarExpr:
		return w.value(e.X), true
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
		cur, isAddr, T = w.value(x), true, el
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
	if from == nil || to == nil || !isInterface(to) || isInterface(from) {
		return v
	}
	if b, ok := from.(*types2.Basic); ok && b.Kind() == types2.UntypedNil {
		return nil
	}
	if b, ok := from.Underlying().(*types2.Basic); ok && b.Info()&types2.IsUntyped != 0 {
		return nil // an untyped constant becomes a word or a static string
	}
	w.a.noteConversion(from)
	_, known := isDirect(from)
	if w.a.isDirectType(from) {
		switch {
		case types2.PointerShaped(from):
			return shapeOut(v, from)
		case !known:
			// A type parameter whose instances are all direct, some of
			// them perhaps pointer-shaped structs: the data word is the
			// value, whichever field held its pointer.
			var out tset
			for _, t := range v {
				out.add(t.untag())
			}
			return out
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
	box := w.tmp(node, lkBox)
	box.typ, box.box = from, true
	w.addContents(box, v)
	out := tset{frameTok(box)}
	if !known {
		out.addAll(v)
	}
	return out
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

// explicitConv handles T(x).
func (w *walker) explicitConv(e *ast.CallExpr, to types2.Type) tset {
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
		if isUnsafePointer(from) || isUnsafePointer(to) {
			// The address keeps its path while no pointers can be read
			// through it under the new type; reinterpreted as memory that
			// holds pointers, its field paths no longer mean anything.
			if isUnsafePointer(to) || !hasPointers(elemOrPointee(to)) {
				return v
			}
			return collapse(v)
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
	case *types2.Slice:
		val = w.elems(e.Elts, u.Elem())
		if w.static {
			w.store(tset{heapTok}, val, e.Pos(), dest{text: "a static slice literal"})
			return tset{heapTok}
		}
		l := w.tmp(e, lkSliceLit)
		w.addContents(l, val)
		return tset{frameTok(l)}
	default:
		for _, el := range e.Elts {
			w.value(el)
		}
	}
	return val
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
	var out []*types2.Var
	seen := map[*types2.Var]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := w.a.uses[id].(*types2.Var)
		if !ok || seen[v] || v.IsField() || isGlobal(v) {
			return true
		}
		if v.Pos() >= lit.Pos() && v.Pos() < lit.End() {
			return true
		}
		seen[v] = true
		out = append(out, v)
		return true
	})
	return out
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
	fn := sel.Obj().(*types2.Func)
	recv := fn.Type().(*types2.Signature).Recv()
	wantPtr := false
	if recv != nil {
		_, wantPtr = recv.Type().(*types2.Pointer)
	}
	path := sel.Index()
	xt := w.typeOf(x)
	var cur tset
	isAddr := false
	T := xt
	if el := pointerElem(xt); el != nil {
		cur, isAddr, T = w.value(x), true, el
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
		l := w.tmp(x, lkRecv)
		w.addContents(l, cur)
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
		res := w.call(x)
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
	text   string   // "n.next", "package variable G"
	global bool     // a package variable
	src    ast.Expr // the value stored, as written, if known
	// Set when a callee does the store: the callee and what it does.
	callee, calleeWhy string
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
		return fmt.Sprintf("passes %s to %s (%s), which %s", desc, d.callee, shortPos(pos), d.calleeWhy)
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
	why := func(v tok, desc string) func() string {
		return func() string { return d.why(desc, w.a.fset.Position(pos)) }
	}
	escape := func(v tok) {
		w.escapeParam(v, func() string { return why(v, v.sc.fn.paramDesc(keyOf(v)))() })
	}
	frameErr := func(v tok, where storeKind, via string, owner *scope) {
		e := &viewErr{sink: sinkStore, pos: pos, expr: d.src, l: v.loc, fn: w.scopeName(owner), dst: d.text, where: where, via: via}
		if d.global {
			e.where = storeGlobal
		}
		if d.callee != "" {
			e.sink, e.callee, e.calleeWhy = sinkCall, d.callee, d.calleeWhy
		}
		w.report(e, fmt.Sprintf("store|%p", v.loc))
	}
	for _, t := range targets {
		switch t.kind {
		case tFrame:
			var add tset
			for _, v := range vals {
				switch v.kind {
				case tFrame:
					if v.loc.level > t.loc.level {
						frameErr(v, storeOuter, "", w.sc)
						continue
					}
					if L := w.keptPast(t.loc, v.loc); L != nil {
						w.loopErr(v.loc, L, d, pos)
					}
				case tParam:
					if v.sc.level > t.loc.level {
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
						sum.addStore(e, &reason{text: why(v, "it")()})
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
// channel, handed to a go statement or to a callee that keeps it. e
// describes the sink for an error; its l is filled in for each frame
// location. act is a format for summary reasons, with %s for what escapes,
// and tail follows the position: "passes %s to g" and ", which stores it
// in G (a.p:3)".
func (w *walker) sink(v tset, e *viewErr, act, tail string) {
	for _, t := range v {
		switch t.kind {
		case tFrame:
			ve := *e
			ve.l = t.loc
			w.report(&ve, fmt.Sprintf("sink|%p", t.loc))
		case tParam:
			w.escapeParam(t, func() string {
				return fmt.Sprintf(act, t.sc.fn.paramDesc(keyOf(t))) + " (" + shortPos(w.a.fset.Position(e.pos)) + ")" + tail
			})
		}
	}
}

// escapeParam records that a parameter-derived pointer escapes from the
// function that owns the parameter. why is called only when that is new.
func (w *walker) escapeParam(t tok, why func() string) {
	if t.sc.level == 0 && t.sc.fn.resultHolds[t.idx] {
		// Kept only while the result, which holds it, lives.
		return
	}
	sum := &t.sc.fn.sum
	if _, ok := sum.escapes[keyOf(t)]; ok {
		return
	}
	sum.addEscape(keyOf(t), &reason{text: why()})
	w.a.grew = true
}
