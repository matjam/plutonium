package escape

import (
	"fmt"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Closures returned in regions (language spec section 11.5).
//
// A function whose results can hold a func value builds the closures it
// returns in a region of its caller's frame (internal/lower/regions.go),
// so a call of one returns memory of the calling frame: a loc of kind
// lkCtor at the call, holding what the arguments point to, since the
// captured variables in the region may be copies of them. Lowering checks
// what a function may return of its own frame (closureflow.go, E3100 to
// E3102); this analysis checks everything else a region's closure may
// reach, like any other view of the frame.
//
// The region holds more than copies of the arguments: the closure records
// the function returns, the variables their literals use, and the regions
// of the calls whose closures they hold. What the function returns may
// reach nothing else of its frame (regionReturn). Calling one of the
// closures, or ranging over it, may give back memory of the region itself
// when its literal passes on what it captures by address or a closure
// built there (regionLeaks); otherwise it gives back copies of what the
// region holds.

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
		return !t.IsBox() && holdsFunc(t.Elem(), seen)
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

// regionReturn checks the closure t that sc's function returns, built in
// its caller's region. The region holds the closure records the result
// reaches and the variables of this frame their literals use; any other
// memory of this frame the closure reaches is gone once the function
// returns. What it reaches of the frames around a literal is returned as
// a literal returns it. Each literal is marked as built in a region, and
// as passing on memory of the region when it may (regionLeaks), for the
// calls of it (dynFuncCall) and the loops over it (yieldValues).
func (w *walker) regionReturn(sc *scope, k int, t tok, pos token.Pos, expr ast.Expr) {
	r := w.reach(tset{t})
	region := map[*loc]bool{}
	for _, u := range r {
		if u.kind != tFrame || !closureLoc(u.loc) || u.loc.level < sc.level {
			continue
		}
		region[u.loc] = true
		lit := w.recLit[u.loc]
		if lit == nil {
			continue
		}
		for _, v := range u.loc.captures {
			region[w.varLoc(v)] = true
		}
		if f := w.a.lits[lit]; f != nil && !f.regionLeaks && w.regionLeaks(sc, u.loc, lit) {
			f.regionLeaks = true
			w.a.grew = true
		}
	}
	for _, u := range r {
		switch {
		case u.kind != tFrame || region[u.loc]:
		case u.loc.level >= sc.level:
			w.report(&viewErr{sink: sinkReturn, pos: pos, expr: expr, l: u.loc, fn: w.scopeName(sc)}, fmt.Sprintf("ret|%p", u.loc))
		case w.outerReturn(sc, k, u):
			w.grew = true
		}
	}
}

// outerReturn records a literal result's dependence on an enclosing
// frame. Calling a literal elsewhere cannot recover its captured values
// exactly, but must distinguish these results from independent values
// such as a counted string (sections 11.5 and 18.8).
func (w *walker) outerReturn(sc *scope, k int, t tok) bool {
	if sc.outerRet == nil {
		sc.outerRet = map[int]tset{}
	}
	if sc.fn.outerResults == nil {
		sc.fn.outerResults = map[int]map[string]bool{}
	}
	if sc.fn.outerResults[k] == nil {
		sc.fn.outerResults[k] = map[string]bool{}
	}
	if !sc.fn.outerResults[k][t.tag] {
		sc.fn.outerResults[k][t.tag] = true
		w.a.grew = true
	}
	if t.kind == tFrame && !sc.fn.outerRegionResults[k][t.tag] {
		if sc.fn.outerRegionResults == nil {
			sc.fn.outerRegionResults = map[int]map[string]bool{}
		}
		if sc.fn.outerRegionResults[k] == nil {
			sc.fn.outerRegionResults[k] = map[string]bool{}
		}
		sc.fn.outerRegionResults[k][t.tag] = true
		w.a.grew = true
	}
	r := sc.outerRet[k]
	grew := r.add(t)
	sc.outerRet[k] = r
	return grew
}

// regionLeaks reports whether calling the closure rec of lit, which sc's
// function returns in its caller's region, may give back memory of the
// region rather than copies of what the region holds: a variable it
// captures holds a pointer into this frame (to a closure built in the
// region, or to a captured variable), or its code takes the address of
// what it captures (litAddrsCaptures).
func (w *walker) regionLeaks(sc *scope, rec *loc, lit *ast.FuncLit) bool {
	for _, v := range rec.captures {
		for _, c := range w.load(tset{frameTok(w.varLoc(v))}) {
			if c.kind == tFrame && c.loc.level >= sc.level {
				return true
			}
		}
	}
	return w.a.litAddrsCaptures(lit, sc.node)
}

// addrsCaptures reports whether the closures f returns may pass on memory
// of their region: their code takes the address of what they capture
// (litAddrsCaptures), or what they capture points into the region
// (function.regionLeaks). Without that, they pass on copies of what the
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
		if a.litAddrsCaptures(lit, decl) || a.lits[lit] != nil && a.lits[lit].regionLeaks {
			return true
		}
	}
	return false
}

// litAddrsCaptures reports whether lit, a literal fn returns, may take the
// address of a variable it captures, which lives in the region: with &, by
// slicing an array, by calling a pointer method, or from a function
// literal inside it.
func (a *analyzer) litAddrsCaptures(lit *ast.FuncLit, fn ast.Node) bool {
	captured := func(e ast.Expr) bool {
		for {
			switch x := e.(type) {
			case *ast.ParenExpr:
				e = x.X
			case *ast.SelectorExpr:
				if sel := a.selections[x]; sel != nil && sel.Indirect() {
					return false // a field reached through a pointer, not the variable
				}
				e = x.X
			case *ast.IndexExpr:
				if _, arr := underlying(a.types[x.X].Type).(*types2.Array); !arr {
					return false // an element of a slice or through a pointer
				}
				e = x.X
			case *ast.Ident:
				v, ok := a.uses[x].(*types2.Var)
				if !ok || v.IsField() {
					return false
				}
				p := v.Pos()
				return p >= fn.Pos() && p < fn.End() && (p < lit.Pos() || p >= lit.End())
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
			found = a.capturesFrom(x, fn)
		}
		return !found
	})
	return found
}

// capturesFrom reports whether lit uses a variable declared in fn
// outside lit.
func (a *analyzer) capturesFrom(lit *ast.FuncLit, fn ast.Node) bool {
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if v, ok := a.uses[id].(*types2.Var); ok && !v.IsField() {
				p := v.Pos()
				if p >= fn.Pos() && p < fn.End() && (p < lit.Pos() || p >= lit.End()) {
					found = true
				}
			}
		}
		return !found
	})
	return found
}
