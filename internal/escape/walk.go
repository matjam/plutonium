package escape

import (
	"fmt"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// A walker analyses one declared function, or one package's variable
// initialisers, together with the function literals inside it. It keeps the
// frame's locations across repeated walks of the body, which add to their
// contents until nothing changes.
type walker struct {
	a      *analyzer
	f      *function
	top    *scope
	sc     *scope // the scope being walked
	locs   map[*types2.Var]*loc
	objs   map[*types2.Var]*loc // lkObject: the objects of boxes held in variables
	arenas []*arenaWindow       // where a local arena is the default allocator (arena.go)
	tmps   map[tmpKey]*loc
	scopes map[*ast.FuncLit]*scope
	grew   bool // a location's contents grew during this walk
	static bool // package initialisation: &T{} and slice literals are static

	recLit map[*loc]*ast.FuncLit // closure records made here, and their literals

	tracked map[*types2.Var]bool // locals followed along the control flow
	env     env                  // what they hold at the current point
	targets []*flowTarget        // enclosing breakable statements

	loops   []*loopInfo                 // the loops of the function, in source order
	loopOf  map[ast.Stmt]*loopInfo      // by statement
	litList []*ast.FuncLit              // the function literals in the function
	pending map[pendingKey]pendingStore // views of loop storage given to tracked locals
}

type tmpKey struct {
	node ast.Node
	kind locKind
}

func newWalker(a *analyzer, f *function) *walker {
	w := &walker{
		a:      a,
		f:      f,
		locs:   map[*types2.Var]*loc{},
		objs:   map[*types2.Var]*loc{},
		tmps:   map[tmpKey]*loc{},
		scopes: map[*ast.FuncLit]*scope{},
		recLit: map[*loc]*ast.FuncLit{},
	}
	w.top = &scope{fn: f}
	if f.decl != nil {
		w.top.node = f.decl
		// Which variables are tracked depends only on the source, so
		// it is worked out once for all the passes over f.
		if f.tracked == nil {
			w.computeTracked(f.decl)
			f.tracked = w.tracked
		}
		w.tracked = f.tracked
		w.computeLoops(f.decl)
		w.computeArenaWindows(f.decl.Body)
	} else {
		w.tracked = map[*types2.Var]bool{}
		w.computeLoops(nil)
	}
	return w
}

func (w *walker) run() {
	w.sc = w.top
	w.env = env{}
	w.targets = nil
	if w.f.inits {
		w.runInits()
		return
	}
	w.bindParams(w.top)
	w.block(w.f.decl.Body)
	w.finishScope(w.top)
}

// bindParams gives a scope's parameters their locations, holding the
// caller's pointers, and its results theirs.
func (w *walker) bindParams(sc *scope) {
	for i, p := range sc.fn.params {
		l := w.paramLoc(p, sc)
		if !hasPointers(p.Type()) {
			continue
		}
		switch s, isStruct := underlying(p.Type()).(*types2.Struct); {
		case w.isTracked(p):
			w.setVar(l, tset{paramTok(sc, i)})
		case isStruct:
			// Each field of a struct parameter holds the caller's pointers
			// for that field only, so that a call can tell them apart
			// (walkParam).
			for fi := 0; fi < s.NumFields(); fi++ {
				if hasPointers(s.Field(fi).Type()) {
					if l.put(fieldStep(fi), tset{paramTok(sc, i).extend(fieldStep(fi))}) {
						w.grew = true
					}
				}
			}
		default:
			w.addContents(l, tset{paramTok(sc, i)})
		}
	}
	res := sc.fn.sig.Results()
	if sc.results == nil {
		for i := 0; i < res.Len(); i++ {
			v := res.At(i)
			if v.Name() != "" {
				sc.named = true
				l := w.paramLoc(v, sc)
				l.kind = lkResult
				sc.results = append(sc.results, l)
			} else {
				sc.results = append(sc.results, &loc{kind: lkResultN, v: v, fn: w.scopeName(sc), level: sc.level})
			}
		}
	}
}

func (w *walker) paramLoc(v *types2.Var, sc *scope) *loc {
	if l := w.locs[v]; l != nil {
		return l
	}
	l := &loc{kind: lkParam, v: v, fn: w.scopeName(sc), pos: v.Pos(), level: sc.level, typ: v.Type()}
	w.locs[v] = l
	return l
}

// finishScope checks what named results hold when the function returns.
func (w *walker) finishScope(sc *scope) {
	if !sc.named {
		return
	}
	end := token.NoPos
	if sc.node != nil {
		end = sc.node.End() - 1
	}
	for k, l := range sc.results {
		var expr ast.Expr
		if l.v != nil {
			expr = &ast.Ident{NamePos: end, Name: l.v.Name()}
		}
		w.returnVal(sc, k, l.get(""), end, expr)
	}
}

// runInits walks a package's variable initialisers. They run in the
// package's init function, whose frame is gone once initialisation is over,
// but &T{} and slice literals in them are given static storage.
func (w *walker) runInits() {
	w.static = true
	defer func() { w.static = false }()
	for _, init := range w.f.pkg.Info.InitOrder {
		var vals []tset
		var types []types2.Type
		if len(init.Lhs) == 1 {
			vals = []tset{w.value(init.Rhs)}
			types = []types2.Type{w.typeOf(init.Rhs)}
		} else {
			vals, types = w.multi(init.Rhs, len(init.Lhs))
		}
		for i, v := range init.Lhs {
			if v.Name() == "_" || i >= len(vals) {
				continue
			}
			val := w.conv(vals[i], types[i], v.Type(), init.Rhs)
			w.store(tset{heapTok}, val, init.Rhs.Pos(), dest{text: "package variable " + v.Name(), global: true})
		}
	}
}

// ----------------------------------------------------------------------------
// Statements

func (w *walker) block(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	for _, st := range b.List {
		w.stmt(st)
	}
}

func (w *walker) stmt(st ast.Stmt) { w.labeled(st, "") }

// labeled walks a statement that may carry a label, for break and
// continue.
func (w *walker) labeled(st ast.Stmt, label string) {
	switch st := st.(type) {
	case nil, *ast.EmptyStmt, *ast.BadStmt:
	case *ast.BranchStmt:
		w.branch(st)
	case *ast.ExprStmt:
		w.value(st.X)
	case *ast.DeclStmt:
		w.declStmt(st)
	case *ast.AssignStmt:
		w.assignStmt(st)
	case *ast.IncDecStmt:
		w.value(st.X)
	case *ast.BlockStmt:
		w.block(st)
	case *ast.IfStmt:
		w.stmt(st.Init)
		w.value(st.Cond)
		entry := w.env.clone()
		w.block(st.Body)
		after := w.env
		w.env = entry
		w.stmt(st.Else)
		w.env = join(after, w.env)
	case *ast.ForStmt:
		w.stmt(st.Init)
		w.loop(label, st, func() {
			if st.Cond != nil {
				w.value(st.Cond)
			}
		}, func() {
			w.block(st.Body)
			t := w.targets[len(w.targets)-1]
			w.env = join(w.env, t.conts)
			t.conts = nil
			w.stmt(st.Post)
		})
	case *ast.RangeStmt:
		w.rangeStmt(st, label)
	case *ast.SwitchStmt:
		w.stmt(st.Init)
		if st.Tag != nil {
			w.value(st.Tag)
		}
		w.clauses(label, st.Body.List, func(c ast.Stmt) []ast.Stmt {
			cc := c.(*ast.CaseClause)
			for _, e := range cc.List {
				w.value(e)
			}
			return cc.Body
		})
	case *ast.TypeSwitchStmt:
		w.typeSwitch(st, label)
	case *ast.SelectStmt:
		w.clauses(label, st.Body.List, func(c ast.Stmt) []ast.Stmt {
			cc := c.(*ast.CommClause)
			w.stmt(cc.Comm)
			return cc.Body
		})
	case *ast.LabeledStmt:
		w.labeled(st.Stmt, st.Label.Name)
	case *ast.ReturnStmt:
		w.returnStmt(st)
		w.env = nil
	case *ast.DeferStmt:
		w.call(st.Call)
	case *ast.GoStmt:
		w.goStmt(st)
	case *ast.SendStmt:
		w.value(st.Chan)
		v := w.value(st.Value)
		if ch, ok := w.typeOf(st.Chan).Underlying().(*types2.Chan); ok {
			v = w.conv(v, w.typeOf(st.Value), ch.Elem(), st.Value)
		}
		w.sink(v, &viewErr{sink: sinkSend, pos: st.Arrow, expr: st.Value, ch: st.Chan, fn: w.scopeName(w.sc)}, "sends %s on a channel", "")
	default:
		w.a.internalf(st.Pos(), "unsupported statement %T", st)
	}
}

func (w *walker) declStmt(st *ast.DeclStmt) {
	gd, ok := st.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return
	}
	for _, spec := range gd.Specs {
		vs := spec.(*ast.ValueSpec)
		switch {
		case len(vs.Values) == 0:
			// Zero values hold no pointers.
			for _, name := range vs.Names {
				if v, ok := w.objOf(name).(*types2.Var); ok && w.isTracked(v) {
					w.setVar(w.varLoc(v), nil)
				}
			}
		case len(vs.Values) == len(vs.Names):
			for i, name := range vs.Names {
				v := w.value(vs.Values[i])
				w.assignTo(name, v, w.typeOf(vs.Values[i]), vs.Values[i], vs.Values[i].Pos())
			}
		case len(vs.Values) == 1:
			vals, types := w.multi(vs.Values[0], len(vs.Names))
			for i, name := range vs.Names {
				if i < len(vals) {
					w.assignTo(name, vals[i], types[i], vs.Values[0], vs.Values[0].Pos())
				}
			}
		}
	}
}

