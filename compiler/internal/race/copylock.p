package race

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// This file checks the rule of language spec section 19.3, Copying a lock
// (E3505): a value whose type holds a sync.Mutex, sync.RWMutex, sync.Once,
// sync.WaitGroup or sync.Cond, or outside pluto/rt an rt.Lock, directly or
// through fields and array elements, may not be copied from a place (a
// variable, field, element or dereference). A composite literal, a zero
// value and the result of a call are new values, and moving an owner is a
// move, so none of them is a copy.

type copyCheck struct {
	a    *analyzer
	inRT bool
	memo map[types2.Type]string
	// sink, when set, receives every copy of a place instead of the
	// lock check (mapplace.go).
	sink *mapPlaces
}

// checkCopies reports every copy of a lock in the program's source.
func (a *analyzer) checkCopies() {
	cc := pu.New(copyCheck{a: a, memo: map[types2.Type]string{}})
	for _, p := range a.prog.Packages {
		cc.inRT = p.Path == "pluto/rt" || strings.HasPrefix(p.Path, "pluto/rt/")
		cc.memo = map[types2.Type]string{}
		for _, f := range p.Files {
			ast.Inspect(f, cc.node)
		}
	}
}

// lockIn returns the lock type that T holds ("sync.Mutex"), or "".
func (cc *copyCheck) lockIn(T types2.Type) string {
	if T == nil {
		return ""
	}
	if s, ok := cc.memo[T]; ok {
		return s
	}
	cc.memo[T] = "" // a recursive type holds itself only through pointers
	s := cc.lockOf(T)
	cc.memo[T] = s
	return s
}

func (cc *copyCheck) lockOf(T types2.Type) string {
	if n, ok := types2.Unalias(T).(*types2.Named); ok && n.Obj().Pkg() != nil {
		switch compat.Concat(n.Obj().Pkg().Path(), ".", n.Obj().Name()) {
		case "pluto/sync.Mutex", "pluto/sync.RWMutex", "pluto/sync.Once", "pluto/sync.WaitGroup", "pluto/sync.Cond":
			return compat.Concat("sync.", n.Obj().Name())
		case "pluto/rt.Lock":
			if cc.inRT {
				return ""
			}
			return "rt.Lock"
		}
	}
	switch u := underlying(T).(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if s := cc.lockIn(u.Field(i).Type()); s != "" {
				return s
			}
		}
	case *types2.Array:
		if u.Len() > 0 {
			return cc.lockIn(u.Elem())
		}
	}
	return ""
}

// isPlace reports whether e denotes a variable, a field, an element or a
// dereference: memory that a value is copied out of.
func (cc *copyCheck) isPlace(e ast.Expr) bool {
	a := cc.a
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		_, ok := a.objOf(e).(*types2.Var)
		return ok
	case *ast.SelectorExpr:
		if sel := a.selections[e]; sel != nil {
			return sel.Kind() == types2.FieldVal
		}
		_, ok := a.uses[e.Sel].(*types2.Var)
		return ok
	case *ast.IndexExpr:
		if tv := a.types[e.X]; tv.IsType() {
			return false
		}
		switch underlying(a.typeOf(e.X)).(type) {
		case *types2.Array, *types2.Slice, *types2.Map, *types2.Pointer:
			return true
		}
	case *ast.StarExpr:
		if a.types[e].IsType() {
			return false
		}
		// Memory reached through unsafe.Pointer is not checked (section
		// 19.3, What is not checked): *(*T)(p) for an unsafe.Pointer p.
		if c, ok := ast.Unparen(e.X).(*ast.CallExpr); ok && len(c.Args) == 1 {
			if tv := a.types[ast.Unparen(c.Fun)]; tv.IsType() && isUnsafePointer(a.typeOf(c.Args[0])) {
				return false
			}
		}
		return true
	}
	return false
}

