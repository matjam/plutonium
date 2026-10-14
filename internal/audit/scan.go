package audit

import (
	"slices"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

func (g *graph) scan(f *function) {
	info := f.pkg.Info
	parent := parents(f.body)
	results := map[*types2.Var]bool{}
	borrowed := map[*types2.Var]bool{}
	if f.sig != nil {
		for i := 0; i < f.sig.Params().Len(); i++ {
			v := f.sig.Params().At(i)
			if _, literal := f.body.(*ast.BlockStmt); literal {
				if types2.IsOwner(v.Type()) && g.mayDropVariable(f, v) {
					g.addDrops(f, v.Type(), f.pos, map[types2.Type]bool{})
				}
				if isUnsafePointer(v.Type()) {
					f.unsafe = append(f.unsafe, site{f.pos, "unsafe.Pointer parameter"})
				}
			}
			if !types2.IsOwner(v.Type()) {
				borrowed[v] = true
			}
		}
		if r := f.sig.Recv(); r != nil && !types2.IsOwner(r.Type()) {
			borrowed[r] = true
		}
		for i := 0; i < f.sig.Results().Len(); i++ {
			results[f.sig.Results().At(i)] = true
			if _, literal := f.body.(*ast.BlockStmt); literal && isUnsafePointer(f.sig.Results().At(i).Type()) {
				f.unsafe = append(f.unsafe, site{f.pos, "unsafe.Pointer result"})
			}
		}
	}
	ast.Inspect(f.body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if s, ok := n.(*ast.ValueSpec); ok && f.obj == nil {
			for i, id := range s.Names {
				if id.Name != "_" {
					continue
				}
				if len(s.Values) == len(s.Names) {
					e := s.Values[i]
					g.addDrops(f, info.TypeOf(e), e.Pos(), map[types2.Type]bool{})
				} else if len(s.Values) == 1 {
					if ts, ok := info.TypeOf(s.Values[0]).(*types2.Tuple); ok && i < ts.Len() {
						g.addDrops(f, ts.At(i).Type(), s.Values[0].Pos(), map[types2.Type]bool{})
					}
				}
			}
		}
		if id, ok := n.(*ast.Ident); ok {
			if isUnsafePointer(info.TypeOf(id)) {
				f.unsafe = append(f.unsafe, site{id.Pos(), "unsafe.Pointer"})
			}
			if v, ok := info.Defs[id].(*types2.Var); ok && v.Parent() != f.pkg.Types.Scope() && !results[v] && !borrowed[v] && g.mayDropVariable(f, v) {
				g.addDrops(f, v.Type(), id.Pos(), map[types2.Type]bool{})
			}
		}
		if call, ok := n.(*ast.CallExpr); ok {
			g.cleanupCall(f, call, parent)
			if reason := g.unsafeCall(info, call.Fun); reason != "" {
				f.unsafe = append(f.unsafe, site{call.Pos(), reason})
			}
			if _, direct := object(info, call.Fun).(*types2.Func); !direct {
				if sig := signature(info.TypeOf(call.Fun)); sig != nil {
					for _, op := range g.operations {
						if sameSignature(sig, op.sig) {
							f.unsafe = append(f.unsafe, site{call.Pos(), "may invoke " + op.reason})
						}
					}
				}
			}
			if o := object(info, call.Fun); o != nil && o.Pkg() == types2.Unsafe && (o.Name() == "Sizeof" || o.Name() == "Alignof" || o.Name() == "Offsetof") {
				return false
			}
			if tv, ok := info.Types[call.Fun]; !ok || !tv.IsType() {
				g.addCall(f, call.Fun, call.Pos(), "")
			}
			if g.moves != nil && g.moves.Temps[call] {
				g.addDrops(f, info.TypeOf(call), call.Pos(), map[types2.Type]bool{})
			}
		}
		if e, ok := n.(ast.Expr); ok && g.moves != nil && g.moves.Temps[e] {
			g.addDrops(f, info.TypeOf(e), e.Pos(), map[types2.Type]bool{})
		}
		if s, ok := n.(*ast.AssignStmt); ok && g.moves != nil && s.Tok == token.ASSIGN {
			for _, lhs := range s.Lhs {
				if !g.moves.Fresh[lhs] {
					g.addDrops(f, info.TypeOf(lhs), lhs.Pos(), map[types2.Type]bool{})
				}
			}
		}
		if r, ok := n.(*ast.RangeStmt); ok {
			t := info.TypeOf(r.X)
			if signature(t) != nil {
				g.addCall(f, r.X, r.Pos(), "iterate")
			} else if t != nil {
				if o, _, _ := types2.LookupFieldOrMethod(t, true, f.pkg.Types, "All"); o != nil {
					if o, ok := o.(*types2.Func); ok {
						if target := g.decls[o.Origin()]; target != nil {
							f.calls = append(f.calls, edge{target, r.Pos(), "iterate"})
							if sig := signature(o.Type()); sig != nil && sig.Results().Len() == 1 {
								if iter := signature(sig.Results().At(0).Type()); iter != nil {
									for _, v := range g.values {
										if v.sig != nil && sameSignature(iter, v.sig) {
											f.calls = append(f.calls, edge{v.fn, r.Pos(), "may iterate"})
										}
									}
								}
							}
						}
					}
				}
			}
		}
		return true
	})
	slices.SortFunc(f.unsafe, func(a, b site) int {
		if a.pos != b.pos {
			return int(a.pos - b.pos)
		}
		return strings.Compare(a.text, b.text)
	})
	f.unsafe = slices.Compact(f.unsafe)
	slices.SortFunc(f.calls, func(a, b edge) int {
		if a.pos != b.pos {
			return int(a.pos - b.pos)
		}
		return strings.Compare(a.target.name, b.target.name)
	})
	f.calls = slices.Compact(f.calls)
}

