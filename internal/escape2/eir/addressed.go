package eir

import (
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Which local variables are frame objects. A variable is a register
// variable (in SSA form) only if nothing needs its address: it is never the
// operand of &, never assigned in part (x.f = v, x[i] = v on an array),
// never the receiver of a method that takes a pointer, never sliced, never
// used by a function literal or range-over-func body nested in the function
// that declares it (closure records hold the addresses of what they
// capture), and never the operand of a container's indexing, length or All
// method. Owners that need dropping are frame objects too (declareVar):
// their drop glue takes their address. The rule is conservative: a variable
// that is a frame object where it need not be costs flow sensitivity only.

// addressedIn returns the addressed local variables of a declaration or
// expression tree.
//
// A variable that function literals or range bodies capture, whose address
// nothing else needs, and that is never assigned after its declaration (nor
// is an owner, a scope, or a named result), is captured by value (byValue):
// its closure records hold its value, which is what every read through the
// address would find, and it stays a register variable. The record still
// lives as the variable does (fnb.finish), so a closure kept past the
// iteration of the variable's loop is E3006 as before.
func (b *builder) addressedIn(root ast.Node) map[*types2.Var]bool {
	a := &addrScan{b: b, out: map[*types2.Var]bool{}, captured: map[*types2.Var]bool{}, assigned: map[*types2.Var]bool{}}
	ast.Inspect(root, a.visit)
	for v := range a.captured {
		if a.out[v] {
			continue
		}
		if a.assigned[v] || b.needsDrop(v.Type()) || scoped.IsScopeType(v.Type()) {
			a.out[v] = true
			continue
		}
		if b.byValue == nil {
			b.byValue = map[*types2.Var]bool{}
		}
		b.byValue[v] = true
	}
	return a.out
}

type addrScan struct {
	b        *builder
	out      map[*types2.Var]bool
	captured map[*types2.Var]bool // used by a nested literal or range body
	assigned map[*types2.Var]bool // assigned after their declaration
}

// assign records an assignment to e, a plain identifier, that is not its
// declaration.
func (a *addrScan) assign(e ast.Expr) {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return
	}
	if v := a.b.localVar(id); v != nil && a.b.defs[id] != v {
		a.assigned[v] = true
	}
}

// results records the named results of a function type: a return assigns
// them.
func (a *addrScan) results(ft *ast.FuncType) {
	if ft == nil || ft.Results == nil {
		return
	}
	for _, f := range ft.Results.List {
		for _, id := range f.Names {
			if v, ok := a.b.defs[id].(*types2.Var); ok {
				a.assigned[v] = true
			}
		}
	}
}

// localVar returns the local variable an identifier names, or nil.
func (b *builder) localVar(id *ast.Ident) *types2.Var {
	obj := b.uses[id]
	if obj == nil {
		obj = b.defs[id]
	}
	v, ok := obj.(*types2.Var)
	if !ok || v.IsField() || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() {
		return nil
	}
	return v
}

// rootVar returns the local variable whose storage e is part of, reached
// without a load: x, x.f, x[i] on an array, (x); nil otherwise.
func (a *addrScan) rootVar(e ast.Expr) *types2.Var {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		return a.b.localVar(e)
	case *ast.SelectorExpr:
		sel := a.b.selections[e]
		if sel == nil || sel.Kind() != types2.FieldVal || sel.Indirect() {
			return nil
		}
		return a.rootVar(e.X)
	case *ast.IndexExpr:
		T := a.b.types[e.X].Type
		if T == nil {
			return nil
		}
		if _, ok := unwrap(T).Underlying().(*types2.Array); ok {
			return a.rootVar(e.X)
		}
	}
	return nil
}

func (a *addrScan) mark(e ast.Expr) {
	if v := a.rootVar(e); v != nil {
		a.out[v] = true
	}
}