func (w *walker) assignStmt(st *ast.AssignStmt) {
	switch st.Tok {
	case token.ASSIGN, token.DEFINE:
	default:
		// x op= y: arithmetic, no pointers move.
		w.value(st.Lhs[0])
		w.value(st.Rhs[0])
		return
	}
	if len(st.Lhs) == len(st.Rhs) {
		vals := make([]tset, len(st.Rhs))
		for i, r := range st.Rhs {
			vals[i] = w.value(r)
		}
		for i, l := range st.Lhs {
			w.assignTo(l, vals[i], w.typeOf(st.Rhs[i]), st.Rhs[i], st.Pos())
		}
		return
	}
	vals, types := w.multi(st.Rhs[0], len(st.Lhs))
	for i, l := range st.Lhs {
		if i < len(vals) {
			w.assignTo(l, vals[i], types[i], st.Rhs[0], st.Pos())
		}
	}
}

// assignTo stores val, of type from, into lhs.
func (w *walker) assignTo(lhs ast.Expr, val tset, from types2.Type, node ast.Node, pos token.Pos) {
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok && id.Name == "_" {
		w.conv(val, from, from, node)
		return
	}
	to := w.lhsType(lhs)
	val = w.conv(val, from, to, node)
	src, _ := node.(ast.Expr)
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
		if v, ok := w.objOf(id).(*types2.Var); ok && w.isTracked(v) {
			if !hasPointers(to) {
				val = nil
			}
			l := w.varLoc(v)
			w.noteTracked(l, val, pos, dest{text: id.Name, src: src})
			w.setVar(l, val)
			return
		}
	}
	target, ok := w.place(lhs)
	if !ok || !hasPointers(to) {
		return
	}
	if f := w.fieldOf(lhs); f != nil {
		w.a.storeField(f, val, w.enclosingFields(lhs))
	} else if outer := w.enclosingFields(lhs); len(outer) > 0 {
		w.a.storeField(outer[0], val, outer[1:])
	}
	d := w.destOf(lhs)
	d.src = src
	w.store(target, val, pos, d)
}

