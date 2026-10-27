// Translated by go2pu from internal/escape/loops.go at the go-cutover tag; edited by hand since.

package escape

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
)

// Loops.
//
// Go gives each iteration of a loop its own variables, so a function
// literal or a pointer made in one iteration keeps seeing that iteration's
// values. Plutonium keeps a function's variables in its frame, one copy of
// each, and every iteration reuses that memory. A view of a loop's
// variables (a pointer, a slice of an array, a function literal that uses
// them) is therefore valid for one iteration only: it may not be kept
// anywhere the next iteration can see it (spec section 11.5).
//
// The iteration storage of a loop is every variable it declares (in its
// header or its body) and every temporary its condition, post statement
// and body make. A place survives the loop's back edge if it is declared
// outside that code, or in the loop's header. A function literal's record
// is iteration storage of the loops whose variables it uses: it holds only
// their addresses, which are the same in every iteration.
//
// Locals followed along the control flow are checked where the next
// iteration starts, so keeping a view and then leaving the loop with break
// or return is fine. Other storage is checked where the view is stored.

// A loopInfo is a for or range statement of the function being walked.
type loopInfo struct {
	node    ast.Stmt
	lit     *ast.FuncLit   // the function literal whose body holds it, or nil
	regions [][2]token.Pos // the code that runs in every iteration
	header  [2]token.Pos   // where the variables declared in the header are
}

func (L *loopInfo) inRegion(p token.Pos) bool {
	for _, r := range L.regions {
		if r[0] <= p && p < r[1] {
			return true
		}
	}
	return false
}

func span(n ast.Node) [2]token.Pos {
	if n == nil {
		return [2]token.Pos{}
	}
	return [2]token.Pos{n.Pos(), n.End()}
}

// computeLoops finds the loops of the function and its literals.
func (w *walker) computeLoops(body ast.Node) {
	w.loops, w.litList = nil, nil
	w.loopOf = map[ast.Stmt]*loopInfo{}
	w.pending = map[pendingKey]pendingStore{}
	if body == nil {
		return
	}
	var lits []*ast.FuncLit
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		var L *loopInfo
		switch n := n.(type) {
		case *ast.FuncLit:
			w.litList = compat.Append(w.litList, n)
			lits = compat.Append(lits, n)
			ast.Inspect(n.Body, visit)
			lits = lits[:len(lits)-1]
			return false
		case *ast.ForStmt:
			L = pu.New(loopInfo{node: n, regions: compat.Clone([][2]token.Pos{span(n.Cond), span(n.Post), span(n.Body)}), header: span(n.Init)})
		case *ast.RangeStmt:
			L = pu.New(loopInfo{node: n, regions: compat.Clone([][2]token.Pos{span(n.Body)})})
			if n.Tok == token.DEFINE && n.Key != nil {
				end := n.Key.End()
				if n.Value != nil {
					end = n.Value.End()
				}
				L.header = [2]token.Pos{n.Key.Pos(), end}
			}
		}
		if L != nil {
			if len(lits) > 0 {
				L.lit = lits[len(lits)-1]
			}
			w.loops = compat.Append(w.loops, L)
			w.loopOf[L.node] = L
		}
		return true
	}
	ast.Inspect(body, visit)
}

// litAt returns the innermost function literal of the function being
// walked whose body or signature contains p, or nil.
func (w *walker) litAt(p token.Pos) *ast.FuncLit {
	var best *ast.FuncLit
	for _, lit := range w.litList {
		if lit.Pos() < p && p < lit.End() && (best == nil || best.Pos() < lit.Pos()) {
			best = lit
		}
	}
	return best
}

// posLoops returns the loops, innermost first, whose iteration storage
// includes something declared (isVar) or made at p.
func (w *walker) posLoops(p token.Pos, isVar bool) []*loopInfo {
	lit := w.litAt(p)
	var out []*loopInfo
	for i := len(w.loops) - 1; i >= 0; i-- {
		L := w.loops[i]
		if L.lit != lit {
			continue
		}
		if L.inRegion(p) || (isVar && L.header[0] <= p && p < L.header[1]) {
			out = compat.Append(out, L)
		}
	}
	// w.loops is in source order, so nested loops come after the loops
	// around them; reversed, the innermost comes first.
	return out
}

