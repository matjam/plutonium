package moves

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/slices"
)

// Deferred calls run where the function returns, the last one deferred
// first (section 12.19). What a deferred call frees or reclaims happens
// then, not at the defer statement: deferStmt records it (deferRec) and
// runDefers applies it at each exit, in that order. What the call uses
// through the views its receiver, arguments and function value hold when
// the defer statement evaluates them, a function literal's included
// (section 18.3), is held by a stand-in variable (deferHold) that the
// events of the walk make stale as they do any other holder; at the exit
// a use of such a view that a free or a reclaim made stale, by the body
// or by a deferred call that runs first, is E3313.
//
// Which deferred calls are armed at an exit follows the paths: the
// stand-in reg of each defer statement is set where the statement runs and
// joins as any variable does. A defer statement runs at most once in each
// call (no path reaches it again, E2530 for a loop), so the calls run in
// the reverse of the order the walk meets their statements.

// A deferRec is what a defer statement leaves to do at the exits.
type deferRec struct {
	st        *ast.DeferStmt
	reg       *types2.Var // set while the call is armed
	holders   []deferHolder
	freed     []freeArg
	fv        []litEffect
	reclaimed []reclaimArg
	drop      *types2.Var // defer x.Drop()
}

// A deferHolder holds the views the deferred call uses through e.
type deferHolder struct {
	v *types2.Var
	e ast.Expr
}

// deferRecOf returns the record of st, made on its first walk; the
// stand-in variables stay the same from walk to walk.
func (f *fn) deferRecOf(st *ast.DeferStmt) *deferRec {
	if f.deferRecs == nil {
		f.deferRecs = map[*ast.DeferStmt]*deferRec{}
	}
	r := f.deferRecs[st]
	if r == nil {
		r = pu.New(deferRec{st: st, reg: f.standIn(st.Pos(), "defer")})
		f.deferRecs[st] = r
	}
	r.holders, r.freed, r.fv, r.reclaimed, r.drop = nil, nil, nil, nil, nil
	if !slices.Contains(f.defers, r) {
		f.defers = compat.Append(f.defers, r)
	}
	return r
}

// standIn returns a variable the walk keeps views in for a deferred call:
// no code names it.
func (f *fn) standIn(pos token.Pos, name string) *types2.Var {
	return types2.NewVar(pos, nil, name, types2.Typ[types2.UnsafePointer])
}

// holderVar returns the stand-in for the views the deferred call r uses
// through key.
func (f *fn) holderVar(key ast.Node, name string) *types2.Var {
	if f.deferVars == nil {
		f.deferVars = map[ast.Node]*types2.Var{}
	}
	v := f.deferVars[key]
	if v == nil {
		v = f.standIn(key.Pos(), name)
		f.deferVars[key] = v
	}
	return v
}

// deferHold arms the deferred call of r and records the views it will use
// when it runs, as the defer statement evaluates its receiver, arguments
// and function value. A function literal uses the views the variables it
// uses hold where it is made (section 18.3): for one written in the
// statement, those of each such variable now, by the variable's name.
func (f *fn) deferHold(r *deferRec) {
	if f.env == nil {
		return
	}
	f.setState(r.reg, vstate{dl: pu.New(deferMark{stmt: r.st.Pos()})})
	call := r.st.Call
	hold := func(key ast.Node, e ast.Expr, name string, vs []view) {
		var keep []view
		for _, w := range vs {
			if w.lit == nil {
				keep = compat.Append(keep, w)
			}
		}
		if len(keep) == 0 {
			return
		}
		h := f.holderVar(key, name)
		f.setState(h, vstate{views: sortViews(whole(keep))})
		r.holders = compat.Append(r.holders, deferHolder{v: h, e: e})
	}
	litVars := func(lit *ast.FuncLit) {
		seen := map[*types2.Var]bool{}
		ast.Inspect(lit.Body, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			v := f.varOf(id)
			if v == nil || !f.vars[v] || seen[v] || f.c.uses[id] == nil {
				return true
			}
			seen[v] = true
			hold(id, id, v.Name(), f.held(v, nil))
			return true
		})
	}
	if tv := f.c.types[call.Fun]; tv.IsType() || tv.IsBuiltin() {
		for _, a := range call.Args {
			hold(a, a, exprText(a), f.viewsTo(a, nil))
		}
		return
	}
	f.eachArg(call, func(key, val ast.Expr, T types2.Type) {
		if lit, ok := ast.Unparen(val).(*ast.FuncLit); ok {
			litVars(lit)
			return
		}
		hold(key, key, exprText(key), f.viewsTo(val, T))
	})
}

// runDefers runs, at an exit, the deferred calls armed on some path here,
// the last one deferred first: each finds the views it uses as the body
// and the calls that ran before it left them, then frees and reclaims what
// it may. The walk goes on from the state before.
func (f *fn) runDefers() {
	if !f.report || f.env == nil || len(f.defers) == 0 {
		return
	}
	saved := f.env
	f.env = f.env.clone()
	defer func() { f.env = saved }()
	var armed []*deferRec
	for _, r := range f.defers {
		if f.env[r.reg].dl != nil {
			armed = compat.Append(armed, r)
		}
	}
	run := func(r *deferRec) {
		call := r.st.Call
		f.freeViews(call, r.freed)
		f.funcValueViews(call, r.fv)
		if r.drop != nil {
			mv := move{kind: mvDrop, pos: r.st.Pos(), to: exprText(call.Fun)}
			f.staleWhere(r.drop, mv, func(w view) bool { return !w.itself && w.lit == nil })
		} else {
			f.reclaim(call, r.reclaimed)
		}
	}
	for i := len(armed) - 1; i >= 0; i-- {
		r := armed[i]
		f.deferUsesAtExit(r)
		run(r)
	}
}