// lhsType returns the type of an assignment's left-hand side, which for a
// newly declared variable is only recorded on its object.
func (w *walker) lhsType(lhs ast.Expr) types2.Type {
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
		if v, ok := w.objOf(id).(*types2.Var); ok {
			return v.Type()
		}
	}
	return w.typeOf(lhs)
}

func (w *walker) rangeStmt(st *ast.RangeStmt, label string) {
	xv := w.value(st.X)
	xt := w.typeOf(st.X)
	var key, val tset
	var keyT, valT types2.Type
	switch u := underlying(xt).(type) {
	case *types2.Array:
		val, valT = xv, u.Elem()
	case *types2.Pointer:
		if a, ok := underlying(u.Elem()).(*types2.Array); ok {
			val, valT = w.load(xv), a.Elem()
		}
	case *types2.Slice:
		val, valT = w.load(xv), u.Elem()
	case *types2.Chan:
		key, keyT = tset{heapTok}, u.Elem()
	case *types2.Signature:
		// Range over a function: the iterator passes its yield values,
		// made from what its closure reaches or from the heap.
		yv := w.yieldValues(xv)
		if u.Params().Len() == 1 {
			if y, ok := u.Params().At(0).Type().Underlying().(*types2.Signature); ok {
				if y.Params().Len() > 0 {
					key, keyT = yv, y.Params().At(0).Type()
				}
				if y.Params().Len() > 1 {
					val, valT = yv, y.Params().At(1).Type()
				}
			}
		}
	}
	w.loop(label, st, func() {
		for _, kv := range []struct {
			e ast.Expr
			v tset
			t types2.Type
		}{{st.Key, key, keyT}, {st.Value, val, valT}} {
			if kv.e == nil {
				continue
			}
			if st.Tok == token.DEFINE || st.Tok == token.ASSIGN {
				w.assignTo(kv.e, kv.v, kv.t, kv.e, st.Pos())
			}
		}
	}, func() {
		w.block(st.Body)
	})
}

