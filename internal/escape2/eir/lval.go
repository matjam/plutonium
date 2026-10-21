package eir

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Places: what an assignment writes and what & takes the address of.

type lvKind uint8

const (
	lvBlank     lvKind = iota
	lvVar              // a register variable
	lvMem              // memory at addr
	lvContainer        // x[k] on a container: Index and SetIndex
	lvMap              // m[k] on a built-in map: mapAccess and mapAssign
)

// lval is a place.
type lval struct {
	kind lvKind
	v    *types2.Var
	addr Reg
	T    types2.Type // the type of the place
	c    *contRef    // lvContainer, lvMap
}

// dropsOld reports whether storing into the place destroys its old value
// by a drop the assignment makes (a container's SetIndex and a map's
// mapAssign destroy it themselves).
func (lv lval) dropsOld() bool { return lv.kind == lvVar || lv.kind == lvMem }

// contRef is a container operand, evaluated once, and the key of x[k].
type contRef struct {
	T     types2.Type // the container's type
	x     ast.Expr
	at    ast.Expr // the index expression, or the operand of len or range
	iface Reg      // the container, when it is an interface value
	isIfc bool
	base  Reg         // the address of the container's storage
	baseT types2.Type // the type there
	key   Reg
	keyT  types2.Type
	mapT  *types2.Map
	mapV  Reg
}

// lvalue evaluates the operands of a place.
func (fb *fnb) lvalue(e ast.Expr) lval {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		if e.Name == "_" {
			return lval{kind: lvBlank}
		}
		v, ok := fb.b.uses[e].(*types2.Var)
		if !ok {
			v, ok = fb.b.defs[e].(*types2.Var)
		}
		if !ok {
			fb.b.errorf(e.Pos(), "%s is not a variable", e.Name)
			return lval{kind: lvBlank}
		}
		return fb.varLV(v)
	case *ast.SelectorExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			if _, isPkg := fb.b.uses[id].(*types2.PkgName); isPkg {
				if v, ok := fb.b.uses[e.Sel].(*types2.Var); ok {
					return fb.varLV(v)
				}
			}
		}
		sel := fb.b.selections[e]
		if sel == nil || sel.Kind() != types2.FieldVal {
			fb.b.errorf(e.Pos(), "%s is not a field", e.Sel.Name)
			return lval{kind: lvBlank}
		}
		addr, T, ok := fb.fieldBase(e.X)
		if !ok {
			// A field of a value held in a register: no place, only a
			// value.
			x := fb.expr(e.X)
			o := fb.newObj(Object{Kind: ObjTemp, Type: fb.typeOf(e.X), Name: "tmp"})
			fb.store(fb.addr(o), x, fb.typeOf(e.X))
			addr, T = fb.addr(o), fb.typeOf(e.X)
		}
		addr, T = fb.fieldPath(addr, T, sel.Index())
		return lval{kind: lvMem, addr: addr, T: T}
	case *ast.IndexExpr:
		xt := fb.typeOf(e.X)
		if xt != nil && !fb.b.types[e.X].IsType() && types2.IndexesByMethod(xt) {
			c := fb.container(e.X, e)
			kT := fb.indexKeyType(c)
			c.key = fb.exprAs(e.Index, kT)
			c.keyT = kT
			return lval{kind: lvContainer, T: fb.indexElemType(c), c: c}
		}
		if m := mapOf(xt); m != nil {
			mv := fb.expr(e.X)
			k := fb.exprAs(e.Index, m.Key())
			return lval{kind: lvMap, T: m.Elem(), c: &contRef{T: xt, x: e.X, at: e, mapT: m, mapV: mv, key: k, keyT: m.Key()}}
		}
		if coreType(unwrap(xt)) == nil && isTypeParam(unwrap(xt)) {
			// A type parameter whose type set has no core type (string |
			// []byte, []int | [5]int): its element is the result type.
			x := fb.expr(e.X)
			fb.expr(e.Index)
			T := fb.typeOf(e)
			if !hasPointers(T) {
				return lval{kind: lvMem, T: T}
			}
			return lval{kind: lvMem, addr: fb.elem(x), T: T}
		}
		switch u := coreType(unwrap(xt)).(type) {
		case *types2.Array:
			addr, ok := fb.addrIfPlace(e.X)
			if !ok {
				x := fb.expr(e.X)
				o := fb.newObj(Object{Kind: ObjTemp, Type: xt, Name: "tmp"})
				addr = fb.addr(o)
				fb.store(addr, x, xt)
			}
			fb.expr(e.Index)
			return lval{kind: lvMem, addr: fb.field(addr, ElemStep), T: u.Elem()}
		case *types2.Pointer:
			x := fb.expr(e.X)
			fb.expr(e.Index)
			arr := unwrap(u.Elem()).Underlying().(*types2.Array)
			return lval{kind: lvMem, addr: fb.elem(x), T: arr.Elem()}
		case *types2.Slice:
			x := fb.expr(e.X)
			fb.expr(e.Index)
			return lval{kind: lvMem, addr: fb.elem(x), T: u.Elem()}
		case *types2.Wrapper:
			// owned, shared and synced index like a pointer to their
			// object.
			x := fb.pointee(e.X)
			fb.expr(e.Index)
			if arr, ok := unwrap(u.Elem()).Underlying().(*types2.Array); ok {
				return lval{kind: lvMem, addr: fb.elem(x), T: arr.Elem()}
			}
			if sl, ok := unwrap(u.Elem()).Underlying().(*types2.Slice); ok {
				return lval{kind: lvMem, addr: fb.elem(fb.load(x, u.Elem())), T: sl.Elem()}
			}
		}
		fb.b.errorf(e.Pos(), "cannot index %s", xt)
		return lval{kind: lvBlank}
	case *ast.StarExpr:
		x := fb.pointee(e.X)
		return lval{kind: lvMem, addr: x, T: pointerElem(fb.typeOf(e.X))}
	case *ast.CompositeLit:
		v := fb.compositeLit(e, fb.typeOf(e))
		o := fb.newObj(Object{Kind: ObjTemp, Type: fb.typeOf(e), Name: "lit"})
		fb.store(fb.addr(o), v, fb.typeOf(e))
		return lval{kind: lvMem, addr: fb.addr(o), T: fb.typeOf(e)}
	}
	// Not a place: a value, held in a temporary.
	T := fb.typeOf(e)
	v := fb.expr(e)
	o := fb.newObj(Object{Kind: ObjTemp, Type: T, Name: "tmp"})
	fb.store(fb.addr(o), v, T)
	return lval{kind: lvMem, addr: fb.addr(o), T: T}
}

