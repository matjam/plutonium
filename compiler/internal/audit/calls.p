// Translated by go2pu from internal/audit/calls.go; edited by hand since.

package audit

import (
	"pluto/slices"
	"pluto/strings"
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
)

func unwrap(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		default:
			return e
		}
	}
}

func object(info *types2.Info, e ast.Expr) types2.Object {
	switch e := unwrap(e).(type) {
	case *ast.Ident:
		return info.ObjectOf(e)
	case *ast.SelectorExpr:
		if s := info.Selections[e]; s != nil {
			return s.Obj()
		}
		return info.ObjectOf(e.Sel)
	}
	return nil
}

func signature(t types2.Type) *types2.Signature {
	if t == nil {
		return nil
	}
	s, _ := t.Underlying().(*types2.Signature)
	return s
}

func (g *graph) collectValues() {
	for _, pkg := range g.prog.Packages {
		for _, s := range pkg.Info.Selections {
			if fn, ok := s.Obj().(*types2.Func); ok {
				g.receiverArguments(fn)
			}
		}
		for _, tv := range pkg.Info.Types {
			if tv.Type == nil {
				continue
			}
			t := types2.Unalias(tv.Type)
			if p, ok := t.(*types2.Pointer); ok {
				t = types2.Unalias(p.Elem())
			}
			if n, ok := t.(*types2.Named); ok {
				for i := 0; i < n.NumMethods(); i++ {
					g.receiverArguments(n.Method(i))
				}
			}
		}
		for id, inst := range pkg.Info.Instances {
			if fn, ok := pkg.Info.Uses[id].(*types2.Func); ok {
				sig := fn.Origin().Type().(*types2.Signature)
				for i := 0; i < sig.TypeParams().Len() && i < inst.TypeArgs.Len(); i++ {
					tp := sig.TypeParams().At(i)
					g.typeArgs[tp] = compat.Append(g.typeArgs[tp], inst.TypeArgs.At(i))
				}
			}
			if n, ok := pkg.Info.Uses[id].(*types2.TypeName); ok {
				if named, ok := n.Type().(*types2.Named); ok {
					for i := 0; i < named.TypeParams().Len() && i < inst.TypeArgs.Len(); i++ {
						tp := named.TypeParams().At(i)
						g.typeArgs[tp] = compat.Append(g.typeArgs[tp], inst.TypeArgs.At(i))
					}
				}
			}
		}
		called := map[*ast.Ident]bool{}
		calledSelectors := map[*ast.SelectorExpr]bool{}
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					switch e := unwrap(c.Fun).(type) {
					case *ast.Ident:
						called[e] = true
					case *ast.SelectorExpr:
						called[e.Sel] = true
						calledSelectors[e] = true
					}
				}
				return true
			})
		}
		for id, o := range pkg.Info.Uses {
			if called[id] {
				continue
			}
			if r := g.unsafeCall(pkg.Info, id); r != "" {
				if s := signature(pkg.Info.TypeOf(id)); s != nil {
					g.operations = compat.Append(g.operations, operation{s, r})
				}
			}
			if fn, ok := o.(*types2.Func); ok {
				if f := g.decls[fn.Origin()]; f != nil {
					s := signature(pkg.Info.TypeOf(id))
					if inst, ok := pkg.Info.Instances[id]; ok {
						s = signature(inst.Type)
					}
					g.values = compat.Append(g.values, value{f, s})
				}
			}
		}
		for e, s := range pkg.Info.Selections {
			if calledSelectors[e] {
				continue
			}
			if r := g.unsafeCall(pkg.Info, e); r != "" {
				if sig := signature(pkg.Info.TypeOf(e)); sig != nil {
					g.operations = compat.Append(g.operations, operation{sig, r})
				}
			}
			if fn, ok := s.Obj().(*types2.Func); ok {
				if f := g.decls[fn.Origin()]; f != nil {
					g.values = compat.Append(g.values, value{f, signature(pkg.Info.TypeOf(e))})
				} else if _, ok := s.Recv().Underlying().(*types2.Interface); ok {
					ts, _ := g.targets(pkg.Info, e)
					for _, f := range ts {
						g.values = compat.Append(g.values, value{f, signature(pkg.Info.TypeOf(e))})
					}
				}
			}
		}
	}
}