func (g *graph) mayDropVariable(f *function, v *types2.Var) bool {
	if g.moves == nil || !types2.IsOwner(v.Type()) {
		return true
	}
	if v.Parent() == nil {
		return false
	}
	may := false
	ast.Inspect(f.body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if n == nil || n.End() < v.Pos() {
			return true
		}
		if !v.Parent().Contains(n.End() - 1) {
			return true
		}
		switch n.(type) {
		case *ast.BlockStmt, *ast.ReturnStmt, *ast.BranchStmt:
			if b, ok := n.(*ast.BlockStmt); ok && len(b.List) > 0 {
				switch b.List[len(b.List)-1].(type) {
				case *ast.ReturnStmt, *ast.BranchStmt:
					return true
				}
			}
			if held, ok := g.moves.Empty[n][v]; !ok || len(held.Parts) > 0 {
				may = true
			}
		}
		return !may
	})
	return may
}

func isUnsafePointer(t types2.Type) bool {
	return unsafePointerType(t, map[types2.Type]bool{})
}

func unsafePointerType(t types2.Type, seen map[types2.Type]bool) bool {
	if t == nil {
		return false
	}
	if seen[t] {
		return false
	}
	seen[t] = true
	if tp, ok := types2.Unalias(t).(*types2.TypeParam); ok {
		return unsafePointerType(tp.Constraint(), seen)
	}
	switch t := t.Underlying().(type) {
	case *types2.Interface:
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if unsafePointerType(t.EmbeddedType(i), seen) {
				return true
			}
		}
	case *types2.Union:
		for i := 0; i < t.Len(); i++ {
			if unsafePointerType(t.Term(i).Type(), seen) {
				return true
			}
		}
	}
	b, ok := t.Underlying().(*types2.Basic)
	return ok && b.Kind() == types2.UnsafePointer
}

func (g *graph) unsafeCall(info *types2.Info, e ast.Expr) string {
	o := object(info, e)
	if o != nil && o.Pkg() != nil {
		p, n := o.Pkg().Path(), o.Name()
		switch {
		case p == "unsafe":
			switch n {
			case "Sizeof", "Alignof", "Offsetof":
				return "unsafe." + n + " (layout query)"
			case "Add", "Slice", "String", "SliceData", "StringData", "Drop":
				return "unsafe." + n
			}
		case p == "pluto/arch/x86":
			return p + "." + n
		case p == "pluto/alloc" && (n == "Raw" || n == "New" || n == "Free"):
			return p + "." + n
		case p == "pluto/rt" && n == "Pinned":
			return "pluto/rt.Pinned"
		case p == "pluto/arena":
			if fn, ok := o.(*types2.Func); ok {
				if reason := manualArenaOperation(fn); reason != "" {
					return reason
				}
			}
		}
	}
	if sel, ok := unwrap(e).(*ast.SelectorExpr); ok && g.allocator != nil {
		if s := info.Selections[sel]; s != nil {
			if (types2.Implements(s.Recv(), g.allocator) || types2.Implements(types2.NewPointer(s.Recv()), g.allocator)) && (s.Obj().Name() == "Alloc" || s.Obj().Name() == "Free") {
				return "direct Allocator." + s.Obj().Name()
			}
		}
	}
	return ""
}

// Manual arena operations are unsafe API declarations under section 18.12.
// Methods belong to the actual Arena origin; Fixed and same-name declarations
// do not acquire the manual contract from their spelling or signature.
func manualArenaOperation(fn *types2.Func) string {
	fn = fn.Origin()
	sig := fn.Type().(*types2.Signature)
	if sig.Recv() == nil {
		if fn.Name() == "NewUnsafe" && fn.Pkg().Scope().Lookup("NewUnsafe") == fn {
			return "pluto/arena.NewUnsafe"
		}
		return ""
	}
	switch fn.Name() {
	case "New", "Slice", "Reset", "Release":
	default:
		return ""
	}
	recv := types2.Unalias(sig.Recv().Type())
	if p, ok := recv.(*types2.Pointer); ok {
		recv = types2.Unalias(p.Elem())
	}
	named, ok := recv.(*types2.Named)
	if !ok {
		return ""
	}
	obj, ok := fn.Pkg().Scope().Lookup("Arena").(*types2.TypeName)
	if !ok {
		return ""
	}
	arena, ok := types2.Unalias(obj.Type()).(*types2.Named)
	if !ok || named.Origin() != arena.Origin() {
		return ""
	}
	return "manual Arena." + fn.Name()
}
