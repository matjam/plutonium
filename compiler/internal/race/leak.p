package race

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
)

// Leaks of owned memory (language spec sections 8.8 and 19.3). The memory
// a view field of an owner holds moves with the owner only if nothing else
// keeps it: an alias kept beyond the owner would let another task use it
// after the transfer. findLeaks follows each load of a view field of an
// owner, and the local variables that take its value, to where the value
// goes. Kept in a package variable, in memory that is not the owner's, in
// a map, sent on a channel, given to a task or captured by a function
// literal, the memory leaks and the field does not own it (ownedView).
// A value a function returns, or passes to a call, is left to the moves
// checker, which follows the views of a local owner (moves/views.p).

// findLeaks computes ov.leaked.
func (ov *ownedViews) findLeaks() {
	ov.leaked = map[*types2.Var]bool{}
	ov.derived = map[*types2.Var][]*types2.Var{}
	ov.derivedIn = map[*types2.Var]ast.Node{}
	var work []*types2.Var
	for _, p := range ov.a.prog.Packages {
		for _, file := range p.Files {
			for _, d := range file.Decls {
				ov.walkUses(d, d, func(n ast.Node) []*types2.Var {
					if x, ok := n.(*ast.SelectorExpr); ok && ov.ownerField(x) {
						var fs []*types2.Var
						return compat.Append(fs, ov.fieldOf(x))
					}
					return nil
				}, &work)
			}
		}
	}
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		ov.walkUses(ov.derivedIn[v], ov.derivedIn[v], func(n ast.Node) []*types2.Var {
			if id, ok := n.(*ast.Ident); ok && ov.a.uses[id] == v {
				return ov.derived[v]
			}
			return nil
		}, &work)
	}
}

// walkUses visits root, whose top-level declaration is decl, and follows
// each node for which seed gives the fields whose memory it may be.
func (ov *ownedViews) walkUses(root, decl ast.Node, seed func(ast.Node) []*types2.Var, work *[]*types2.Var) {
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = compat.Append(stack, n)
		if fs := seed(n); len(fs) > 0 {
			ov.follow(stack, decl, fs, work)
		}
		return true
	})
}

// follow climbs from the last node of stack, which may be memory of the
// fields fs, to where its value goes.
func (ov *ownedViews) follow(stack []ast.Node, decl ast.Node, fs []*types2.Var, work *[]*types2.Var) {
	leak := func() {
		for _, f := range fs {
			ov.leaked[f] = true
		}
	}
	if id, ok := stack[len(stack)-1].(*ast.Ident); ok {
		// A local variable used in a function literal that does not
		// declare it: the literal may keep it.
		if v := ov.varOf(id); v != nil {
			for _, n := range stack {
				if lit, ok := n.(*ast.FuncLit); ok && (v.Pos() < lit.Pos() || v.Pos() >= lit.End()) {
					leak()
					return
				}
			}
		}
	}
	for i := len(stack) - 1; i > 0; i-- {
		e, parent := stack[i], stack[i-1]
		switch p := parent.(type) {
		case *ast.ParenExpr, *ast.SliceExpr, *ast.CompositeLit, *ast.KeyValueExpr, *ast.TypeAssertExpr:
			continue
		case *ast.UnaryExpr:
			if p.Op == token.AND {
				continue
			}
			return
		case *ast.StarExpr:
			if mayHoldPtr(ov.a.typeOf(p)) {
				continue
			}
			return
		case *ast.SelectorExpr:
			if p.X == e && ov.fieldOf(p) != nil && mayHoldPtr(ov.a.typeOf(p)) {
				continue
			}
			return // a method value, given its receiver as a call is
		case *ast.IndexExpr:
			if p.X == e {
				if mayHoldPtr(ov.a.typeOf(p)) {
					continue
				}
				return
			}
			if _, ok := underlying(ov.a.typeOf(p.X)).(*types2.Map); ok {
				leak() // a map key
			}
			return
		case *ast.CallExpr:
			if p.Fun == e {
				return
			}
			if tv := ov.a.types[ast.Unparen(p.Fun)]; tv.IsType() {
				continue // a conversion
			}
			if b, ok := ov.a.uses[calleeIdent(p)].(*types2.Builtin); ok {
				switch b.Name() {
				case "append", "Slice", "Add":
					continue
				case "copy":
					if len(p.Args) == 2 && p.Args[1] == e {
						ov.storeLeaks(p.Args[0], decl, fs, work)
					}
				}
				return
			}
			if i > 1 {
				if _, ok := stack[i-2].(*ast.TaskStmt); ok {
					leak() // given to a task
				}
			}
			return
		case *ast.SendStmt:
			if p.Value == e {
				leak()
			}
			return
		case *ast.AssignStmt:
			if p.Tok != token.ASSIGN && p.Tok != token.DEFINE {
				return
			}
			for j, r := range p.Rhs {
				if r != e {
					continue
				}
				if len(p.Rhs) == len(p.Lhs) {
					ov.storeLeaks(p.Lhs[j], decl, fs, work)
				} else {
					for _, l := range p.Lhs {
						ov.storeLeaks(l, decl, fs, work)
					}
				}
			}
			return
		case *ast.ValueSpec:
			for j, r := range p.Values {
				if r != e {
					continue
				}
				if len(p.Values) == len(p.Names) {
					ov.storeLeaks(p.Names[j], decl, fs, work)
				} else {
					for _, l := range p.Names {
						ov.storeLeaks(l, decl, fs, work)
					}
				}
			}
			return
		case *ast.RangeStmt:
			if p.X == e {
				for _, l := range []ast.Expr{p.Key, p.Value} {
					if l != nil {
						ov.storeLeaks(l, decl, fs, work)
					}
				}
			}
			return
		default:
			return
		}
	}
}