func (g *graph) receiverArguments(fn *types2.Func) {
	sig := fn.Type().(*types2.Signature)
	if sig.Recv() == nil {
		return
	}
	origin := fn.Origin().Type().(*types2.Signature)
	t := types2.Unalias(sig.Recv().Type())
	if p, ok := t.(*types2.Pointer); ok {
		t = types2.Unalias(p.Elem())
	}
	if n, ok := t.(*types2.Named); ok {
		for i := 0; i < origin.RecvTypeParams().Len() && i < n.TypeArgs().Len(); i++ {
			tp := origin.RecvTypeParams().At(i)
			g.typeArgs[tp] = compat.Append(g.typeArgs[tp], n.TypeArgs().At(i))
		}
	}
}

func (g *graph) targets(info *types2.Info, e ast.Expr) ([]*function, string) {
	if lit, ok := unwrap(e).(*ast.FuncLit); ok {
		return compat.Clone([]*function{g.literals[lit]}), "call"
	}
	if sel, ok := unwrap(e).(*ast.SelectorExpr); ok {
		if s := info.Selections[sel]; s != nil {
			if iface, ok := s.Recv().Underlying().(*types2.Interface); ok {
				var out []*function
				for _, f := range g.functions {
					if f.obj == nil || f.obj.Name() != sel.Sel.Name {
						continue
					}
					r := f.sig.Recv()
					if r == nil {
						continue
					}
					if types2.Implements(r.Type(), iface) {
						out = compat.Append(out, f)
					}
				}
				// Instantiated/promoted method selections carry the concrete
				// receiver even when the declaration's receiver is generic.
				for _, pkg := range g.prog.Packages {
					for _, x := range pkg.Info.Types {
						if x.Type == nil || !types2.Implements(x.Type, iface) {
							continue
						}
						if m, _, _ := types2.LookupFieldOrMethod(x.Type, true, s.Obj().Pkg(), sel.Sel.Name); m != nil {
							if m, ok := m.(*types2.Func); ok {
								if f := g.decls[m.Origin()]; f != nil {
									out = compat.Append(out, f)
								}
							}
						}
					}
				}
				return g.sorted(out), "may call"
			}
		}
	}
	if fn, ok := object(info, e).(*types2.Func); ok {
		if f := g.decls[fn.Origin()]; f != nil {
			return compat.Clone([]*function{f}), "call"
		}
		return nil, "call"
	}
	s := signature(info.TypeOf(e))
	if s == nil {
		return nil, "call"
	}
	var out []*function
	for _, v := range g.values {
		if v.sig != nil && sameSignature(s, v.sig) {
			out = compat.Append(out, v.fn)
		}
	}
	return g.sorted(out), "may call"
}

func sameSignature(a, b *types2.Signature) bool {
	// Receivers are supplied by method-value records; callable shapes compare
	// only explicit arguments and results, as in lowering's plainSig.
	return a.Variadic() == b.Variadic() && sameTuple(a.Params(), b.Params()) && sameTuple(a.Results(), b.Results())
}

func sameTuple(a, b *types2.Tuple) bool {
	if a.Len() != b.Len() {
		return false
	}
	for i := 0; i < a.Len(); i++ {
		if !possibleType(a.At(i).Type(), b.At(i).Type()) {
			return false
		}
	}
	return true
}

