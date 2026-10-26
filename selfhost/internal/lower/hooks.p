// Translated by go2pu from internal/lower/hooks.go at the go-cutover tag; edited by hand since.

package lower

import (
	"selfhost/compat"
	"selfhost/internal/ssa"
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
	"selfhost/pu"
)

// Hook variables, for the call graph of the context check (context.go).
//
// A call through a func value may call every function of its type whose
// value the program takes. That is too coarse for a hook: an unexported
// package variable of func type that its package only assigns functions
// (or nil) to, calls, and compares with nil, such as the runtime's
// shutdownWaitFn, which startTasks sets to waitShutdown so that a program
// without tasks links no scheduler. Nothing else can read such a variable,
// so a call of it calls only the functions assigned to it, and a function
// whose value is taken only to be assigned to hooks is called only through
// them.

// hookInfo is what the hook variables of the program are.
type hookInfo struct {
	targets map[*types2.Var][]*ssa.Sym // the functions each hook may hold
	hidden  map[*ssa.Sym]bool          // functions whose value only hooks take
}

// hookVar returns the package-level variable of func type that x names,
// or nil.
func hookVar(info *types2.Info, x ast.Expr) *types2.Var {
	x = ast.Unparen(x)
	var id *ast.Ident
	switch x := x.(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	default:
		return nil
	}
	v, ok := info.Uses[id].(*types2.Var)
	if !ok || v.Pkg() == nil || v.Parent() != v.Pkg().Scope() {
		return nil
	}
	if _, ok := v.Type().Underlying().(*types2.Signature); !ok {
		return nil
	}
	return v
}

// plainFunc returns the function, not a method nor generic, that x names,
// or nil.
func plainFunc(info *types2.Info, x ast.Expr) *types2.Func {
	x = ast.Unparen(x)
	var id *ast.Ident
	switch x := x.(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	default:
		return nil
	}
	fn, ok := info.Uses[id].(*types2.Func)
	if !ok {
		return nil
	}
	sig := fn.Type().(*types2.Signature)
	if sig.Recv() != nil || sig.TypeParams().Len() > 0 {
		return nil
	}
	return fn
}

// findHooks scans the program for hook variables.
func (l *Lowerer) findHooks() *hookInfo {
	bad := map[*types2.Var]bool{}
	assigned := map[*types2.Var][]*types2.Func{}
	seen := map[*types2.Var]bool{}
	general := map[*types2.Func]bool{} // value taken other than into a hook
	intoHook := map[*types2.Func][]*types2.Var{}

	for _, p := range l.prog.Packages {
		info := p.Info
		// assign records that the value of rhs is stored in v.
		assign := func(v *types2.Var, rhs ast.Expr) {
			seen[v] = true
			if id, ok := ast.Unparen(rhs).(*ast.Ident); ok && id.Name == "nil" {
				if _, ok := info.Uses[id].(*types2.Nil); ok {
					return
				}
			}
			if fn := plainFunc(info, rhs); fn != nil {
				assigned[v] = compat.Append(assigned[v], fn)
				intoHook[fn] = compat.Append(intoHook[fn], v)
				return
			}
			bad[v] = true
		}
		for _, f := range p.Files {
			// handled holds the references whose use is accounted for.
			handled := map[ast.Expr]bool{}
			var stack []ast.Node
			ast.Inspect(f, func(n ast.Node) bool {
				if n == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				stack = compat.Append(stack, n)
				switch n := n.(type) {
				case *ast.AssignStmt:
					if n.Tok == token.ASSIGN && len(n.Lhs) == len(n.Rhs) {
						for i, lhs := range n.Lhs {
							if v := hookVar(info, lhs); v != nil {
								assign(v, n.Rhs[i])
								handled[ast.Unparen(lhs)] = true
								if plainFunc(info, n.Rhs[i]) != nil {
									handled[ast.Unparen(n.Rhs[i])] = true
								}
							}
						}
					}
				case *ast.ValueSpec:
					for i, name := range n.Names {
						v, ok := info.Defs[name].(*types2.Var)
						if !ok || v.Pkg() == nil || v.Parent() != v.Pkg().Scope() {
							continue
						}
						if _, ok := v.Type().Underlying().(*types2.Signature); !ok {
							continue
						}
						if v.Exported() {
							bad[v] = true
						}
						seen[v] = true
						if i < len(n.Values) {
							assign(v, n.Values[i])
							if plainFunc(info, n.Values[i]) != nil {
								handled[ast.Unparen(n.Values[i])] = true
							}
						} else if len(n.Values) > 0 {
							bad[v] = true
						}
					}
				case *ast.Ident, *ast.SelectorExpr:
					x := n.(ast.Expr)
					if id, ok := n.(*ast.Ident); ok {
						if info.Defs[id] != nil {
							return true
						}
						// pkg.Name is seen as the selector.
						if se, ok := stack[len(stack)-2].(*ast.SelectorExpr); ok && se.Sel == id {
							return true
						}
					}
					v, fn := hookVar(info, x), plainFunc(info, x)
					if v == nil && fn == nil || handled[x] || isCallee(stack, x) {
						return true
					}
					if v != nil {
						if !isNilCompare(stack, x) {
							bad[v] = true
						}
						return true
					}
					general[fn] = true
				}
				return true
			})
		}
	}

	h := pu.New(hookInfo{targets: map[*types2.Var][]*ssa.Sym{}, hidden: map[*ssa.Sym]bool{}})
	for v := range seen {
		if bad[v] || v.Exported() {
			continue
		}
		var syms []*ssa.Sym
		ok := true
		for _, fn := range assigned[v] {
			// The value's symbol is the function's own, not a thunk.
			sym := l.funcs[funcSymName(fn, nil)]
			if _, taken := l.funcValues[sym]; sym == nil || !taken {
				ok = false
				break
			}
			syms = compat.Append(syms, sym)
		}
		if ok {
			h.targets[v] = syms
		}
	}
	for fn, vs := range intoHook {
		if general[fn] {
			continue
		}
		ok := true
		for _, v := range vs {
			if _, good := h.targets[v]; !good {
				ok = false
			}
		}
		if sym := l.funcs[funcSymName(fn, nil)]; ok && sym != nil {
			h.hidden[sym] = true
		}
	}
	return h
}

// isCallee reports whether x, the top of stack, is the function of a call.
func isCallee(stack []ast.Node, x ast.Expr) bool {
	for i := len(stack) - 2; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ParenExpr:
			continue
		case *ast.CallExpr:
			return ast.Unparen(p.Fun) == x
		}
		return false
	}
	return false
}

// isNilCompare reports whether x, the top of stack, is compared with nil.
func isNilCompare(stack []ast.Node, x ast.Expr) bool {
	for i := len(stack) - 2; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ParenExpr:
			continue
		case *ast.BinaryExpr:
			if p.Op != token.EQL && p.Op != token.NEQ {
				return false
			}
			other := p.Y
			if ast.Unparen(p.Y) == x {
				other = p.X
			}
			id, ok := ast.Unparen(other).(*ast.Ident)
			return ok && id.Name == "nil"
		}
		return false
	}
	return false
}
