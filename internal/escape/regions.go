package escape

import (
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Closures returned in regions (language spec section 4.5).
//
// A function whose results can hold a func value builds the closures it
// returns in a region of its caller's frame (internal/lower/regions.go),
// so a call of one returns memory of the calling frame: a loc of kind
// lkCtor at the call, holding what the arguments point to, since the
// captured variables in the region may be copies of them. Lowering checks
// what a function may return of its own frame (closureflow.go, E3100 to
// E3102); this analysis checks everything else a region's closure may
// reach, like any other view of the frame.

// regionSig reports whether a function of signature sig takes a region
// for the closures it returns: some result can hold a func value by value
// (lower/regions.go has the same rule).
func regionSig(sig *types2.Signature) bool {
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
		return !t.IsPointer() && holdsFunc(t.Elem(), seen)
	case *types2.TaggedUnion:
		for i := 0; i < t.NumVariants(); i++ {
			if v := t.Variant(i); v.HasPayload() && holdsFunc(v.Payload(), seen) {
				return true
			}
		}
	}
	return false
}

// targetBuilds reports whether a call may return a closure built in a
// region of the calling frame: its results can hold a func value, and a
// function it may call builds (function.builds, found where the analysis
// sees a function return a closure of its own frame).
func (w *walker) targetBuilds(t callTarget) bool {
	if !regionSig(t.sig) {
		return false
	}
	switch {
	case t.static != nil:
		return t.static.builds
	case t.method != "":
		for _, c := range w.a.ifaceCandidates(t.method, t.byName, t.sig) {
			if c.f.builds {
				return true
			}
		}
	case t.dynFunc:
		for _, fv := range w.a.funcCandidates(t.sig) {
			if fv.f.builds {
				return true
			}
		}
	}
	return false
}

// region returns what the closures a call builds in this frame point to:
// the region, which holds copies of the arguments. During package
// initialisation the region is static storage.
func (w *walker) region(e *ast.CallExpr, args []tset) tset {
	if w.static {
		for _, a := range args {
			w.store(tset{heapTok}, a, e.Pos(), dest{text: "a static closure"})
		}
		return tset{heapTok}
	}
	rec := w.tmp(e, lkCtor)
	for _, a := range args {
		w.addContents(rec, a)
	}
	return tset{frameTok(rec)}
}

// regionResults adds the region of call e to the results that can hold a
// func value.
func (w *walker) regionResults(e *ast.CallExpr, t callTarget, args []tset, res []tset) []tset {
	if len(res) == 0 || !w.targetBuilds(t) {
		return res
	}
	reg := w.region(e, args)
	for i := range res {
		if i < t.sig.Results().Len() && holdsFunc(t.sig.Results().At(i).Type(), map[types2.Type]bool{}) {
			res[i] = union(res[i], reg)
		}
	}
	return res
}

// closureLoc reports whether l is a closure record or a region: what a
// function returns of these is lowering's to check (E3100).
func closureLoc(l *loc) bool {
	return l.kind == lkClosure || l.kind == lkMethodValue || l.kind == lkCtor
}

// addrsCaptures reports whether the closures f returns may take the
// address of a variable they capture, which lives in the region: with &,
// by slicing an array, by calling a pointer method, or from a function
// literal inside them. Without that, they pass on copies of what the
// region holds, never the region itself. Only a function whose every
// return returns a function literal is examined.
func (a *analyzer) addrsCaptures(f *function) bool {
	if f.decl == nil || f.decl.Body == nil {
		return true
	}
	decl := f.decl
	var lits []*ast.FuncLit
	ok := true
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(x.Results) != 1 {
				ok = false
				return false
			}
			lit, isLit := ast.Unparen(x.Results[0]).(*ast.FuncLit)
			if !isLit {
				ok = false
				return false
			}
			lits = append(lits, lit)
		}
		return ok
	})
	if !ok || len(lits) == 0 {
		return true
	}
	for _, lit := range lits {
		if a.litAddrsCaptures(lit, decl) {
			return true
		}
	}
	return false
}

func (a *analyzer) litAddrsCaptures(lit *ast.FuncLit, decl *ast.FuncDecl) bool {
	captured := func(e ast.Expr) bool {
		for {
			switch x := e.(type) {
			case *ast.ParenExpr:
				e = x.X
			case *ast.SelectorExpr:
				e = x.X
			case *ast.IndexExpr:
				e = x.X
			case *ast.Ident:
				v, ok := a.uses[x].(*types2.Var)
				if !ok || v.IsField() {
					return false
				}
				p := v.Pos()
				return p >= decl.Pos() && p < decl.End() && (p < lit.Pos() || p >= lit.End())
			default:
				return false
			}
		}
	}
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.UnaryExpr:
			found = x.Op == token.AND && captured(x.X)
		case *ast.SliceExpr:
			_, arr := underlying(a.types[x.X].Type).(*types2.Array)
			found = arr && captured(x.X)
		case *ast.SelectorExpr:
			if sel := a.selections[x]; sel != nil && sel.Kind() == types2.MethodVal {
				_, ptrRecv := sel.Obj().Type().(*types2.Signature).Recv().Type().(*types2.Pointer)
				found = ptrRecv && pointerElem(a.types[x.X].Type) == nil && captured(x.X)
			}
		case *ast.FuncLit:
			found = a.capturesFrom(x, decl)
		}
		return !found
	})
	return found
}

// capturesFrom reports whether lit uses a variable declared in decl
// outside lit.
func (a *analyzer) capturesFrom(lit *ast.FuncLit, decl *ast.FuncDecl) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := a.uses[id].(*types2.Var); ok && !v.IsField() {
				p := v.Pos()
				if p >= decl.Pos() && p < decl.End() && (p < lit.Pos() || p >= lit.End()) {
					found = true
				}
			}
		}
		return !found
	})
	return found
}
