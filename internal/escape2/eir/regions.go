package eir

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
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

// planRegion finds the closures a function with a region returns directly,
// and the variables they capture, in its own frame (not in nested
// literals).
func (fb *fnb) planRegion(sig *types2.Signature, body *ast.BlockStmt) {
	if !regionSig(sig) || body == nil {
		return
	}
	fb.regionNodes = map[ast.Node]bool{}
	fb.regionVars = map[*types2.Var]bool{}
	var mark func(e ast.Expr)
	mark = func(e ast.Expr) {
		switch x := ast.Unparen(e).(type) {
		case *ast.FuncLit:
			fb.regionNodes[x] = true
			ast.Inspect(x.Body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					if v := fb.b.localVar(id); v != nil && (v.Pos() < x.Pos() || v.Pos() >= x.End()) && v.Pos() >= body.Pos() || isParamOf(sig, v) {
						fb.regionVars[v] = true
					}
				}
				return true
			})
		case *ast.SelectorExpr:
			if sel := fb.b.selections[x]; sel != nil && sel.Kind() == types2.MethodVal {
				fb.regionNodes[x] = true
			}
		case *ast.CallExpr:
			if tv := fb.b.types[x.Fun]; tv.IsType() && len(x.Args) == 1 {
				mark(x.Args[0])
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
				mark(r)
			}
		}
		return true
	})
}

// isParamOf reports whether v is a parameter or the receiver of sig.
func isParamOf(sig *types2.Signature, v *types2.Var) bool {
	if r := sig.Recv(); r == v {
		return true
	}
	for i := 0; i < sig.Params().Len(); i++ {
		if sig.Params().At(i) == v {
			return true
		}
	}
	return false
}
