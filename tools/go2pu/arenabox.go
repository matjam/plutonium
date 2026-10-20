package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// Values converted to an interface that Go moves to the heap.
//
// A value that does not fit the data word of an interface is boxed by the
// compiler in the frame that makes the conversion (language spec section
// 18.3). Go boxes it on the heap when it outlives the frame, and says so
// ("e escapes to heap"). Where the converted value is stored, passed to a
// function value or returned in an interface the package asserts, the
// translation allocates the box in the arena instead: pu.Box(e) is an
// interface of e's own type whose box lives as long as the arena, and for an
// interface with methods the assertion .(I) makes the type the context
// wants. Calls of functions outside the compiler's own packages (fmt and
// the like) keep the frame box: they do not keep their arguments, and
// boxing every operand of every message would fill the arena.

// arenaBox returns the translation of e if e is converted to an interface and
// Go moves its box to the heap.
func (t *fileTr) arenaBox(e ast.Expr) (string, bool) {
	if t.arenaBoxing[e] || t.boxed[e] {
		return "", false
	}
	tv, ok := t.info.Types[e]
	if !ok || tv.Type == nil || tv.IsType() || tv.IsNil() || tv.Value != nil {
		return "", false
	}
	if direct(tv.Type) {
		return "", false
	}
	target := t.convTarget(e)
	if target == nil {
		return "", false
	}
	if _, ok := target.Underlying().(*types.Interface); !ok {
		return "", false
	}
	heap := false
	for _, p := range diagPositions(e) {
		heap = heap || t.esc.has(t.fset, p, "", "escapes to heap")
	}
	if !heap {
		return "", false
	}
	var assert string
	if iface := target.Underlying().(*types.Interface); !iface.Empty() {
		switch n := target.(type) {
		case *types.Named:
			if n.Obj().Pkg() != nil && n.Obj().Pkg() != t.pkg {
				return "", false
			}
			assert = ".(" + goName(n.Obj().Name()) + ")"
		default:
			return "", false
		}
	}
	if t.arenaBoxing == nil {
		t.arenaBoxing = map[ast.Expr]bool{}
	}
	t.arenaBoxing[e] = true
	inner := t.render(e)
	delete(t.arenaBoxing, e)
	return t.use(puPkg, "pu") + ".Box(" + inner + ")" + assert, true
}

// convTarget returns the type the value of e is converted to by its context
// (the parameter it is passed to, the variable it is assigned to), or nil.
func (t *fileTr) convTarget(e ast.Expr) types.Type {
	switch p := t.parent[e].(type) {
	case *ast.CallExpr:
		if p.Fun == e {
			return nil
		}
		for i, a := range p.Args {
			if a != e {
				continue
			}
			if !t.callKeeps(p) {
				return nil
			}
			sig, _ := t.typeOf(p.Fun).Underlying().(*types.Signature)
			if sig == nil || p.Ellipsis.IsValid() {
				return nil
			}
			n := sig.Params().Len()
			switch {
			case sig.Variadic() && i >= n-1:
				if s, ok := sig.Params().At(n - 1).Type().(*types.Slice); ok {
					return s.Elem()
				}
			case i < n:
				return sig.Params().At(i).Type()
			}
		}
	case *ast.AssignStmt:
		if p.Tok != token.ASSIGN || len(p.Lhs) != len(p.Rhs) {
			return nil
		}
		for i, r := range p.Rhs {
			if r == e {
				if id, ok := p.Lhs[i].(*ast.Ident); ok && id.Name == "_" {
					return nil
				}
				return t.info.TypeOf(p.Lhs[i])
			}
		}
	}
	return nil
}

// callKeeps reports whether a call may keep its arguments as far as the
// translation can tell: it is not a call of a function of a package outside
// the compiler.
func (t *fileTr) callKeeps(c *ast.CallExpr) bool {
	var id *ast.Ident
	switch f := ast.Unparen(c.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	}
	if id == nil {
		return true
	}
	fn, ok := t.info.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return true // a func value, or a builtin
	}
	return strings.HasPrefix(fn.Pkg().Path(), goModule)
}

// diagPositions returns the positions Go may report a conversion of e at: the
// start of e, and the token its compiler node is placed at (the brace of a
// composite literal, the parenthesis of a call, the dot of a selector, the
// operator of a binary expression, the bracket of an index).
func diagPositions(e ast.Expr) []token.Pos {
	out := []token.Pos{e.Pos()}
	switch e := e.(type) {
	case *ast.CompositeLit:
		out = append(out, e.Lbrace)
	case *ast.CallExpr:
		out = append(out, e.Lparen)
	case *ast.SelectorExpr:
		out = append(out, e.Sel.Pos(), e.Sel.Pos()-1)
	case *ast.BinaryExpr:
		out = append(out, e.OpPos)
	case *ast.IndexExpr:
		out = append(out, e.Lbrack)
	case *ast.SliceExpr:
		out = append(out, e.Lbrack)
	case *ast.ParenExpr:
		out = append(out, e.Lparen)
	}
	return out
}