// deferUsesAtExit reports a view the deferred call of r uses that a free,
// a reclaim or a new value of its owner made stale. A move or drop of an
// owner the call uses is reported as a use of the owner (deferExit), and
// the end of the block of an owner the call names is E3311 (deferStmt).
func (f *fn) deferUsesAtExit(r *deferRec) {
	for _, h := range r.holders {
		for _, w := range f.env[h.v].views {
			if w.stale == nil {
				continue
			}
			if s := f.env[w.of]; s.dl != nil && s.st != live {
				continue
			}
			if w.itself && w.stale.kind != mvScopeEnd {
				// The variable's own storage outlives a move, a new value,
				// a free or a reclaim of what it holds: the call finds the
				// variable empty or holding its new value (sections 18.5 and
				// 18.6).
				continue
			}
			if w.stale.kind == mvScopeEnd && f.names(r.st.Call, w.of) {
				continue
			}
			f.c.report(f.c.deferStaleDiag(r.st, h.e, h.v, w))
			break
		}
	}
}

// names reports whether e names v.
func (f *fn) names(e ast.Expr, v *types2.Var) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && f.varOf(id) == v {
			found = true
		}
		return !found
	})
	return found
}

// deferStaleDiag explains a deferred call's use, where the function
// returns, of a view that e held when the defer statement st ran.
func (c *checker) deferStaleDiag(st *ast.DeferStmt, e ast.Expr, v *types2.Var, w view) *diag.Diagnostic {
	d := c.staleDiag(e, v, w)
	name, owner := exprText(e), w.of.Name()
	at, lost := c.line(st.Pos()), c.line(w.stale.pos)
	how := c.howMoved(w.stale)
	if w.maybe {
		d.Msg = compat.Sprintfb("%s holds a view of %s (%s, line %d), which may have been %s at line %d, but the call deferred at line %d uses it when the function returns", compat.ArgS(name), compat.ArgS(owner), compat.ArgS(w.text), compat.ArgI(int64(c.line(w.pos))), compat.ArgS(how), compat.ArgI(int64(lost)), compat.ArgI(int64(at)))
	} else {
		d.Msg = compat.Sprintfb("%s holds a view of %s (%s, line %d), which was %s at line %d, but the call deferred at line %d uses it when the function returns", compat.ArgS(name), compat.ArgS(owner), compat.ArgS(w.text), compat.ArgI(int64(c.line(w.pos))), compat.ArgS(how), compat.ArgI(int64(lost)), compat.ArgI(int64(at)))
	}
	d.Text = compat.Concat(d.Text, compat.Sprintfb(" The call deferred at line %d keeps what %s held there and runs when the function returns, after the deferred calls made after it (section 12.19).", compat.ArgI(int64(at)), compat.ArgS(name)))
	return d
}

// rangeHold returns a stand-in that holds the views of what the range
// statement st reads at each iteration: the array of a slice, the array a
// pointer points to, a map, a string. The operand is evaluated once, so a
// free or a move in the body that ends one of those views is E3313 where
// the next iteration reads it (section 18.3). A view already stale is the
// operand's own error.
func (f *fn) rangeHold(st *ast.RangeStmt) *types2.Var {
	if f.env == nil {
		return nil
	}
	switch u := f.c.typeOf(st.X).Underlying().(type) {
	case *types2.Slice, *types2.Map:
	case *types2.Basic:
		if u.Info()&types2.IsString == 0 {
			return nil
		}
	case *types2.Pointer:
		if _, ok := u.Elem().Underlying().(*types2.Array); !ok {
			return nil
		}
	default:
		return nil
	}
	var keep []view
	for _, w := range f.viewsOf(st.X) {
		if w.stale == nil && w.lit == nil {
			keep = compat.Append(keep, w)
		}
	}
	if len(keep) == 0 {
		return nil
	}
	h := f.holderVar(st, exprText(st.X))
	f.setState(h, vstate{views: sortViews(whole(keep))})
	return h
}

// rangeNext reports, at the end of an iteration of st, a view of the
// range's operand that the body ended: the next iteration reads it.
func (f *fn) rangeNext(st *ast.RangeStmt, h *types2.Var) {
	if h == nil || !f.report || f.env == nil {
		return
	}
	for _, w := range f.env[h].views {
		if w.stale == nil || w.itself && (w.stale.kind == mvNewValue || w.stale.kind == mvTake) {
			continue
		}
		d := f.c.staleDiag(st.X, h, w)
		d.Text = compat.Concat(d.Text, " The range statement evaluates its operand once and reads the next element through it at each iteration, after the body has run.")
		f.c.report(d)
		return
	}
}