// varLV is the place of a variable.
func (fb *fnb) varLV(v *types2.Var) lval {
	if fb.isGlobal(v) {
		return lval{kind: lvMem, addr: fb.heap(HeapStatic), T: v.Type()}
	}
	if fb.locals[v] {
		return lval{kind: lvVar, v: v, T: v.Type()}
	}
	if a := fb.varAddr(v); a != 0 {
		return lval{kind: lvMem, addr: a, T: v.Type()}
	}
	fb.b.errorf(fb.pos, "variable %s has no storage", v.Name())
	return lval{kind: lvBlank}
}

// addrIfPlace returns the address of e if e is a place with an address
// (not a register variable or a value).
func (fb *fnb) addrIfPlace(e ast.Expr) (Reg, bool) {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		v, ok := fb.b.uses[x].(*types2.Var)
		if !ok || fb.locals[v] {
			return 0, false
		}
	case *ast.SelectorExpr:
		sel := fb.b.selections[x]
		if sel == nil || sel.Kind() != types2.FieldVal {
			if id, ok := x.X.(*ast.Ident); ok {
				if _, isPkg := fb.b.uses[id].(*types2.PkgName); isPkg {
					break
				}
			}
			return 0, false
		}
		if _, _, ok := fb.fieldBaseCheck(x.X); !ok {
			return 0, false
		}
	case *ast.IndexExpr:
		xt := fb.typeOf(x.X)
		if xt == nil || types2.IndexesByMethod(xt) || mapOf(xt) != nil {
			return 0, false
		}
		if _, isArr := coreType(unwrap(xt)).(*types2.Array); isArr {
			if _, ok := fb.addrIfPlace(x.X); !ok {
				return 0, false
			}
		}
	case *ast.StarExpr:
	default:
		return 0, false
	}
	lv := fb.lvalue(e)
	if lv.kind != lvMem {
		return 0, false
	}
	return lv.addr, true
}

// fieldBaseCheck reports, without building anything, whether the
// operand of a field selection is reached through memory.
func (fb *fnb) fieldBaseCheck(x ast.Expr) (Reg, types2.Type, bool) {
	xt := fb.typeOf(x)
	if pointerElem(xt) != nil {
		return 0, xt, true
	}
	switch y := ast.Unparen(x).(type) {
	case *ast.Ident:
		v, ok := fb.b.uses[y].(*types2.Var)
		return 0, xt, ok && !fb.locals[v]
	case *ast.SelectorExpr:
		sel := fb.b.selections[y]
		if sel == nil {
			if id, ok := y.X.(*ast.Ident); ok {
				if _, isPkg := fb.b.uses[id].(*types2.PkgName); isPkg {
					return 0, xt, true
				}
			}
			return 0, xt, false
		}
		if sel.Kind() != types2.FieldVal {
			return 0, xt, false
		}
		return fb.fieldBaseCheck(y.X)
	case *ast.IndexExpr:
		yt := fb.typeOf(y.X)
		if yt == nil || types2.IndexesByMethod(yt) || mapOf(yt) != nil {
			return 0, xt, false
		}
		switch coreType(unwrap(yt)).(type) {
		case *types2.Slice, *types2.Pointer:
			return 0, xt, true
		case *types2.Array:
			return fb.fieldBaseCheck(y.X)
		}
	case *ast.StarExpr:
		return 0, xt, true
	}
	return 0, xt, false
}