// copied reports a copy of e when it is a place whose value holds a lock.
func (cc *copyCheck) copied(e ast.Expr, what string) {
	if e == nil || !cc.isPlace(e) {
		return
	}
	cc.report(e, cc.a.typeOf(e), what)
}

func (cc *copyCheck) report(e ast.Expr, T types2.Type, what string) {
	if T == nil || types2.IsOwner(T) {
		return
	}
	if cc.sink != nil {
		cc.sink.structCopy(e, T, what)
		return
	}
	lock := cc.lockIn(T)
	if lock == "" {
		return
	}
	held := compat.Concat("holds a ", lock)
	if strings.HasSuffix(types2.TypeString(T, nil), lock) {
		held = compat.Concat("is a ", lock)
	}
	cc.a.add(compat.Sprintf("copy|%d", int(e.Pos())), diag.New(cc.a.pos(e.Pos()), diag.RaceLockCopied, "%s copies %s, which %s: a lock may not be copied, so pass a pointer or keep the value in one place", pu.Box(what), pu.Box(exprText(e)), pu.Box(held)))
}

func (cc *copyCheck) node(n ast.Node) bool {
	a := cc.a
	switch n := n.(type) {
	case *ast.AssignStmt:
		if n.Tok != token.ASSIGN && n.Tok != token.DEFINE {
			return true
		}
		if len(n.Lhs) == len(n.Rhs) {
			for _, r := range n.Rhs {
				cc.copied(r, "the assignment")
			}
		}
	case *ast.ValueSpec:
		if len(n.Names) == len(n.Values) {
			for _, v := range n.Values {
				cc.copied(v, "the initialization")
			}
		}
	case *ast.ReturnStmt:
		for _, r := range n.Results {
			cc.copied(r, "the return")
		}
	case *ast.SendStmt:
		cc.copied(n.Value, "the send")
	case *ast.CompositeLit:
		for _, el := range n.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				el = kv.Value
			}
			cc.copied(el, "the composite literal")
		}
	case *ast.RangeStmt:
		if n.Value == nil {
			return true
		}
		if id, ok := ast.Unparen(n.Value).(*ast.Ident); ok && id.Name == "_" {
			return true
		}
		switch underlying(a.typeOf(n.X)).(type) {
		case *types2.Array, *types2.Slice, *types2.Map, *types2.Pointer:
			cc.report(n.Value, a.typeOf(n.Value), "the range value")
		}
	case *ast.CallExpr:
		tv := a.types[n.Fun]
		switch {
		case tv.IsType():
			for _, arg := range n.Args {
				cc.copied(arg, "the conversion")
			}
		case tv.IsBuiltin():
			if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok && id.Name == "append" && !hasDots(n) {
				for _, arg := range n.Args[1:] {
					cc.copied(arg, "the append")
				}
			}
		default:
			for i, arg := range n.Args {
				if hasDots(n) && i == len(n.Args)-1 {
					break
				}
				cc.copied(arg, "the argument")
			}
		}
	case *ast.SelectorExpr:
		sel := a.selections[n]
		if sel == nil || sel.Kind() != types2.MethodVal {
			return true
		}
		fn, ok := sel.Obj().(*types2.Func)
		if !ok {
			return true
		}
		r := fn.Type().(*types2.Signature).Recv()
		if r == nil || isInterface(r.Type()) {
			return true
		}
		if _, isPtr := underlying(r.Type()).(*types2.Pointer); isPtr {
			return true
		}
		if _, isTP := types2.Unalias(r.Type()).(*types2.TypeParam); isTP {
			return true
		}
		// A value receiver copies the receiver: from a place, or from what
		// a pointer points to.
		if cc.isPlace(n.X) || pointerElem(a.typeOf(n.X)) != nil {
			cc.report(n.X, r.Type(), compat.Concat("the value receiver of ", fn.Name()))
		}
	}
	return true
}

// hasDots reports whether the last argument of c is passed with "...".
func hasDots(c *ast.CallExpr) bool { return c.Ellipsis.IsValid() }