// yieldValues returns what an iterator whose closure may point to xv may
// pass to yield: what its closure reaches, or the heap. The region a
// closure constructor builds is left out when its closure never passes on
// memory of the region (analyzer.addrsCaptures).
func (w *walker) yieldValues(xv tset) tset {
	var start tset
	for _, t := range xv {
		if t.kind == tFrame && t.loc.kind == lkCtor {
			if call, ok := t.loc.node.(*ast.CallExpr); ok {
				if f := w.a.staticCallee(call); f != nil && f.builds && !w.a.addrsCaptures(f) {
					start.addAll(w.load(tset{t}))
					continue
				}
			}
		}
		start.add(t)
	}
	return append(w.reach(start), heapTok)
}

func (w *walker) typeSwitch(st *ast.TypeSwitchStmt, label string) {
	w.stmt(st.Init)
	var x ast.Expr
	switch a := st.Assign.(type) {
	case *ast.AssignStmt:
		x = a.Rhs[0].(*ast.TypeAssertExpr).X
	case *ast.ExprStmt:
		x = a.X.(*ast.TypeAssertExpr).X
	}
	xv := w.value(x)
	w.clauses(label, st.Body.List, func(c ast.Stmt) []ast.Stmt {
		cc := c.(*ast.CaseClause)
		if v, ok := w.a.implicits[cc].(*types2.Var); ok {
			cv := xv
			if len(cc.List) == 1 {
				cv = w.unbox(xv, w.typeOf(cc.List[0]))
			}
			switch {
			case !hasPointers(v.Type()):
			case w.isTracked(v):
				w.setVar(w.varLoc(v), cv)
			default:
				w.store(tset{frameTok(w.varLoc(v))}, cv, cc.Pos(), dest{text: v.Name()})
			}
		}
		return cc.Body
	})
}

func (w *walker) returnStmt(st *ast.ReturnStmt) {
	sc := w.sc
	res := sc.fn.sig.Results()
	if len(st.Results) == 0 {
		return
	}
	var vals []tset
	var types []types2.Type
	if len(st.Results) == 1 && res.Len() > 1 {
		vals, types = w.multi(st.Results[0], res.Len())
	} else {
		for _, r := range st.Results {
			vals = append(vals, w.value(r))
			types = append(types, w.typeOf(r))
		}
	}
	for k := 0; k < len(vals) && k < res.Len(); k++ {
		var node ast.Node = st
		var expr ast.Expr
		if k < len(st.Results) {
			node, expr = st.Results[k], st.Results[k]
		} else if len(st.Results) == 1 {
			expr = st.Results[0]
		}
		v := w.conv(vals[k], types[k], res.At(k).Type(), node)
		if !hasPointers(res.At(k).Type()) {
			continue
		}
		pos := st.Pos()
		if expr != nil {
			pos = expr.Pos()
		}
		w.returnVal(sc, k, v, pos, expr)
		if sc.named {
			w.addContents(sc.results[k], v)
		}
	}
}

