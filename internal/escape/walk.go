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
	tmps   map[tmpKey]*loc
	scopes map[*ast.FuncLit]*scope
	grew   bool // a location's contents grew during this walk
	static bool // package initialisation: &T{} and slice literals are static

	recLit  map[*loc]*ast.FuncLit // closure records made here, and their literals

	tracked map[*types2.Var]bool // locals followed along the control flow
	env     env                  // what they hold at the current point
	targets []*flowTarget        // enclosing breakable statements
}

type tmpKey struct {
	node ast.Node
	kind string
}

func newWalker(a *analyzer, f *function) *walker {
	w := &walker{
		a:      a,
		f:      f,
		locs:   map[*types2.Var]*loc{},
		tmps:   map[tmpKey]*loc{},
		scopes: map[*ast.FuncLit]*scope{},
		recLit: map[*loc]*ast.FuncLit{},
	}
	w.top = &scope{fn: f}
	if f.decl != nil {
		w.top.node = f.decl
		w.computeTracked(f.decl)
	} else {
		w.tracked = map[*types2.Var]bool{}
	}
	return w
}

// errorf reports an escape. A non-empty key makes it one error per key in
// this function.
func (w *walker) errorf(pos token.Pos, key string, format string, args ...any) {
	if key != "" {
		key = fmt.Sprintf("%p|%s", w.f, key)
	}
	w.a.errorf(pos, key, format, args...)
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
		if hasPointers(p.Type()) {
			if w.isTracked(p) {
				w.setVar(l, tset{paramTok(sc, i)})
			} else {
				w.addContents(l, tset{paramTok(sc, i)})
			}
		}
	}
	res := sc.fn.sig.Results()
	if sc.results == nil {
		for i := 0; i < res.Len(); i++ {
			v := res.At(i)
			if v.Name() != "" {
				sc.named = true
				l := w.paramLoc(v, sc)
				l.desc = "a pointer to result " + v.Name()
				sc.results = append(sc.results, l)
			} else {
				sc.results = append(sc.results, &loc{desc: fmt.Sprintf("result %d", i+1), level: sc.level})
			}
		}
	}
}

func (w *walker) paramLoc(v *types2.Var, sc *scope) *loc {
	if l := w.locs[v]; l != nil {
		return l
	}
	name := v.Name()
	if name == "" || name == "_" {
		name = "(unnamed)"
	}
	l := &loc{desc: "a pointer to parameter " + name, level: sc.level, hint: hintLocal, typ: v.Type()}
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
		w.returnVal(sc, k, l.get(""), end)
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
		w.loop(label, func() {
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
		w.sink(v, st.Arrow, "is sent on a channel", "sends %s on a channel", "")
	default:
		w.errorf(st.Pos(), "","escape analysis: unsupported statement %T", st)
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
	if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
		if v, ok := w.objOf(id).(*types2.Var); ok && w.isTracked(v) {
			if !hasPointers(to) {
				val = nil
			}
			w.setVar(w.varLoc(v), val)
			return
		}
	}
	target, ok := w.place(lhs)
	if !ok || !hasPointers(to) {
		return
	}
	w.store(target, val, pos, w.destOf(lhs))
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
	}
	w.loop(label, func() {
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
		if k < len(st.Results) {
			node = st.Results[k]
		}
		v := w.conv(vals[k], types[k], res.At(k).Type(), node)
		if !hasPointers(res.At(k).Type()) {
			continue
		}
		w.returnVal(sc, k, v, st.Pos())
		if sc.named {
			w.addContents(sc.results[k], v)
		}
	}
}

// returnVal applies the return rule to what result k of scope sc holds.
func (w *walker) returnVal(sc *scope, k int, v tset, pos token.Pos) {
	for _, t := range v {
		switch t.kind {
		case tFrame:
			if t.loc.level >= sc.level {
				w.errorf(pos, fmt.Sprintf("ret|%p", t.loc), "%s escapes: it is returned from %s%s", t.loc.desc, w.scopeName(sc), t.loc.hintText())
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
	all := w.callOperands(st.Call)
	w.sink(all, st.Pos(), "is passed to a go statement, whose puroutine may outlive this frame", "passes %s to a go statement", "")
}
