// Translated by go2pu from internal/escape/regions.go at the go-cutover tag; edited by hand since.

package escape

import (
	"selfhost/compat"
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
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
// closures, or ranging over it, is checked as any call of a closure is.

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
		w.a.dep(t.static)
		return t.static.builds
	case t.method != "":
		for _, c := range w.a.ifaceCandidates(t.method, t.byName, t.sig) {
			w.a.dep(c.f)
			if c.f.builds {
				return true
			}
		}
	case t.dynFunc:
		for _, fv := range w.a.funcCandidates(t.sig) {
			w.a.dep(fv.f)
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
		return compat.Clone(tset{heapTok})
	}
	rec := w.tmp(e, lkCtor)
	for _, a := range args {
		w.addContents(rec, a)
	}
	return compat.Clone(tset{frameTok(rec)})
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
// a literal returns it.
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
	}
	for _, u := range r {
		switch {
		case u.kind != tFrame || region[u.loc]:
		case u.loc.level >= sc.level:
			w.report(&viewErr{sink: sinkReturn, pos: pos, expr: expr, l: u.loc, fn: w.scopeName(sc)}, compat.Sprintf("ret|%p", u.loc))
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
		sc.fn.sum.ver++
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
		sc.fn.sum.ver++
		w.a.grew = true
	}
	r := sc.outerRet[k]
	grew := r.add(t)
	sc.outerRet[k] = r
	return grew
}
