// Translated by go2pu from internal/escape2/eir/regions.go at the go-cutover tag; edited by hand since.

package eir

import (
	"compiler/internal/syntax/ast"
	"compiler/internal/types2"
)

// Closure regions (section 11.5). A function whose results can hold a
// func value takes a region from its caller, a block of the caller's frame,
// and builds there the closures that may reach its results, with the
// variables they capture: those live as long as the caller's frame
// (LifeCaller). The lowerer finds every closure that may reach a result
// (lower/closureflow.go); EIR marks the ones a return statement returns
// directly (a function literal, a conversion of one, a method value) and
// their captured variables. That is a subset of the lowerer's: every
// object EIR puts in the region the lowerer puts there too, so EIR never
// takes a frame object for the caller's; a closure the lowerer also puts
// there and EIR does not is only a false rejection, until stage 4 follows
// the closures through variables and calls.

// regionSig reports whether a function of signature sig takes a region: a
// result can hold a func value without a pointer in between
// (lower/regions.go regionSig).
func regionSig(sig *types2.Signature) bool {
	if sig == nil {
		return false
	}
	r := sig.Results()
	for i := 0; i < r.Len(); i++ {
		if holdsFunc(r.At(i).Type(), map[types2.Type]bool{}) {
			return true
		}
	}
	return false
}

func holdsFunc(T types2.Type, seen map[types2.Type]bool) bool {
	if seen[T] {
		return false
	}
	seen[T] = true
	switch t := types2.Unalias(T).Underlying().(type) {
	case *types2.Signature, *types2.Interface, *types2.TypeParam:
		return true
	case *types2.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if holdsFunc(t.Field(i).Type(), seen) {
				return true
			}
		}
	case *types2.Array:
		return holdsFunc(t.Elem(), seen)
	case *types2.Wrapper:
		return !t.IsBox() && holdsFunc(t.Elem(), seen)
	case *types2.TaggedUnion:
		for i := 0; i < t.NumVariants(); i++ {
			if holdsFunc(t.Variant(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}

// planRegion finds the closures a function with a region builds that
// reach its results, and the variables they capture, in its own frame (not
// in nested literals). The closures are followed as the lowerer follows
// them (lower/closureflow.go), but only through local variables (assigned
// whole, or a field or element of them), named results, composite
// literals and conversions, flow-insensitively: a subset of what the
// lowerer puts in the region.
func (fb *fnb) planRegion(sig *types2.Signature, body *ast.BlockStmt) {
	if !regionSig(sig) || body == nil {
		return
	}
	fb.regionNodes = map[ast.Node]bool{}
	fb.regionVars = map[*types2.Var]bool{}

	// The closures each local variable may hold, from the assignments of
	// the whole body.
	held := map[*types2.Var]map[ast.Node]bool{}
	var origins func(e ast.Expr, out map[ast.Node]bool)
	origins = func(e ast.Expr, out map[ast.Node]bool) {
		switch x := ast.Unparen(e).(type) {
		case *ast.FuncLit:
			out[x] = true
		case *ast.SelectorExpr:
			if sel := fb.b.selections[x]; sel != nil && sel.Kind() == types2.MethodVal {
				out[x] = true
			}
		case *ast.Ident:
			if v := fb.b.localVar(x); v != nil {
				for n := range held[v] {
					out[n] = true
				}
			}
		case *ast.CallExpr:
			if tv := fb.b.types[x.Fun]; tv.IsType() && len(x.Args) == 1 {
				origins(x.Args[0], out)
			}
		case *ast.CompositeLit:
			for _, elt := range x.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					elt = kv.Value
				}
				origins(elt, out)
			}
		}
	}
	// rootVar returns the local variable x, x.f, x[i] (on an array or
	// struct, no load) is part of, or nil.
	var rootVar func(e ast.Expr) *types2.Var
	rootVar = func(e ast.Expr) *types2.Var {
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			return fb.b.localVar(x)
		case *ast.SelectorExpr:
			if sel := fb.b.selections[x]; sel != nil && sel.Kind() == types2.FieldVal && !sel.Indirect() {
				return rootVar(x.X)
			}
		case *ast.IndexExpr:
			if _, ok := coreType(unwrap(fb.typeOf(x.X))).(*types2.Array); ok {
				return rootVar(x.X)
			}
		}
		return nil
	}
	flow := func(lhs ast.Expr, rhs ast.Expr) bool {
		v := rootVar(lhs)
		if v == nil {
			return false
		}
		got := map[ast.Node]bool{}
		origins(rhs, got)
		changed := false
		for n := range got {
			if held[v] == nil {
				held[v] = map[ast.Node]bool{}
			}
			if !held[v][n] {
				held[v][n] = true
				changed = true
			}
		}
		return changed
	}
	for changed := true; changed; {
		changed = false
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				if len(n.Lhs) == len(n.Rhs) {
					for i := range n.Lhs {
						if flow(n.Lhs[i], n.Rhs[i]) {
							changed = true
						}
					}
				}
			case *ast.ValueSpec:
				if len(n.Names) == len(n.Values) {
					for i := range n.Names {
						if flow(n.Names[i], n.Values[i]) {
							changed = true
						}
					}
				}
			}
			return true
		})
	}

	results := map[ast.Node]bool{}
	for i := 0; i < sig.Results().Len(); i++ {
		if v := sig.Results().At(i); v.Name() != "" && v.Name() != "_" {
			for n := range held[v] {
				results[n] = true
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.RangeStmt:
			if fb.b.isRangeFunc(n) {
				return false
			}
		case *ast.ReturnStmt:
			for _, r := range n.Results {
				origins(r, results)
			}
		}
		return true
	})
	for n := range results {
		if !fb.madeHere(n, body) {
			continue
		}
		fb.regionNodes[n] = true
		if x, ok := n.(*ast.FuncLit); ok {
			ast.Inspect(x.Body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					if v := fb.b.localVar(id); v != nil && (v.Pos() < x.Pos() || v.Pos() >= x.End()) && v.Pos() >= body.Pos() || isParamOf(sig, v) {
						fb.regionVars[v] = true
					}
				}
				return true
			})
		}
	}
}

// madeHere reports whether the closure node n is built by the function
// whose body is body itself: not inside a nested literal or range body,
// which build their own.
func (fb *fnb) madeHere(n ast.Node, body *ast.BlockStmt) bool {
	here := true
	ast.Inspect(body, func(m ast.Node) bool {
		if !here || m == nil {
			return false
		}
		if m.Pos() > n.Pos() || m.End() < n.End() {
			return false // does not contain n
		}
		if m == n {
			return false
		}
		switch m := m.(type) {
		case *ast.FuncLit:
			here = false
		case *ast.RangeStmt:
			if fb.b.isRangeFunc(m) {
				here = false
			}
		}
		return true
	})
	return here
}

// isParamOf reports whether v is a parameter, a named result or the
// receiver of sig (a named result a returned closure captures is in the
// region too: lower/func.go).
func isParamOf(sig *types2.Signature, v *types2.Var) bool {
	if v == nil {
		return false
	}
	if r := sig.Recv(); r == v {
		return true
	}
	for i := 0; i < sig.Params().Len(); i++ {
		if sig.Params().At(i) == v {
			return true
		}
	}
	for i := 0; i < sig.Results().Len(); i++ {
		if sig.Results().At(i) == v {
			return true
		}
	}
	return false
}