func (a *addrScan) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.UnaryExpr:
		if n.Op == token.AND {
			a.mark(n.X)
		}
	case *ast.AssignStmt:
		for _, l := range n.Lhs {
			if _, ok := ast.Unparen(l).(*ast.Ident); !ok {
				a.mark(l)
			} else {
				a.assign(l)
			}
		}
	case *ast.OrStmt:
		for _, l := range n.Lhs {
			if _, ok := ast.Unparen(l).(*ast.Ident); !ok {
				a.mark(l)
			} else {
				a.assign(l)
			}
		}
	case *ast.IncDecStmt:
		if _, ok := ast.Unparen(n.X).(*ast.Ident); !ok {
			a.mark(n.X)
		} else {
			a.assign(n.X)
		}
	case *ast.FuncDecl:
		a.results(n.Type)
	case *ast.SliceExpr:
		a.mark(n.X)
	case *ast.SelectorExpr:
		sel := a.b.selections[n]
		if sel != nil && (sel.Kind() == types2.MethodVal || sel.Kind() == types2.FieldVal && len(sel.Index()) > 1) {
			// A method may take a pointer receiver, reached through
			// embedded fields; a promoted field may be reached through
			// an embedded pointer, which the path loads.
			a.mark(n.X)
		}
	case *ast.IndexExpr:
		if T := a.b.types[n.X].Type; T != nil && !a.b.types[n.X].IsType() && types2.IndexesByMethod(T) {
			a.mark(n.X)
		}
	case *ast.CallExpr:
		if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok && len(n.Args) == 1 {
			if bi, ok := a.b.uses[id].(*types2.Builtin); ok && bi.Name() == "len" {
				if T := a.b.types[n.Args[0]].Type; T != nil && types2.LenByMethod(T) {
					a.mark(n.Args[0])
				}
			}
		}
	case *ast.RangeStmt:
		// The loop's variables get a new value every iteration.
		for _, k := range []ast.Expr{n.Key, n.Value} {
			if id, ok := k.(*ast.Ident); ok {
				if v := a.b.localVar(id); v != nil {
					a.assigned[v] = true
				}
			}
		}
		if T := a.b.types[n.X].Type; T != nil && !a.b.types[n.X].IsType() {
			if types2.RangesByMethod(T) {
				a.mark(n.X)
			}
			if a.b.isRangeFunc(n) {
				a.captures(n, n.Body, a.rangeOwn(n))
				if n.Tok != token.DEFINE {
					for _, k := range []ast.Expr{n.Key, n.Value} {
						if k != nil {
							a.mark(k)
						}
					}
				}
			}
		}
	case *ast.FuncLit:
		a.results(n.Type)
		if a.b.scoped != nil && a.b.scoped.Lits[n] != nil {
			// A scoped task's literal: its Scope holds the addresses of
			// what it captures (scopedTask), for the lifetime checks.
			for _, v := range a.b.scoped.Lits[n].Captures {
				a.out[v] = true
			}
		}
		a.captures(n, n.Body, nil)
	}
	return true
}

// rangeOwn returns the variables a range-over-func loop declares, which are
// the body function's parameters.
func (a *addrScan) rangeOwn(st *ast.RangeStmt) map[*types2.Var]bool {
	own := map[*types2.Var]bool{}
	if st.Tok == token.DEFINE {
		for _, k := range []ast.Expr{st.Key, st.Value} {
			if id, ok := k.(*ast.Ident); ok {
				if v, ok := a.b.defs[id].(*types2.Var); ok {
					own[v] = true
				}
			}
		}
	}
	return own
}

// captures marks the variables that body, nested in node, uses but that
// are declared outside node (and are not own).
func (a *addrScan) captures(node ast.Node, body ast.Node, own map[*types2.Var]bool) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v := a.b.localVar(id)
		if v == nil || own[v] {
			return true
		}
		if v.Pos() < node.Pos() || v.Pos() >= node.End() {
			a.captured[v] = true
		}
		return true
	})
}

// isRangeFunc reports whether st ranges over a function, or over a
// container's All method, whose result is one: its body is a function of
// its own (section 12.11).
func (b *builder) isRangeFunc(st *ast.RangeStmt) bool {
	tv := b.types[st.X]
	if tv.Type == nil || tv.IsType() {
		return false
	}
	if types2.RangesByMethod(tv.Type) {
		return true
	}
	return signatureOf(tv.Type) != nil
}
