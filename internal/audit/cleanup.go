package audit

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// parents accounts for full-expression temporary cleanup. Literal bodies are
// independent functions; their temporaries never belong to their constructor.
func parents(root ast.Node) map[ast.Node]ast.Node {
	out := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if len(stack) > 0 {
			out[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return out
}

func (g *graph) cleanupCall(f *function, c *ast.CallExpr, ancestors map[ast.Node]ast.Node) {
	info := f.pkg.Info
	if b, ok := object(info, c.Fun).(*types2.Builtin); ok && len(c.Args) > 0 {
		switch b.Name() {
		case "clear", "copy":
			t := info.TypeOf(c.Args[0])
			if t != nil {
				switch t := t.Underlying().(type) {
				case *types2.Slice:
					g.addDrops(f, t.Elem(), c.Pos(), map[types2.Type]bool{})
				case *types2.Map:
					g.addDrops(f, t.Key(), c.Pos(), map[types2.Type]bool{})
					g.addDrops(f, t.Elem(), c.Pos(), map[types2.Type]bool{})
				}
			}
		case "delete":
			g.addDrops(f, info.TypeOf(c.Args[0]), c.Pos(), map[types2.Type]bool{})
		}
		return
	}
	// A counted call transfers its result directly to a return or stored
	// destination. Other fresh results have statement temporary slots.
	var transferred ast.Node = c
	for {
		p := ancestors[transferred]
		switch p.(type) {
		case *ast.ParenExpr, *ast.KeyValueExpr, *ast.CompositeLit:
			transferred = p
			continue
		}
		break
	}
	switch p := ancestors[transferred].(type) {
	case *ast.ReturnStmt:
		return
	case *ast.AssignStmt:
		if tuple, ok := info.TypeOf(c).(*types2.Tuple); ok && len(p.Rhs) == 1 {
			for i, lhs := range p.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
					g.addCountedDrops(f, tuple.At(i).Type(), c.Pos())
				}
			}
			return
		}
		if len(p.Lhs) == len(p.Rhs) {
			for i, r := range p.Rhs {
				if r == transferred {
					if id, ok := p.Lhs[i].(*ast.Ident); !ok || id.Name != "_" {
						return
					}
				}
			}
		}
	case *ast.ValueSpec:
		if tuple, ok := info.TypeOf(c).(*types2.Tuple); ok && len(p.Values) == 1 {
			for i, id := range p.Names {
				if id.Name == "_" {
					g.addCountedDrops(f, tuple.At(i).Type(), c.Pos())
				}
			}
			return
		}
		if len(p.Names) == len(p.Values) {
			for i, r := range p.Values {
				if r == transferred && p.Names[i].Name != "_" {
					return
				}
			}
		}
	case *ast.SendStmt:
		return
	}
	g.addCountedDrops(f, info.TypeOf(c), c.Pos())
}

func (g *graph) addCountedDrops(f *function, t types2.Type, pos token.Pos) {
	g.countedDrops(f, t, pos, map[types2.Type]bool{})
}

func (g *graph) countedDrops(f *function, t types2.Type, pos token.Pos, seen map[types2.Type]bool) {
	if t == nil {
		return
	}
	if seen[t] {
		return
	}
	seen[t] = true
	if tp, ok := types2.Unalias(t).(*types2.TypeParam); ok {
		for _, actual := range g.typeArgs[tp] {
			g.countedDrops(f, actual, pos, seen)
		}
		return
	}
	switch t := t.Underlying().(type) {
	case *types2.Wrapper:
		if t.IsCounted() {
			g.addDrops(f, t, pos, map[types2.Type]bool{})
		}
	case *types2.Map, *types2.Chan:
		g.addDrops(f, t, pos, map[types2.Type]bool{})
	case *types2.Interface:
		if types2.IsCountedError(t) {
			g.addDrops(f, t, pos, map[types2.Type]bool{})
		}
	case *types2.Struct:
		for i := 0; i < t.NumFields(); i++ {
			g.countedDrops(f, t.Field(i).Type(), pos, seen)
		}
	case *types2.Array:
		g.countedDrops(f, t.Elem(), pos, seen)
	case *types2.Tuple:
		for i := 0; i < t.Len(); i++ {
			g.countedDrops(f, t.At(i).Type(), pos, seen)
		}
	}
}