// iterLoops returns the loops, innermost first, of which l is iteration
// storage.
func (w *walker) iterLoops(l *loc) []*loopInfo {
	if l.kind == lkClosure {
		var out []*loopInfo
		for _, v := range l.captures {
			for _, L := range w.posLoops(v.Pos(), true) {
				if !containsLoop(out, L) {
					out = compat.Append(out, L)
				}
			}
		}
		return out
	}
	if l.kind == lkResult || l.kind == lkResultN || l.kind == lkParam {
		return nil
	}
	return w.posLoops(l.pos, l.isVarLoc())
}

func containsLoop(ls []*loopInfo, L *loopInfo) bool {
	for _, x := range ls {
		if x == L {
			return true
		}
	}
	return false
}

// survives reports whether h keeps what it holds across L's back edge:
// whether it is declared or made outside the code L runs in every
// iteration. Variables declared in L's header survive, since Go copies
// them from one iteration to the next.
func (w *walker) survives(h *loc, L *loopInfo) bool {
	return !h.pos.IsValid() || !containsLoop(w.posLoops(h.pos, false), L)
}

// keptPast returns the loop past whose iteration a view of s would be kept
// if stored in h, or nil.
func (w *walker) keptPast(h, s *loc) *loopInfo {
	for _, L := range w.iterLoops(s) {
		if w.survives(h, L) {
			return L
		}
	}
	return nil
}

// iterVarOf returns the variable of loop L that closure record s uses.
func (w *walker) iterVarOf(s *loc, L *loopInfo) *types2.Var {
	for _, v := range s.captures {
		if containsLoop(w.posLoops(v.Pos(), true), L) {
			return v
		}
	}
	return nil
}

// loopErr reports that a view of s, stored in d at pos, is kept past L's
// iterations.
func (w *walker) loopErr(s *loc, L *loopInfo, d dest, pos token.Pos) {
	e := &viewErr{sink: sinkLoop, pos: pos, expr: d.src, l: s, fn: w.scopeName(w.sc), dst: d.text, callee: d.callee, calleeWhy: d.calleeWhy, loop: L.node}
	if s.kind == lkClosure {
		e.iterVar = w.iterVarOf(s, L)
	}
	w.report(e, compat.Sprintf("loop|%p|%p", s, L.node))
}

// A pendingKey names a tracked local h holding a view of s.
type pendingKey struct{ h, s *loc }

// A pendingStore is where a tracked local was given a view of a loop's
// storage, reported if the view is still there when the next iteration
// starts.
type pendingStore struct {
	pos token.Pos
	d   dest
}

// noteTracked records the views of loop storage a tracked local h is
// given at pos.
func (w *walker) noteTracked(h *loc, v tset, pos token.Pos, d dest) {
	for _, t := range v {
		if t.kind != tFrame {
			continue
		}
		k := pendingKey{h, t.loc}
		if _, ok := w.pending[k]; !ok && len(w.iterLoops(t.loc)) > 0 {
			w.pending[k] = pendingStore{pos: pos, d: d}
		}
	}
}

// checkBackEdge checks what the tracked locals hold where the next
// iteration of L starts.
func (w *walker) checkBackEdge(L *loopInfo, at env) {
	if L == nil || !w.a.report {
		return
	}
	for h, ts := range at {
		for _, t := range ts {
			if t.kind != tFrame || !containsLoop(w.iterLoops(t.loc), L) || !w.survives(h, L) {
				continue
			}
			p, ok := w.pending[pendingKey{h, t.loc}]
			if !ok {
				p = pendingStore{pos: L.node.Pos(), d: dest{text: h.v.Name()}}
			}
			w.loopErr(t.loc, L, p.d, p.pos)
		}
	}
}