// Generic source bodies retain type parameters. Matching their constraints
// conservatively includes instantiated callbacks without inventing a main root.
func possibleType(a, b types2.Type) bool {
	a, b = types2.Unalias(a), types2.Unalias(b)
	if types2.Identical(a, b) {
		return true
	}
	if p, ok := a.(*types2.TypeParam); ok {
		return types2.Satisfies(b, p.Constraint().Underlying().(*types2.Interface))
	}
	if p, ok := b.(*types2.TypeParam); ok {
		return types2.Satisfies(a, p.Constraint().Underlying().(*types2.Interface))
	}
	switch a := a.(type) {
	case *types2.Pointer:
		b, ok := b.(*types2.Pointer)
		return ok && possibleType(a.Elem(), b.Elem())
	case *types2.Slice:
		b, ok := b.(*types2.Slice)
		return ok && possibleType(a.Elem(), b.Elem())
	case *types2.Array:
		b, ok := b.(*types2.Array)
		return ok && a.Len() == b.Len() && possibleType(a.Elem(), b.Elem())
	case *types2.Map:
		b, ok := b.(*types2.Map)
		return ok && possibleType(a.Key(), b.Key()) && possibleType(a.Elem(), b.Elem())
	case *types2.Chan:
		b, ok := b.(*types2.Chan)
		return ok && a.Dir() == b.Dir() && possibleType(a.Elem(), b.Elem())
	case *types2.Signature:
		b, ok := b.(*types2.Signature)
		return ok && sameSignature(a, b)
	case *types2.Named:
		b, ok := b.(*types2.Named)
		if !ok || a.Origin() != b.Origin() || a.TypeArgs().Len() != b.TypeArgs().Len() {
			return false
		}
		for i := 0; i < a.TypeArgs().Len(); i++ {
			if !possibleType(a.TypeArgs().At(i), b.TypeArgs().At(i)) {
				return false
			}
		}
		return true
	case *types2.Wrapper:
		b, ok := b.(*types2.Wrapper)
		return ok && a.Kind() == b.Kind() && a.N() == b.N() && possibleType(a.Elem(), b.Elem())
	}
	return false
}

func (g *graph) sorted(fs []*function) []*function {
	slices.SortFunc(fs, func(a, b *function) int { return strings.Compare(a.name, b.name) })
	return slices.Compact(fs)
}

func (g *graph) addCall(f *function, e ast.Expr, pos token.Pos, kind string) {
	ts, k := g.targets(f.pkg.Info, e)
	if kind == "" {
		kind = k
	} else if k == "may call" {
		kind = compat.Concat("may ", kind)
	}
	for _, t := range ts {
		if t != nil {
			f.calls = compat.Append(f.calls, edge{t, pos, kind})
		}
	}
}

// addDrops includes user-defined cleanup, without making ordinary allocation
// syntax unsafe because the compiler's own counting machinery uses raw memory.
func (g *graph) addDrops(f *function, t types2.Type, pos token.Pos, seen map[types2.Type]bool) {
	if t == nil || seen[t] {
		return
	}
	seen[t] = true
	if tp, ok := types2.Unalias(t).(*types2.TypeParam); ok {
		for _, actual := range g.typeArgs[tp] {
			g.addDrops(f, actual, pos, seen)
		}
		return
	}
	owner := types2.IsOwner(t)
	if m, _, _ := types2.LookupFieldOrMethod(t, true, f.pkg.Types, "Drop"); types2.IsOwner(t) && m != nil {
		if m, ok := m.(*types2.Func); ok {
			if target := g.decls[m.Origin()]; target != nil {
				f.calls = compat.Append(f.calls, edge{target, pos, "may drop"})
			}
		}
	}
	if A := types2.OwnerBase(t); A != nil {
		g.addDrops(f, A, pos, seen) // type B owner A drops as an A
	}
	switch t := t.Underlying().(type) {
	case *types2.Struct:
		for i := 0; i < t.NumFields(); i++ {
			g.addDrops(f, t.Field(i).Type(), pos, seen)
		}
	case *types2.Array:
		g.addDrops(f, t.Elem(), pos, seen)
	case *types2.Wrapper:
		if t.IsPointer() || t.Kind() == types2.WrapRing || t.Kind() == types2.WrapAligned {
			g.addDrops(f, t.Elem(), pos, seen)
		}
	case *types2.Map:
		g.addDrops(f, t.Key(), pos, seen)
		g.addDrops(f, t.Elem(), pos, seen)
	case *types2.Chan:
		g.addDrops(f, t.Elem(), pos, seen)
	case *types2.Interface:
		if owner || types2.IsCountedError(t) {
			for _, pkg := range g.prog.Packages {
				for _, x := range pkg.Info.Types {
					if x.Type == nil || !types2.Implements(x.Type, t) {
						continue
					}
					if types2.IsOwner(x.Type) {
						g.addDrops(f, x.Type, pos, seen)
					} else if w, ok := x.Type.Underlying().(*types2.Wrapper); ok && w.IsCounted() {
						g.addDrops(f, x.Type, pos, seen)
					}
				}
			}
		}
	}
}