// returnVal applies the return rule to what result k of scope sc holds; expr
// is the result as written, if known.
func (w *walker) returnVal(sc *scope, k int, v tset, pos token.Pos, expr ast.Expr) {
	for _, t := range v {
		switch t.kind {
		case tFrame:
			if closureLoc(t.loc) && t.loc.level >= sc.level && regionSig(sc.fn.sig) {
				// A closure this function returns is built in its
				// caller's region; lowering checks what it may not
				// return (E3100), and regionReturn what it reaches.
				if !sc.fn.builds {
					sc.fn.builds = true
					w.a.grew = true
				}
				w.regionReturn(sc, t, pos, expr)
				continue
			}
			if t.loc.level >= sc.level {
				w.report(&viewErr{sink: sinkReturn, pos: pos, expr: expr, l: t.loc, fn: w.scopeName(sc)}, fmt.Sprintf("ret|%p", t.loc))
			} else if sc.outerRet.add(t.untag()) {
				// A literal returning something of an enclosing frame.
				w.grew = true
			}
		case tParam:
			switch {
			case t.sc == sc:
				if sc.fn.sum.addResult(k, keyOf(t), t.tag) {
					w.a.grew = true
				}
			case sc.outerRet.add(t.untag()):
				w.grew = true
			}
		case tHeap:
			if sc.fn.sum.addResultHeap(k, t.tag) {
				w.a.grew = true
			}
		}
	}
}

func (w *walker) scopeName(sc *scope) string {
	if sc.fn.lit != nil {
		return "the function literal"
	}
	return sc.fn.name
}

func (w *walker) goStmt(st *ast.GoStmt) {
	e := st.Call
	ve := func(x ast.Expr) *viewErr {
		return &viewErr{sink: sinkGo, pos: st.Pos(), expr: x, call: e, fn: w.scopeName(w.sc)}
	}
	if tv := w.a.types[e.Fun]; tv.IsBuiltin() || tv.IsType() {
		w.sink(union(w.call(e)...), ve(nil), "passes %s to a go statement", "")
		return
	}
	t := w.target(e)
	if t.ring != nil {
		w.ringCall(e, t)
		return
	}
	args := w.args(e, t.sig)
	exprs := argExprs(e, t.sig)
	// The array behind packed variadic arguments is copied to the new
	// puroutine (lower/conc.go), so only what its elements point to must
	// outlive this frame, unless the started function keeps the slice.
	packed := t.sig.Variadic() && !e.Ellipsis.IsValid() && len(e.Args) >= t.sig.Params().Len()
	for i, a := range args {
		if packed && i == len(args)-1 && t.static != nil {
			idx := i
			if t.hasRecv {
				idx++
			}
			if !t.static.keepsParam(idx) {
				a = w.load(a)
			}
		}
		w.sink(a, ve(exprs[i]), "passes %s to a go statement", "")
	}
	recv := e.Fun
	if se, ok := ast.Unparen(e.Fun).(*ast.SelectorExpr); ok {
		recv = se.X
	}
	w.sink(t.recv, ve(recv), "passes %s to a go statement", "")
	w.sink(t.fv, ve(e.Fun), "passes %s to a go statement", "")
	// What the started function's parameters hold, for heap-only fields.
	all := args
	if t.hasRecv {
		all = append([]tset{t.recv}, args...)
	}
	switch {
	case t.static != nil:
		w.a.passArgs(t.static, all)
	case t.method != "":
		for _, c := range w.a.ifaceCandidates(t.method, t.byName, t.sig) {
			w.a.passArgs(c.f, append([]tset{w.reach(t.recv)}, args...))
		}
	case t.dynFunc:
		for _, fv := range w.a.funcCandidates(t.sig) {
			fa := args
			if fv.kind != fvPlain {
				fa = append([]tset{w.reach(t.fv)}, args...)
			}
			w.a.passArgs(fv.f, fa)
		}
	}
}

// argExprs returns, for each parameter of sig, the argument expression a
// call passes for it, or nil where several are packed into a variadic
// parameter.
func argExprs(e *ast.CallExpr, sig *types2.Signature) []ast.Expr {
	n := sig.Params().Len()
	out := make([]ast.Expr, n)
	if len(e.Args) == 1 && n > 1 {
		return out // f(g()) with several results
	}
	variadic := sig.Variadic() && !e.Ellipsis.IsValid()
	for i, a := range e.Args {
		if i >= n || (variadic && i >= n-1) {
			break
		}
		out[i] = a
	}
	return out
}