// storeLeaks handles a store of memory of the fields fs in l.
func (ov *ownedViews) storeLeaks(l ast.Expr, decl ast.Node, fs []*types2.Var, work *[]*types2.Var) {
	leak := func() {
		for _, f := range fs {
			ov.leaked[f] = true
		}
	}
	switch x := ast.Unparen(l).(type) {
	case *ast.Ident:
		if x.Name == "_" {
			return
		}
		v := ov.varOf(x)
		if v == nil || isPackageVar(v) {
			leak()
			return
		}
		ov.derive(v, decl, fs, work)
	case *ast.SelectorExpr:
		if ov.fieldOf(x) == nil {
			leak() // a package variable of another package
			return
		}
		if ov.ownerField(x) {
			return // the memory of an owner
		}
		if pointerElem(ov.a.typeOf(x.X)) != nil {
			if !ov.ownedBase(x.X) {
				leak()
			}
			return
		}
		ov.storeLeaks(x.X, decl, fs, work)
	case *ast.IndexExpr:
		switch underlying(ov.a.typeOf(x.X)).(type) {
		case *types2.Array:
			ov.storeLeaks(x.X, decl, fs, work)
		case *types2.Slice, *types2.Pointer:
			if !ov.ownedBase(x.X) {
				leak()
			}
		default:
			leak()
		}
	case *ast.StarExpr:
		if !ov.ownedBase(x.X) {
			leak()
		}
	default:
		leak()
	}
}

// derive records that the local variable v, of declaration decl, may hold
// memory of the fields fs.
func (ov *ownedViews) derive(v *types2.Var, decl ast.Node, fs []*types2.Var, work *[]*types2.Var) {
	added := false
	for _, f := range fs {
		if !containsVar(ov.derived[v], f) {
			ov.derived[v] = compat.Append(ov.derived[v], f)
			added = true
		}
	}
	if added {
		ov.derivedIn[v] = decl
		*work = compat.Append(*work, v)
	}
}

// ownedBase reports whether e is memory of an owner: a load of a view
// field of one, a local variable that holds such memory, or memory
// reached through it.
func (ov *ownedViews) ownedBase(e ast.Expr) bool {
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.SelectorExpr:
			if ov.fieldOf(x) == nil {
				return false
			}
			if ov.ownerField(x) {
				return true
			}
			e = x.X
		case *ast.Ident:
			v := ov.varOf(x)
			return v != nil && !isPackageVar(v) && (len(ov.derived[v]) > 0 || ov.intoOwner[v])
		case *ast.IndexExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.SliceExpr:
			e = x.X
		default:
			return false
		}
	}
}

// ownerField reports whether x selects a view field of an owner.
func (ov *ownedViews) ownerField(x *ast.SelectorExpr) bool {
	sel := ov.a.selections[x]
	if sel == nil || sel.Kind() != types2.FieldVal {
		return false
	}
	f, _ := sel.Obj().(*types2.Var)
	if f == nil || !isView(f.Type()) {
		return false
	}
	T := sel.Recv()
	idx := sel.Index()
	for i := 0; i < len(idx)-1; i++ {
		if el := pointerElem(T); el != nil {
			T = el
		}
		st, ok := underlying(T).(*types2.Struct)
		if !ok {
			return false
		}
		T = st.Field(idx[i]).Type()
	}
	if el := pointerElem(T); el != nil {
		T = el
	}
	return types2.IsOwner(T)
}

// containsVar reports whether vs holds v.
func containsVar(vs []*types2.Var, v *types2.Var) bool {
	for _, w := range vs {
		if w == v {
			return true
		}
	}
	return false
}