// fieldBase returns the address of the struct a field selection's operand
// x designates, and its type: through a pointer, or the address of the
// place x is. ok is false if x is a value with no place.
func (fb *fnb) fieldBase(x ast.Expr) (Reg, types2.Type, bool) {
	xt := fb.typeOf(x)
	if el := pointerElem(xt); el != nil {
		return fb.pointee(x), el, true
	}
	if _, _, ok := fb.fieldBaseCheck(x); !ok {
		return 0, xt, false
	}
	lv := fb.lvalue(x)
	if lv.kind != lvMem {
		return 0, xt, false
	}
	return lv.addr, unwrap(xt), true
}

// fieldPath follows a field selection path from the struct at addr, of
// type T, loading through embedded pointers.
func (fb *fnb) fieldPath(addr Reg, T types2.Type, path []int) (Reg, types2.Type) {
	for i, idx := range path {
		if el := pointerElem(T); el != nil && i > 0 {
			addr = fb.load(addr, T)
			T = el
		}
		st, ok := coreType(unwrap(T)).(*types2.Struct)
		if !ok {
			if el := pointerElem(T); el != nil {
				addr = fb.load(addr, T)
				T = el
				st, ok = coreType(unwrap(T)).(*types2.Struct)
			}
			if !ok {
				if p, isPacked := unwrap(T).Underlying().(*types2.Packed); isPacked && idx < p.NumFields() {
					// A field of a packed type: bits of the same word,
					// which hold no pointers.
					T = p.Field(idx).Var().Type()
					continue
				}
				fb.b.errorf(fb.pos, "field path through %s", T)
				return addr, T
			}
		}
		addr = fb.field(addr, idx)
		T = st.Field(idx).Type()
	}
	return addr, T
}

// lvAddr returns the address of a place.
func (fb *fnb) lvAddr(lv lval) Reg {
	switch lv.kind {
	case lvMem:
		return lv.addr
	case lvVar:
		fb.b.errorf(fb.pos, "the address of register variable %s", lv.v.Name())
	case lvContainer, lvMap:
		fb.b.errorf(fb.pos, "the address of a container element")
	}
	return 0
}

// loadLV reads a place.
func (fb *fnb) loadLV(lv lval) Reg {
	switch lv.kind {
	case lvVar:
		return fb.readLocal(lv.v)
	case lvMem:
		return fb.load(lv.addr, lv.T)
	case lvContainer:
		res, _ := fb.contMethod(lv.c, types2.IndexMethodName, []Reg{lv.c.key}, ImplIndex)
		if len(res) == 1 {
			return res[0]
		}
	case lvMap:
		res := fb.mapCall("mapAccess", lv.c.mapT, []Reg{lv.c.mapV, lv.c.key}, lv.c.at.Pos(), ImplIndex)
		if len(res) == 1 {
			return res[0]
		}
	}
	return 0
}

// storeLV writes a place.
func (fb *fnb) storeLV(lv lval, r Reg) {
	switch lv.kind {
	case lvVar:
		fb.assignVar(lv.v, r)
	case lvMem:
		fb.store(lv.addr, r, lv.T)
		fb.noteFieldWrite(lv.T, r, fb.pos)
	case lvContainer:
		fb.contMethod(lv.c, types2.SetIndexMethodName, []Reg{lv.c.key, r}, ImplSetIndex)
	case lvMap:
		fb.mapCall("mapAssign", lv.c.mapT, []Reg{lv.c.mapV, lv.c.key, r}, lv.c.at.Pos(), ImplSetIndex)
	}
}

// opAssign builds x op= y and x++: a read and a write of the place, of a
// map element through mapAssignPtr.
func (fb *fnb) opAssign(lv lval, y Reg, pos token.Pos) {
	switch lv.kind {
	case lvMap:
		res := fb.mapCall("mapAssignPtr", lv.c.mapT, []Reg{lv.c.mapV, lv.c.key}, lv.c.at.Pos(), ImplOpAssign)
		var p Reg
		if len(res) == 1 {
			p = res[0]
		}
		if isString(lv.T) {
			fb.store(p, fb.heap(HeapCounted), lv.T)
		}
		return
	case lvContainer:
		res, _ := fb.contMethod(lv.c, types2.IndexMethodName, []Reg{lv.c.key}, ImplOpAssign)
		var old Reg
		if len(res) == 1 {
			old = res[0]
		}
		nv := old
		if isString(lv.T) {
			nv = fb.heap(HeapCounted)
		}
		fb.contMethod(lv.c, types2.SetIndexMethodName, []Reg{lv.c.key, nv}, ImplOpAssign)
		return
	}
	if isString(lv.T) {
		fb.loadLV(lv)
		fb.storeLV(lv, fb.heap(HeapCounted))
		return
	}
	_ = y
}
