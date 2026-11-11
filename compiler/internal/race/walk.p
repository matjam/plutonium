// Translated by go2pu from internal/race/walk.go at the go-cutover tag; edited by hand since.

package race

import (
	"compiler/compat"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// A state is what the flow analysis knows at a point of a function body.
type state struct {
	dead    bool     // unreachable
	locks   lockset  // taken in this function and held on every path here
	rel     released // locks the caller held that may have been released
	spawned bool     // a task may have been started since entry
	pubAll  bool     // every allocation may have been published (after a goto)
	pub     map[ast.Node]bool
	running runSet // the check of scoped tasks: the tasks that may be running (scope.go)
	// snaps are the regions this function opened that were held where each
	// load that a dependent per-CPU store depends on was made (dep.go). The
	// map is never changed once it is shared: setSnap copies it.
	snaps map[int]lockset
	// views are the locals holding a view loaded from shared memory, each
	// with the locks this function took that are held from the load to
	// here with no ending event (section 19.3, Views loaded under a lock).
	// The map is never changed once it is shared: setView copies it.
	views map[*types2.Var]lockset
	// vpre are the locals of views whose load was made before any task
	// was started since entry: not a crossing load once the function is
	// the program before the start of sharing. Shared like views.
	vpre map[*types2.Var]bool
}

// setView records the locks under which the view in local v was loaded,
// or, with ok false, that v holds no such view.
func (s *state) setView(v *types2.Var, held lockset, ok bool) {
	if _, had := s.views[v]; !had && !ok {
		return
	}
	m := make(map[*types2.Var]lockset, len(s.views)+1)
	for k, l := range s.views {
		m[k] = l
	}
	if ok {
		m[v] = held
	} else {
		delete(m, v)
	}
	s.views = m
	if pre := ok && !s.spawned; pre != s.vpre[v] {
		n := make(map[*types2.Var]bool, len(s.vpre)+1)
		for k := range s.vpre {
			n[k] = true
		}
		if pre {
			n[v] = true
		} else {
			delete(n, v)
		}
		s.vpre = n
	}
}

// endViews ends the views loaded under lock p (all locks when p is nil):
// an Unlock or a Wait of it, or a call that may release it.
func (s *state) endViews(p *vpath) {
	if len(s.views) == 0 {
		return
	}
	m := make(map[*types2.Var]lockset, len(s.views))
	for k, l := range s.views {
		if p == nil {
			m[k] = nil
		} else {
			m[k] = l.without(*p)
		}
	}
	s.views = m
}

// joinViews returns the views after either of two paths: a lock counts
// for a view only when it was held without an ending event on both.
func joinViews(a, b map[*types2.Var]lockset) map[*types2.Var]lockset {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make(map[*types2.Var]lockset, len(a)+len(b))
	for k, v := range a {
		if w, ok := b[k]; ok {
			v = v.meet(w)
		}
		out[k] = v
	}
	for k, v := range b {
		if _, ok := a[k]; !ok {
			out[k] = v
		}
	}
	return out
}

// joinPre returns the views loaded before any task was started after
// either of two paths: on each path that holds the view.
func joinPre(a, b *state) map[*types2.Var]bool {
	if len(a.vpre) == 0 && len(b.vpre) == 0 {
		return nil
	}
	out := map[*types2.Var]bool{}
	for _, x := range []*state{a, b} {
		y := b
		if x == b {
			y = a
		}
		for v := range x.vpre {
			if _, held := y.views[v]; !held || y.vpre[v] {
				out[v] = true
			}
		}
	}
	return out
}

func samePre(a, b map[*types2.Var]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sameViews(a, b map[*types2.Var]lockset) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || !v.equal(w) {
			return false
		}
	}
	return true
}

// setSnap records the regions held at load n.
func (s *state) setSnap(n int, regions lockset) {
	m := make(map[int]lockset, len(s.snaps)+1)
	for k, v := range s.snaps {
		m[k] = v
	}
	m[n] = regions
	s.snaps = m
}

// ownRegions returns the regions this function opened that are held now.
func (s *state) ownRegions() lockset {
	var out lockset
	for _, l := range s.locks {
		if isRegion(l.place) {
			out = lockset(compat.Append(out, l))
		}
	}
	return out
}

// joinSnaps returns the snapshots after either of two paths. A region
// released on the way no longer covers a load made before it, so the
// snapshots keep only the regions still held (locks).
func joinSnaps(a, b map[int]lockset, locks lockset) map[int]lockset {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make(map[int]lockset, len(a)+len(b))
	for k, v := range a {
		if w, ok := b[k]; ok {
			v = v.meet(w)
		}
		out[k] = v.meet(locks)
	}
	for k, v := range b {
		if _, ok := a[k]; !ok {
			out[k] = v.meet(locks)
		}
	}
	return out
}

func sameSnaps(a, b map[int]lockset) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || !v.equal(w) {
			return false
		}
	}
	return true
}

func (s *state) clone() *state {
	c := pu.New(*s)
	c.pub = map[ast.Node]bool{}
	for k := range s.pub {
		c.pub[k] = true
	}
	return c
}

// join returns the state after either of two paths.
func join(a, b *state) *state {
	switch {
	case a == nil || a.dead:
		if b == nil {
			return pu.New(state{dead: true})
		}
		return b.clone()
	case b == nil || b.dead:
		return a.clone()
	}
	out := a.clone()
	out.locks = a.locks.meet(b.locks)
	out.rel = a.rel.union(b.rel)
	out.spawned = a.spawned || b.spawned
	out.pubAll = a.pubAll || b.pubAll
	out.running = a.running.union(b.running)
	out.snaps = joinSnaps(a.snaps, b.snaps, out.locks)
	out.views = joinViews(a.views, b.views)
	out.vpre = joinPre(a, b)
	for k := range b.pub {
		out.pub[k] = true
	}
	return out
}

func (s *state) equal(t *state) bool {
	if s.dead || t.dead {
		return s.dead == t.dead
	}
	if !s.locks.equal(t.locks) || !s.rel.equal(t.rel) || s.spawned != t.spawned || s.pubAll != t.pubAll || len(s.pub) != len(t.pub) || !s.running.equal(t.running) || !sameSnaps(s.snaps, t.snaps) || !sameViews(s.views, t.views) || !samePre(s.vpre, t.vpre) {
		return false
	}
	for k := range s.pub {
		if !t.pub[k] {
			return false
		}
	}
	return true
}

// A target is a statement that break or continue may leave.
type target struct {
	label  string
	loop   bool
	breaks *state
	conts  *state
}

// A walker summarises one function.
type walker struct {
	a   *analyzer
	fn  *function
	u   *unit
	sum *summary
	st  *state

	quiet      int         // inside a loop's trial iterations: record nothing
	loopPos    []token.Pos // the loops being walked, innermost last
	loops      int         // loop nesting, for spawns that may run more than once
	targets    []*target
	exit       *state
	defers     []*pendingDefer
	gotos      map[string]bool
	depth      int               // nested applications of func-valued arguments
	created    map[ast.Node]bool // allocations made by this function
	lockedHere map[vpath]bool    // mutexes this function locks

	// The check of scoped tasks (scope.go): the check, and the party whose
	// code is walked; nil for a summary.
	frame    *frameCheck
	party    *party
	noFilter bool // deferred calls at the exit: every Scope is still there
	inlined  int  // depth of literals walked in place
	// inlinedAt is the call of the outermost literal walked in place: the
	// tasks running there are those running at its uses of memory.
	inlinedAt token.Pos
	conc      bool // a summary: the code walked runs in several scoped tasks at once
	noPark    int  // inside the communication of a select: the select decided whether to wait
	freeing   int  // the writes recorded free or drop the memory (free.go)
	viewLoad  bool // the read recorded loads a view (loadCrossings)
	syncing   bool // the access recorded is a call of a pluto/sync method (syncUse)
	// vvalid, when set, holds the locks of the view the accesses recorded
	// are made through (viewOf).
	vvalid *lockset
	vpre   bool // the view of vvalid was loaded before any task was started (state.vpre)
	// viewPre is set by identView: whether the view it found was loaded
	// before any task was started.
	viewPre bool
	// The arguments of deferred calls, evaluated at the defer statement:
	// their values, and for each view among them the variable that
	// follows its locks (state.views). deferCall is the deferred call
	// being applied where the function returns.
	deferVals map[ast.Expr]origins
	deferVars map[ast.Expr]*types2.Var
	deferCall *ast.CallExpr
	// callHeld holds, for each call that may return a view, the locks it
	// is valid under (applyOpts.held). pendingView, when set, holds the
	// locks of the view the value being assigned holds (exprView).
	callHeld    map[*ast.CallExpr]lockset
	pendingView *lockset
}

func newWalker(a *analyzer, fn *function) *walker {
	return pu.New(walker{a: a, fn: fn, u: fn.unit, sum: pu.New(summary{}), st: pu.New(state{pub: map[ast.Node]bool{}})})
}

func (w *walker) run() {
	if w.fn.inits {
		w.runInits()
		return
	}
	for i, p := range w.fn.params {
		w.setVar(p, origins{{vp: vpath{root: root{kind: rParam, idx: i}}}})
	}
	w.gotos = gotoTargets(w.fn.body)
	w.noteRecycle()
	w.block(w.fn.body)
	w.finish()
}

// runInits walks a package's variable initialisers, then calls its init
// functions.
func (w *walker) runInits() {
	for _, init := range w.fn.pkg.Info.InitOrder {
		var vals []origins
		if len(init.Lhs) == 1 {
			vals = []origins{w.value(init.Rhs)}
		} else {
			vals = w.multi(init.Rhs, len(init.Lhs))
		}
		for i, v := range init.Lhs {
			if v.Name() == "_" || i >= len(vals) {
				continue
			}
			p := vpath{root: root{kind: rGlobal, v: v}}
			for _, o := range vals[i] {
				if _, ok := o.vp.root.node.(*ast.CompositeLit); ok && o.vp.root.kind == rFresh && o.vp.path == "&" && o.tag == "" {
					w.a.initLits[o.vp.root.node] = true
				}
			}
			w.store([]vpath{p}, vals[i], init.Rhs.Pos(), v.Type())
			w.access(p, true, false, init.Rhs.Pos(), v.Type(), false, "")
		}
	}
	for _, file := range w.fn.pkg.Files {
		for _, d := range file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "init" && fd.Recv == nil {
				if fn := w.a.funcs[w.a.defs[fd.Name].(*types2.Func)]; fn != nil {
					_, w.st = w.apply(fn, nil, fd.Pos(), "init", w.st, nil)
				}
			}
		}
	}
	w.sum.exitLocks, w.sum.exitRel = w.st.locks, w.st.rel
}

// runProgram walks the whole program: every package's initialisation in
// order, then main.
func (w *walker) runProgram() {
	for _, init := range w.a.inits {
		_, w.st = w.apply(init, nil, token.NoPos, init.name, w.st, nil)
	}
	if m := w.a.prog.Main; m != nil {
		if obj, ok := m.Types.Scope().Lookup("main").(*types2.Func); ok {
			if fn := w.a.funcs[obj]; fn != nil {
				_, w.st = w.apply(fn, nil, token.NoPos, "main", w.st, nil)
			}
		}
	}
}

// A pendingDefer is a deferred call and the join of the states at its
// defer statement and at every point after it.
type pendingDefer struct {
	call *ast.CallExpr
	at   *state
}

// seeDefers joins the current state into the states of the deferred calls
// so far: a deferred call runs where its function leaves, which may be at
// any point after its defer statement, in a callee that ends the task with
// rt.ExitTask or in a panic (language spec section 19.3, Held sets).
func (w *walker) seeDefers() {
	for _, d := range w.defers {
		d.at = join(d.at, w.st)
	}
}

// runDefers applies the deferred calls, last deferred first, from the state
// at the function's exit. Each call's accesses use what holds both there
// and at every point after its defer statement; the state it leaves is the
// one from the exit.
func (w *walker) runDefers() {
	for i := len(w.defers) - 1; i >= 0; i-- {
		d := w.defers[i]
		at := join(w.st, d.at)
		if at.equal(w.st) {
			w.deferred(d.call)
			continue
		}
		exit := w.st
		w.st = at
		w.deferred(d.call)
		w.st = exit
		w.quiet++
		w.deferred(d.call)
		w.quiet--
	}
}

// finish applies the deferred calls and records what the function leaves
// held.
func (w *walker) finish() {
	w.exit = join(w.exit, w.st)
	w.st = w.exit
	if !w.st.dead {
		// Deferred calls run before any variable of the function is
		// dropped, a Scope included.
		w.noFilter = true
		w.runDefers()
		w.noFilter = false
	}
	if w.st.dead {
		w.sum.exitLocks, w.sum.exitRel = nil, released{}
	} else {
		w.sum.exitLocks, w.sum.exitRel = w.st.locks, w.st.rel
	}
	if w.fn.sig != nil {
		res := w.fn.sig.Results()
		for i := 0; i < res.Len(); i++ {
			if v := res.At(i); v.Name() != "" {
				w.sum.addResult(i, w.u.vars[v])
			}
		}
	}
}

// deferred applies a deferred call where the function returns. A deferred
// lock is not counted as held: it may not have been deferred on this path.
func (w *walker) deferred(c *ast.CallExpr) {
	saved := w.deferCall
	w.deferCall = c
	defer func() { w.deferCall = saved }()
	if op, recv := w.lockOp(c); op != "" {
		if op == "unlock" {
			w.unlock(recv)
			if w.isRTLock(recv) {
				w.region(regLock, false, c.Pos())
			}
		}
		return
	}
	w.call(c)
}

// gotoTargets returns the labels goto statements in body jump to.
func gotoTargets(body ast.Node) map[string]bool {
	out := map[string]bool{}
	if body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if b, ok := n.(*ast.BranchStmt); ok && b.Tok == token.GOTO {
			out[b.Label.Name] = true
		}
		return true
	})
	return out
}

func (w *walker) setVar(v *types2.Var, o origins) {
	if len(o) == 0 {
		return
	}
	cur := w.u.vars[v]
	if cur.addAll(o) {
		w.u.vars[v] = cur
		w.u.grew = true
	}
}

// ----------------------------------------------------------------------------
// Statements

func (w *walker) block(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	for _, st := range b.List {
		w.stmt(st, "")
	}
}

func (w *walker) stmt(st ast.Stmt, label string) {
	w.seeDefers()
	switch st := st.(type) {
	case nil, *ast.EmptyStmt, *ast.BadStmt:
	case *ast.LabeledStmt:
		if w.gotos[st.Label.Name] {
			// A goto may arrive here from anywhere: assume the worst.
			w.st = pu.New(state{locks: nil, rel: released{all: true}, spawned: true, pubAll: true, pub: map[ast.Node]bool{}})
		}
		w.stmt(st.Stmt, st.Label.Name)
	case *ast.BranchStmt:
		w.branch(st)
	case *ast.ExprStmt:
		w.value(st.X)
	case *ast.DeclStmt:
		w.declStmt(st)
	case *ast.AssignStmt:
		w.assignStmt(st)
	case *ast.OrStmt:
		w.orStmt(st)
	case *ast.IncDecStmt:
		w.readWrite(st.X, st.Pos())
	case *ast.BlockStmt:
		w.block(st)
	case *ast.IfStmt:
		w.stmt(st.Init, "")
		tState, fState := w.cond(st.Cond)
		w.st = tState
		w.block(st.Body)
		after := w.st
		w.st = fState
		w.stmt(st.Else, "")
		w.st = join(after, w.st)
	case *ast.ForStmt:
		w.stmt(st.Init, "")
		w.loopPos = compat.AppendPlain(w.loopPos, st.Pos())
		defer func() { w.loopPos = w.loopPos[:len(w.loopPos)-1] }()
		w.loop(label, func() *state {
			if st.Cond == nil {
				return pu.New(state{dead: true})
			}
			_, f := w.cond(st.Cond)
			return f
		}, func() { w.block(st.Body) }, func() { w.stmt(st.Post, "") })
	case *ast.RangeStmt:
		w.rangeStmt(st, label)
	case *ast.SwitchStmt:
		w.stmt(st.Init, "")
		if st.Tag != nil {
			w.value(st.Tag)
		}
		w.clauses(label, st.Body.List, true, func(c ast.Stmt) []ast.Stmt {
			cc := c.(*ast.CaseClause)
			for _, e := range cc.List {
				w.value(e)
			}
			return cc.Body
		})
	case *ast.TypeSwitchStmt:
		w.stmt(st.Init, "")
		var x ast.Expr
		switch as := st.Assign.(type) {
		case *ast.AssignStmt:
			x = as.Rhs[0].(*ast.TypeAssertExpr).X
		case *ast.ExprStmt:
			x = as.X.(*ast.TypeAssertExpr).X
		}
		xv := w.value(x)
		w.clauses(label, st.Body.List, true, func(c ast.Stmt) []ast.Stmt {
			cc := c.(*ast.CaseClause)
			if v, ok := w.a.implicits[cc].(*types2.Var); ok {
				w.setVar(v, xv)
			}
			return cc.Body
		})
	case *ast.SelectStmt:
		hasDefault := false
		for _, c := range st.Body.List {
			hasDefault = hasDefault || c.(*ast.CommClause).Comm == nil
		}
		if !hasDefault {
			w.parkPoint() // waits for a communication
		}
		w.clauses(label, st.Body.List, false, func(c ast.Stmt) []ast.Stmt {
			cc := c.(*ast.CommClause)
			if cc.Comm == nil {
				return cc.Body
			}
			w.noPark++ // the select has waited, or has a default case
			w.stmt(cc.Comm, "")
			w.noPark--
			return cc.Body
		})
	case *ast.ReturnStmt:
		w.returnStmt(st)
	case *ast.DeferStmt:
		w.defers = compat.Append(w.defers, pu.New(pendingDefer{call: st.Call, at: w.st.clone()}))
		// The arguments are evaluated now; the call uses their values
		// when it runs. A view among them is used then, so it stays valid
		// only while its lock stays held (section 19.3, Views loaded
		// under a lock): it is followed as a local would be.
		if op, _ := w.lockOp(st.Call); op == "" {
			for _, arg := range st.Call.Args {
				iv := w.identView(arg)
				v := w.value(arg)
				if w.deferVals == nil {
					w.deferVals = map[ast.Expr]origins{}
					w.deferVars = map[ast.Expr]*types2.Var{}
				}
				w.deferVals[arg] = v
				T := w.typeOf(arg)
				if iv == nil && !(w.a.holdsView(T, 0) && w.loadedView(v)) {
					continue
				}
				dv := w.deferVars[arg]
				if dv == nil {
					dv = types2.NewVar(arg.Pos(), nil, "a deferred argument", T)
					w.deferVars[arg] = dv
				}
				held := w.st.locks
				if iv != nil {
					held = *iv
				}
				w.st.setView(dv, held, true)
			}
		}
	case *ast.TaskStmt:
		w.taskStmt(st)
	case *ast.SendStmt:
		w.value(st.Chan)
		w.chanUse(st.Chan, st.Arrow)
		v := w.value(st.Value)
		var elem types2.Type
		if ch, ok := w.typeOf(st.Chan).Underlying().(*types2.Chan); ok {
			elem = ch.Elem()
		}
		w.conv(st.Value, elem)
		w.cross(pu.New(crossing{pos: st.Arrow, what: compat.Concat("the value sent on ", exprText(st.Chan)), typ: elem, vals: v, expr: st.Value}))
		w.publishAt(v, st.Arrow, "sent on a channel")
		w.parkPoint()
	default:
		panic(pu.Box(compat.Sprintf("race: unsupported statement %T", st)))
	}
}

func (w *walker) branch(st *ast.BranchStmt) {
	switch st.Tok {
	case token.BREAK, token.CONTINUE:
		label := ""
		if st.Label != nil {
			label = st.Label.Name
		}
		for i := len(w.targets) - 1; i >= 0; i-- {
			t := w.targets[i]
			if label != "" && t.label != label {
				continue
			}
			if st.Tok == token.CONTINUE {
				if !t.loop {
					continue
				}
				t.conts = join(t.conts, w.st)
			} else {
				t.breaks = join(t.breaks, w.st)
			}
			break
		}
		w.st = pu.New(state{dead: true})
	case token.GOTO:
		w.st = pu.New(state{dead: true})
	case token.FALLTHROUGH:
		// Handled by clauses.
	}
}

// loop walks a loop. cond evaluates the condition and returns the state in
// which the loop ends because of it; body and post walk the rest. The
// iterations are tried quietly until the state at the top stops changing,
// then walked once more to record.
func (w *walker) loop(label string, cond func() *state, body, post func()) {
	entry := w.st
	try := func(record bool) (exit, back *state, t *target) {
		if !record {
			w.quiet++
			defer func() { w.quiet-- }()
		}
		w.loops++
		defer func() { w.loops-- }()
		w.st = entry.clone()
		exit = cond()
		t = pu.New(target{label: label, loop: true})
		w.targets = compat.Append(w.targets, t)
		body()
		w.targets = w.targets[:len(w.targets)-1]
		w.st = join(w.st, t.conts)
		post()
		return exit, w.st, t
	}
	for i := 0; i < 4 && !entry.dead; i++ {
		_, back, _ := try(false)
		next := join(entry, back)
		if next.equal(entry) {
			break
		}
		entry = next
	}
	exit, _, t := try(true)
	w.st = join(exit, t.breaks)
}

func (w *walker) rangeStmt(st *ast.RangeStmt, label string) {
	w.loopPos = compat.AppendPlain(w.loopPos, st.Pos())
	defer func() { w.loopPos = w.loopPos[:len(w.loopPos)-1] }()
	xt := w.typeOf(st.X)
	var key, val origins
	if xt != nil && types2.RangesByMethod(xt) {
		// A container: range over the iterator its All method returns.
		it := w.methodCall(st.X, types2.RangeMethodName, nil, st.Pos())
		w.callValue(it, loopBody(st), st.Pos(), nil, "the iterator of the range loop")
	} else {
		// Without a value variable the loop reads no element: an array's
		// length is constant, so it does not even evaluate the array.
		elems := st.Value != nil
		var xv origins
		if _, arr := underlying(xt).(*types2.Array); !arr || elems {
			xv = w.value(st.X)
		}
		switch u := underlying(xt).(type) {
		case *types2.Array:
			if !elems {
				break
			}
			if ps, ok := w.place(st.X); ok {
				for _, p := range ps.ps {
					w.access(p.extend("[]"), false, false, st.X.Pos(), u.Elem(), ps.sync, ps.key)
				}
				val = valuesAt(extendAll(ps.ps, "[]"))
			} else {
				val = xv
			}
		case *types2.Pointer:
			if arr, ok := underlying(u.Elem()).(*types2.Array); ok && elems {
				ps := xv.derefOr("*", w.unknownFor(st.X))
				for _, p := range ps {
					w.access(p.extend("[]"), false, false, st.X.Pos(), arr.Elem(), false, "")
				}
				val = valuesAt(extendAll(ps, "[]"))
			}
		case *types2.Slice:
			if !elems {
				break
			}
			ps := xv.derefOr("%", w.unknownFor(st.X))
			for _, p := range ps {
				w.access(p, false, false, st.X.Pos(), u.Elem(), false, "")
			}
			val = valuesAt(ps)
		case *types2.Signature:
			// Range over a function: it is called with the loop body as
			// its yield function, which runs here.
			if w.a.cpuOn {
				c := w.a.rangeCall(st)
				w.cpuCall(c)
				w.cpuCallStore(c)
			}
			w.callValue(xv, loopBody(st), st.Pos(), nil, "the iterator of the range loop")
		default:
			if types2.MapOf(xt) != nil {
				ps := xv.derefOr(w.derefStep(xt), w.unknownFor(st.X))
				for _, p := range ps {
					w.access(p, false, false, st.X.Pos(), xt, false, "")
				}
				key, val = valuesAt(ps), valuesAt(ps)
			}
		}
	}
	_, isChan := underlying(xt).(*types2.Chan)
	if isChan {
		w.chanUse(st.X, st.X.Pos())
	}
	w.loop(label, func() *state {
		if isChan {
			w.parkPoint() // each iteration waits for a value
		}
		exit := w.st.clone()
		for _, kv := range []struct {
			e ast.Expr
			v origins
		}{{st.Key, key}, {st.Value, val}} {
			if kv.e == nil {
				continue
			}
			if st.Tok == token.DEFINE {
				if id, ok := kv.e.(*ast.Ident); ok {
					if v, ok := w.a.defs[id].(*types2.Var); ok {
						w.setVar(v, kv.v)
						w.st.setView(v, w.st.locks, w.a.holdsView(v.Type(), 0) && w.loadedView(kv.v))
					}
				}
				continue
			}
			if st.Tok == token.ASSIGN {
				w.assignTo(kv.e, kv.v, st.Pos())
			}
		}
		return exit
	}, func() { w.block(st.Body) }, func() {})
}

// clauses walks the clauses of a switch or select. Without a default
// clause a switch may run none of them.
func (w *walker) clauses(label string, list []ast.Stmt, isSwitch bool, walk func(ast.Stmt) []ast.Stmt) {
	entry := w.st
	t := pu.New(target{label: label})
	w.targets = compat.Append(w.targets, t)
	var out *state
	hasDefault := false
	var carry *state // a fallthrough from the previous clause
	for _, c := range list {
		switch c := c.(type) {
		case *ast.CaseClause:
			if c.List == nil {
				hasDefault = true
			}
		case *ast.CommClause:
			if c.Comm == nil {
				hasDefault = true
			}
		}
		w.st = join(entry.clone(), carry)
		body := walk(c)
		for _, s := range body {
			w.stmt(s, "")
		}
		carry = nil
		if n := len(body); n > 0 {
			if b, ok := body[n-1].(*ast.BranchStmt); ok && b.Tok == token.FALLTHROUGH {
				carry = w.st
				continue
			}
		}
		out = join(out, w.st)
	}
	w.targets = w.targets[:len(w.targets)-1]
	if isSwitch && !hasDefault {
		out = join(out, entry)
	}
	if out == nil {
		out = &state{dead: true} // select {}
	}
	w.st = join(out, t.breaks)
}

func (w *walker) returnStmt(st *ast.ReturnStmt) {
	if w.fn.sig != nil && len(st.Results) > 0 {
		res := w.fn.sig.Results()
		var vals []origins
		if len(st.Results) == 1 && res.Len() > 1 {
			vals = w.multi(st.Results[0], res.Len())
		} else {
			for i, r := range st.Results {
				vals = compat.Append(vals, w.value(r))
				if i < res.Len() {
					w.conv(r, res.At(i).Type())
				}
			}
		}
		for i, v := range vals {
			w.cpuSink(v, st.Pos(), "returned")
			w.sum.addResult(i, v)
			if len(st.Results) == len(vals) && isNil(w.a, st.Results[i]) {
				continue // nil is no memory at all: it does not decide
			}
			w.noteFreshResult(i, v)
		}
	}
	w.exit = join(w.exit, w.st)
	w.st = pu.New(state{dead: true})
}

// orStmt walks an or statement (language spec section 11.27): the call,
// then the clause, where err holds the last result and nothing on the left
// is assigned, then the assignment of the other results. The clause leaves,
// so the path out of it does not reach the statement after.
func (w *walker) orStmt(st *ast.OrStmt) {
	n := 1
	if tup, ok := w.typeOf(st.Call).(*types2.Tuple); ok {
		n = tup.Len()
	}
	vals := w.multi(st.Call, n)
	after := w.st.clone()
	errVar, _ := w.a.implicits[st].(*types2.Var)
	if errVar != nil {
		if w.frame != nil {
			w.access(vpath{root: root{kind: rLocal, v: errVar}}, true, false, st.Or, errVar.Type(), false, "")
		}
		w.setVar(errVar, vals[n-1])
	}
	r, isReturn := st.Clause.(*ast.ReturnStmt)
	form := types2.OrReturnPlain
	if isReturn && w.fn.sig != nil {
		form = types2.OrReturnForm(w.fn.sig.Results(), r, w.a.types)
	}
	if form != types2.OrReturnPlain {
		// A short form of return: the last result is err or E, and the
		// others are zero or the named results as they are.
		k := w.fn.sig.Results().Len() - 1
		var v origins
		if form == types2.OrReturnExpr {
			v = w.value(r.Results[0])
			w.conv(r.Results[0], w.fn.sig.Results().At(k).Type())
		} else if errVar != nil {
			v = w.u.vars[errVar]
		}
		w.sum.addResult(k, v)
		w.noteFreshResult(k, v)
		w.exit = join(w.exit, w.st)
		w.st = pu.New(state{dead: true})
	} else {
		w.stmt(st.Clause, "")
	}
	w.st = after
	for i, l := range st.Lhs {
		if i < n-1 {
			w.assignTo(l, vals[i], st.Pos())
		}
	}
}

func (w *walker) declStmt(st *ast.DeclStmt) {
	gd, ok := st.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return
	}
	for _, spec := range gd.Specs {
		vs := spec.(*ast.ValueSpec)
		if w.frame != nil {
			for _, name := range vs.Names {
				if v, ok := w.a.defs[name].(*types2.Var); ok {
					w.newScope(v)
				}
			}
		}
		switch {
		case len(vs.Values) == len(vs.Names):
			for i, name := range vs.Names {
				w.conv(vs.Values[i], w.typeOf(name))
				val := w.value(vs.Values[i])
				w.pendingView = w.exprView(vs.Values[i])
				w.assignTo(name, val, vs.Values[i].Pos())
			}
		case len(vs.Values) == 1:
			vals := w.multi(vs.Values[0], len(vs.Names))
			view := w.exprView(vs.Values[0])
			for i, name := range vs.Names {
				if i < len(vals) {
					w.pendingView = view
					w.assignTo(name, vals[i], vs.Values[0].Pos())
				}
			}
		}
	}
}

func (w *walker) assignStmt(st *ast.AssignStmt) {
	switch st.Tok {
	case token.ASSIGN, token.DEFINE:
	default:
		// x op= y reads and writes x.
		if st.Tok == token.ADD_ASSIGN && isString(w.typeOf(st.Lhs[0])) {
			w.sum.mayAlloc = true // a new string
		}
		w.value(st.Rhs[0])
		w.readWrite(st.Lhs[0], st.Pos())
		return
	}
	if len(st.Lhs) == len(st.Rhs) {
		vals := compat.Make[origins](len(st.Rhs))
		views := compat.Make[*lockset](len(st.Rhs))
		for i, r := range st.Rhs {
			vals[i] = w.value(r)
			views[i] = w.exprView(r)
			w.conv(r, w.typeOf(st.Lhs[i]))
		}
		for i, l := range st.Lhs {
			w.pendingView = views[i]
			w.assignTo(l, vals[i], st.Pos())
		}
		return
	}
	vals := w.multi(st.Rhs[0], len(st.Lhs))
	view := w.exprView(st.Rhs[0])
	for i, l := range st.Lhs {
		if i < len(vals) {
			w.pendingView = view
			w.assignTo(l, vals[i], st.Pos())
		}
	}
}

// noteCallView records the locks a view call e returns is valid under.
func (w *walker) noteCallView(e *ast.CallExpr, held *lockset) {
	if held == nil {
		if w.callHeld != nil {
			delete(w.callHeld, e)
		}
		return
	}
	if w.callHeld == nil {
		w.callHeld = map[*ast.CallExpr]lockset{}
	}
	w.callHeld[e] = *held
}

// exprView returns, for an expression just evaluated, the locks of the
// view its value holds when that view was loaded before (identView) or
// returned by a call (section 19.3, Views loaded under a lock), or nil
// when its value holds no view, or one it loads itself.
func (w *walker) exprView(e ast.Expr) *lockset {
	e = ast.Unparen(e)
	switch n := e.(type) {
	case *ast.Ident:
		return w.identView(n)
	case *ast.CallExpr:
		if len(n.Args) == 1 && w.a.types[n.Fun].IsType() {
			return w.exprView(n.Args[0])
		}
		if held, ok := w.callHeld[n]; ok {
			return pu.New(held)
		}
	}
	return nil
}

// assignTo writes val to lhs.
func (w *walker) assignTo(lhs ast.Expr, val origins, pos token.Pos) {
	pv := w.pendingView
	w.pendingView = nil
	lhs = ast.Unparen(lhs)
	if id, ok := lhs.(*ast.Ident); ok {
		if id.Name == "_" {
			return
		}
		if v, ok := w.a.objOf(id).(*types2.Var); ok && !isGlobal(v) {
			if w.frame != nil {
				p := vpath{root: root{kind: rLocal, v: v}}
				w.access(p, true, false, id.Pos(), v.Type(), false, "")
				if !w.newVar(id) {
					w.dropWrite([]vpath{p}, v.Type(), id.Pos())
				}
				w.newScope(v)
			}
			w.cpuCarry(v, val, id.Pos())
			w.setVar(v, val)
			held := w.st.locks
			if pv != nil {
				held = held.meet(*pv)
			}
			w.st.setView(v, held, w.a.holdsView(v.Type(), 0) && w.loadedView(val))
			return
		}
	}
	if ix, ok := lhs.(*ast.IndexExpr); ok && w.isContainerIndex(ix) {
		w.value(ix.Index)
		w.methodCall(ix.X, types2.SetIndexMethodName, []origins{nil, val}, pos)
		return
	}
	isMap := w.isMapIndex(lhs)
	if isMap {
		// m[k] = v stores the key too.
		w.publishAt(w.value(lhs.(*ast.IndexExpr).Index), lhs.Pos(), "stored in a map")
	}
	ps, ok := w.place(lhs)
	if !ok {
		w.value(lhs)
		return
	}
	T := w.typeOf(lhs)
	w.useView(lhs)
	for _, p := range ps.ps {
		w.access(p, true, false, lhs.Pos(), T, ps.sync, ps.key)
	}
	w.vvalid = nil
	if !ps.sync && !isMap {
		// The value it replaces is dropped.
		w.dropWrite(ps.ps, T, lhs.Pos())
	}
	if !ps.sync && !isMap {
		// A map element is a copy: no view can see it change.
		w.noteViewWrite(lhs, ps.ps, T)
	}
	w.store(ps.ps, val, pos, T)
}

// isMapIndex reports whether e indexes a map.
func (w *walker) isMapIndex(e ast.Expr) bool {
	ix, ok := ast.Unparen(e).(*ast.IndexExpr)
	return ok && types2.MapOf(w.typeOf(ix.X)) != nil
}

// readWrite reads and writes e (x++, x += y).
func (w *walker) readWrite(e ast.Expr, pos token.Pos) {
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		if v, ok := w.a.objOf(id).(*types2.Var); ok && !isGlobal(v) {
			if w.frame != nil {
				p := vpath{root: root{kind: rLocal, v: v}}
				w.access(p, false, false, id.Pos(), v.Type(), false, "")
				w.access(p, true, false, id.Pos(), v.Type(), false, "")
			}
			return
		}
	}
	if ix, ok := e.(*ast.IndexExpr); ok && w.isContainerIndex(ix) {
		w.value(ix.Index)
		w.methodCall(ix.X, types2.IndexMethodName, []origins{nil}, pos)
		w.methodCall(ix.X, types2.SetIndexMethodName, []origins{nil, nil}, pos)
		return
	}
	ps, ok := w.place(e)
	if !ok {
		w.value(e)
		return
	}
	T := w.typeOf(e)
	w.useView(e)
	for _, p := range ps.ps {
		w.access(p, false, false, e.Pos(), T, ps.sync, ps.key)
		w.access(p, true, false, e.Pos(), T, ps.sync, ps.key)
	}
	w.vvalid = nil
	if !ps.sync && !w.isMapIndex(e) {
		w.noteViewWrite(e, ps.ps, T)
	}
}

// store records that val is stored at places ps: in a local variable, its
// origins grow; anywhere else, what val points to is published.
func (w *walker) store(ps []vpath, val origins, pos token.Pos, T types2.Type) {
	for _, p := range ps {
		if p.root.kind == rGlobal && !strings.ContainsAny(p.path, "*%$&#") && w.quiet == 0 {
			w.a.noteGlobalStore(p, val, T)
		}
		if p.root.kind == rLocal && localPath(p.path) {
			w.cpuCarry(p.root.v, val, pos)
			w.setVar(p.root.v, val.tagged2(fieldTag(p.path)))
			continue
		}
		w.publishAt(val, pos, "stored where it outlives the region")
	}
}

// localPath reports whether a path within a local variable stays in its
// frame memory: only fields and array elements.
func localPath(p string) bool {
	return !strings.ContainsAny(p, "*%$&#")
}

// fieldTag returns the fields of a path, without array steps.
func fieldTag(p string) string {
	var b compat.Builder
	for _, s := range steps(p) {
		if strings.HasPrefix(s, ".") {
			b.WriteString(s)
		}
	}
	return b.String()
}

// tagged2 marks the origins as held at field path tag ("" for the whole
// value).
func (s origins) tagged2(tag string) origins {
	if tag == "" {
		return s
	}
	var out origins
	for _, o := range s {
		o.tag = compat.Concat(tag, o.tag)
		if countSteps(o.tag) > maxSteps {
			o.tag = ""
		}
		out = origins(compat.Append(out, o))
	}
	return out.uniq()
}

// publish records that memory allocated here, which val may point to, is
// now reachable from somewhere else, so writes to it are no longer
// initialising writes. Publishing what a parameter points to is recorded
// for callers.
func (w *walker) publish(val origins) {
	for _, o := range val {
		switch o.vp.root.kind {
		case rFresh:
			if w.st.pub == nil {
				w.st.pub = map[ast.Node]bool{} // a dead state, after a return, has none
			}
			w.st.pub[o.vp.root.node] = true
		case rParam:
			if w.quiet == 0 {
				w.sum.addPubParam(o.vp.root.idx)
			} else {
				w.sum.addPubParam(o.vp.root.idx)
			}
		}
	}
}

// isNil reports whether e is the predeclared nil.
func isNil(a *analyzer, e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	_, isNil := a.objOf(id).(*types2.Nil)
	return isNil
}

// noteFreshResult records whether result i returns memory this function
// allocated and has not published: the caller gets it as its own.
func (w *walker) noteFreshResult(i int, v origins) {
	fresh := len(v) > 0 && !w.st.pubAll
	for _, o := range v {
		if o.vp.root.kind != rFresh || o.vp.path != "&" || !w.created[o.vp.root.node] || w.st.pub[o.vp.root.node] {
			fresh = false
		}
	}
	w.sum.noteFresh(i, fresh)
}

// cond evaluates a condition and returns the states where it is true and
// false. A TryLock in the condition holds the lock where it succeeded.
func (w *walker) cond(e ast.Expr) (t, f *state) {
	neg := false
	x := ast.Unparen(e)
	for {
		u, ok := x.(*ast.UnaryExpr)
		if !ok || u.Op != token.NOT {
			break
		}
		neg = !neg
		x = ast.Unparen(u.X)
	}
	if c, ok := x.(*ast.CallExpr); ok {
		if op, recv := w.lockOp(c); op == "trylock" || op == "tryrlock" {
			ps := w.lockPlaces(recv)
			w.syncUse(ps.ps, c.Pos())
			held := w.st.clone()
			if len(ps.ps) == 1 && !ps.sync {
				held.locks = held.locks.with(ps.ps[0], op == "trylock")
				w.noteLocked(ps.ps[0])
			}
			if w.isRTLock(recv) {
				held.locks = held.locks.with(regionAt(regLock, c.Pos()), true)
				w.noteLocked(regionAny(regLock))
			}
			if neg {
				return w.st.clone(), held
			}
			return held, w.st.clone()
		}
	}
	w.value(e)
	return w.st.clone(), w.st.clone()
}

// ----------------------------------------------------------------------------
// Tasks and channels

func (w *walker) taskStmt(st *ast.TaskStmt) {
	c := st.Call
	desc := compat.Concat("task ", exprText(c.Fun))
	if tv := w.a.types[c.Fun]; tv.IsBuiltin() || tv.IsType() {
		w.call(c)
		return
	}
	t := w.target(c)
	args := w.args(c, t.sig)
	w.cpuTaskStore(c)
	exprs := argExprs(c, t.sig)
	var all []origins
	if t.hasRecv {
		all = compat.Append(all, t.recv)
		RT := w.typeOf(t.recvExpr)
		if wr, ok := underlying(RT).(*types2.Wrapper); ok && wr.IsPointer() {
			// task p.M() on an owned or synced box hands the box itself to the
			// task: it moves, or is counted (language spec section 19.2).
		} else if t.static != nil && len(t.static.params) > 0 && t.static.sig.Recv() != nil {
			RT = t.static.params[0].Type()
		}
		w.cross(pu.New(crossing{pos: st.Pos(), what: compat.Concat("the receiver ", exprText(t.recvExpr), " of ", desc), typ: RT, vals: t.recv, expr: t.recvExpr}))
	}
	n := t.sig.Params().Len()
	packed_ := t.sig.Variadic() && !c.Ellipsis.IsValid() && n > 0
	for i, a := range args {
		var e ast.Expr
		if i < len(exprs) {
			e = exprs[i]
		}
		if packed_ && i == n-1 {
			// Packed variadic arguments are copied into the new task's
			// own array: each element crosses as a value.
			elem := t.sig.Params().At(n - 1).Type().Underlying().(*types2.Slice).Elem()
			for _, x := range c.Args[min(n-1, len(c.Args)):] {
				w.quiet++
				v := w.value(x)
				w.quiet--
				w.cross(pu.New(crossing{pos: st.Pos(), what: compat.Concat("the argument ", exprText(x), " of ", desc), typ: elem, vals: v, expr: x}))
			}
			continue
		}
		what := compat.Sprintfb("argument %d of %s", compat.ArgI(int64(i+1)), compat.ArgS(desc))
		if e != nil {
			what = compat.Concat("the argument ", exprText(e), " of ", desc)
		}
		var T types2.Type
		if i < t.sig.Params().Len() {
			T = t.sig.Params().At(i).Type()
		}
		w.cross(pu.New(crossing{pos: st.Pos(), what: what, typ: T, vals: a, expr: e}))
	}
	all = compat.Append(all, args...)
	for _, a := range all {
		w.publishAt(a, st.Pos(), "passed to a new task")
	}
	var roots origins
	switch {
	case t.static != nil:
		roots = origins(compat.Append(roots, t.static.asValue()))
	case t.method != "":
		for _, f := range w.a.methodCandidates(t.method, t.sig) {
			roots = origins(compat.Append(roots, f.asValue()))
		}
	default:
		roots = t.fv
		if len(roots) == 0 {
			for _, f := range w.a.valueCandidates(t.sig, false) {
				roots = origins(compat.Append(roots, f.asValue()))
			}
		}
	}
	w.spawn(pu.New(spawn{pos: st.Pos(), root: roots, args: all, multi: w.loops > 0 || !w.isMain(), desc: desc}))
}

// isMain reports whether the function is the program's main, which runs
// once.
func (w *walker) isMain() bool {
	return w.fn.obj != nil && w.fn.obj.Name() == "main" && w.fn.obj.Pkg() == w.a.prog.Main.Types && w.fn.decl.Recv == nil
}

func (w *walker) spawn(s *spawn) {
	if w.quiet == 0 {
		w.sum.addSpawn(s)
	}
	w.sum.maySpawn = true
	w.st.spawned = true
}

func (w *walker) cross(c *crossing) {
	c.fn = w.fn.name
	if w.quiet == 0 {
		w.sum.addCrossing(c)
	}
}

func (f *function) asValue() origin {
	if f.lit != nil {
		return origin{vp: vpath{root: root{kind: rLit, node: f.lit}}}
	}
	return origin{vp: vpath{root: root{kind: rFunc, fn: f.obj}}}
}

// argExprs returns, for each parameter of sig, the argument expression a
// call passes for it, or nil where several are packed into a variadic
// parameter.
func argExprs(e *ast.CallExpr, sig *types2.Signature) []ast.Expr {
	n := sig.Params().Len()
	out := compat.Make[ast.Expr](n)
	if len(e.Args) == 1 && n > 1 {
		return out
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

func exprText(e ast.Expr) string {
	if e == nil {
		return ""
	}
	return types2.ExprString(e)
}

// ----------------------------------------------------------------------------
// Accesses

// access records a use of place p of type T. Uses of frame memory and of
// memory allocated by this function are not shared; uses inside a
// synchronised type (inSync) are its own business.
func (w *walker) access(p vpath, write, atomic bool, pos token.Pos, T types2.Type, inSync bool, key string) {
	if inSync || w.quiet > 0 {
		return
	}
	if w.frame != nil {
		w.frame.note(w, pu.New(frameAcc{place: p, write: write, atomic: atomic, free: w.freeing > 0, pos: pos, typ: T, locks: w.st.locks}))
		return
	}
	if p.root.kind == rGlobal && w.a.cpuVars[p.root.v] {
		if w.viewLoad && !write && !hasDeref(p.path) {
			w.sum.addAccess(cpuViewLoad(p, pos, T, w.st.locks, w.st.rel, !w.st.spawned))
		}
		var ok bool
		if p, ok = w.cpuPlace(p, pos, w.st.locks, w.st.rel, !w.st.spawned, false, write, nil, w.sum.addAccess); !ok {
			return
		}
	}
	init := w.initializing(p, w.st)
	if key != "" {
		// A use in main before it starts a task is before the start of
		// sharing: the type rule asks only what changes once an object is
		// shared (section 19.3, Rule 3).
		w.a.recordType(key, p, write, atomic, w.st.locks, pos, w.fn.name, init || (w.isMain() && !w.st.spawned))
	}
	if init || !w.a.shareable(p) {
		// Memory this function allocated and has not handed to anyone:
		// no other task can reach it yet.
		return
	}
	x := pu.New(access{place: p, write: write, atomic: atomic, locks: w.st.locks, rel: w.st.rel, pos: pos, pre: !w.st.spawned, tkey: key, typ: T, free: w.freeing > 0, view: w.viewLoad && !write, sync: w.syncing})
	if w.vvalid != nil {
		x.hasValid, x.vvalid, x.vpre = true, *w.vvalid, w.vpre
	}
	if !write && !atomic && isSyncType(T) {
		// Reading a whole synchronised value copies it, as any read does.
		x.atomic = false
	}
	w.sum.addAccess(x)
}

// initializing reports whether an access of place p in state st uses
// memory this function allocated and has not yet stored, passed or
// returned anywhere: no other task can reach it yet.
func (w *walker) initializing(p vpath, st *state) bool {
	if p.root.kind != rFresh || st.pubAll {
		return false
	}
	n := p.root.node
	path := p.path
	if path != "" && (path[0] == '$' || path[0] == '*') {
		// The object of a box allocated here.
		path = strings.SliceFrom(path, stepEnd(path, 0))
	}
	return w.created[n] && !st.pub[n] && !strings.ContainsAny(path, "*%$#")
}

// unknownFor returns the root of memory reached through x when the
// analysis does not know where x points: named by x's variable, so that a
// lock and a field reached through one variable compare.
func (w *walker) unknownFor(x ast.Expr) root {
	if id, ok := ast.Unparen(x).(*ast.Ident); ok {
		if v, ok := w.a.objOf(id).(*types2.Var); ok && !isGlobal(v) {
			return root{kind: rUnknown, v: v}
		}
	}
	if x == nil {
		return root{kind: rUnknown}
	}
	return root{kind: rUnknown, pos: x.Pos()}
}

// shareable reports whether a place may be used by several tasks: memory
// reached from a package variable or a parameter, or a synced object's.
func (a *analyzer) shareable(p vpath) bool {
	if strings.Contains(p.path, "$") {
		return true
	}
	switch p.root.kind {
	case rGlobal:
		return !a.runtimeVar(p.root.v)
	case rParam:
		return true
	}
	return false
}

// runtimeVar reports whether v belongs to the runtime, which implements
// the tasks themselves (the scheduler, allocators, timers) and keeps its
// own variables with preemption control, per-processor data and its own
// locks, outside these rules (as its unsafe code is).
func (a *analyzer) runtimeVar(v *types2.Var) bool {
	if v.Pkg() == nil {
		return false
	}
	p := v.Pkg().Path()
	return p == "pluto/rt" || strings.HasPrefix(p, "pluto/rt/")
}

// chanUse records a send, receive or close on the channel e holds: an
// atomic access of the memory that holds the channel, which conflicts only
// with a plain write of it (section 19.3, Accesses).
func (w *walker) chanUse(e ast.Expr, pos token.Pos) {
	ps, ok := w.place(e)
	if !ok || ps.sync {
		return
	}
	T := w.typeOf(e)
	w.syncing = true
	for _, p := range ps.ps {
		w.access(p, false, true, pos, T, false, "")
	}
	w.syncing = false
}

// syncUse records a use of a lock or other synchronised value through its
// methods: an atomic access, which conflicts only with plain writes of it.
func (w *walker) syncUse(ps []vpath, pos token.Pos) {
	w.syncing = true
	for _, p := range ps {
		w.access(p, false, true, pos, nil, false, "")
	}
	w.syncing = false
}

// ----------------------------------------------------------------------------
// Expressions

func (w *walker) typeOf(e ast.Expr) types2.Type { return w.a.typeOf(e) }

// underlying returns T's underlying type, looking through volatile and
// aligned, which change how a value is stored but not what it is.
func underlying(T types2.Type) types2.Type {
	if T == nil {
		return nil
	}
	for {
		u := T.Underlying()
		wr, ok := u.(*types2.Wrapper)
		if !ok || (wr.Kind() != types2.WrapVolatile && wr.Kind() != types2.WrapAligned) {
			return u
		}
		T = wr.Elem()
	}
}

// derefStep returns the path step for the memory a value of type T points
// to.
func (w *walker) derefStep(T types2.Type) string {
	switch u := underlying(T).(type) {
	case *types2.Slice:
		return "%"
	case *types2.Wrapper:
		if u.Kind() == types2.WrapSynced {
			return compat.Concat("$", itoa(w.a.typeID(u.Elem())))
		}
	case *types2.Map:
		return compat.Concat("$", itoa(w.a.mapTypeID(u)))
	case *types2.Interface:
		if m := types2.MapOf(T); m != nil {
			return compat.Concat("$", itoa(w.a.mapTypeID(m))) // a type parameter whose types are maps
		}
	}
	return "*"
}

// pointerElem returns what a pointer-like type points to, or nil.
func pointerElem(T types2.Type) types2.Type {
	if T == nil {
		return nil
	}
	switch u := underlying(T).(type) {
	case *types2.Pointer:
		return u.Elem()
	case *types2.Wrapper:
		if u.IsPointer() {
			return u.Elem()
		}
	}
	return nil
}

// value walks e and returns its origins.
func (w *walker) value(e ast.Expr) origins {
	if e == nil {
		return nil
	}
	v := w.value1(e)
	if T := w.typeOf(e); T != nil {
		switch u := underlying(T).(type) {
		case *types2.Chan:
			// A channel is a handle to the runtime's memory, and a
			// string's bytes never change: neither is a view.
			return nil
		case *types2.Basic:
			if u.Kind() != types2.UnsafePointer {
				return nil
			}
		}
	}
	return v
}

func (w *walker) value1(e ast.Expr) origins {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return w.value(e.X)
	case *ast.Ident:
		switch obj := w.a.objOf(e).(type) {
		case *types2.Var:
			if isGlobal(obj) {
				p := vpath{root: root{kind: rGlobal, v: obj}}
				w.cpuSite(e, obj)
				w.viewLoad = w.a.holdsView(obj.Type(), 0)
				w.access(p, false, false, e.Pos(), obj.Type(), false, "")
				w.viewLoad = false
				return compat.Clone(origins{{vp: p}})
			}
			if w.frame != nil {
				w.frame.noteVar(w, obj, e)
			}
			return w.u.vars[obj]
		case *types2.Func:
			if f := w.a.funcs[obj.Origin()]; f != nil {
				return compat.Clone(origins{f.asValue()})
			}
		}
		return nil
	case *ast.BasicLit:
		return nil
	case *ast.FuncLit:
		if f := w.a.lits[e]; f != nil {
			return compat.Clone(origins{f.asValue()})
		}
		return nil
	case *ast.CompositeLit:
		return w.compLit(e)
	case *ast.SelectorExpr:
		sel := w.a.selections[e]
		if sel == nil {
			return w.value(e.Sel) // a qualified identifier
		}
		switch sel.Kind() {
		case types2.FieldVal:
			if ps, ok := w.place(e); ok {
				T := w.typeOf(e)
				w.viewLoad = !w.ownedField(e) && w.a.holdsView(T, 0)
				w.useView(e)
				for _, p := range ps.ps {
					w.access(p, false, false, e.Pos(), T, ps.sync, ps.key)
				}
				w.viewLoad, w.vvalid = false, nil
				return w.fieldValue(ps, e)
			}
			// A field of a value that is not addressable.
			v := w.value(e.X)
			T := w.typeOf(e.X)
			for _, idx := range sel.Index() {
				f := fieldOf(T, idx)
				if f == nil {
					break
				}
				v = v.field(f.Name())
				T = f.Type()
			}
			return v
		case types2.MethodVal:
			return w.methodValue(e, sel)
		case types2.MethodExpr:
			if fn, ok := sel.Obj().(*types2.Func); ok {
				if f := w.a.funcs[fn.Origin()]; f != nil {
					return compat.Clone(origins{f.asValue()})
				}
			}
		}
		return nil
	case *ast.IndexExpr, *ast.IndexListExpr:
		var x ast.Expr
		var index []ast.Expr
		if ix, ok := e.(*ast.IndexExpr); ok {
			x, index = ix.X, []ast.Expr{ix.Index}
		} else {
			ix := e.(*ast.IndexListExpr)
			x, index = ix.X, ix.Indices
		}
		if _, ok := w.typeOf(x).Underlying().(*types2.Signature); ok {
			return w.value(x) // a generic function instantiated as a value
		}
		if ix, ok := e.(*ast.IndexExpr); ok && w.isContainerIndex(ix) {
			w.value(ix.Index)
			return w.methodCall(ix.X, types2.IndexMethodName, []origins{nil}, ix.Pos())
		}
		for _, i := range index {
			w.value(i)
		}
		if ps, ok := w.place(e); ok {
			T := w.typeOf(e)
			w.viewLoad = w.a.holdsView(T, 0)
			w.useView(e)
			for _, p := range ps.ps {
				w.access(p, false, false, e.Pos(), T, ps.sync, ps.key)
			}
			w.viewLoad, w.vvalid = false, nil
			return w.placeValue(ps.ps)
		}
		v := w.value(x)
		if _, ok := underlying(w.typeOf(x)).(*types2.Basic); ok {
			return nil // a byte of a string
		}
		return v
	case *ast.SliceExpr:
		for _, i := range []ast.Expr{e.Low, e.High, e.Max} {
			w.value(i)
		}
		part := w.slicePart(e)
		switch u := underlying(w.typeOf(e.X)).(type) {
		case *types2.Array:
			if ps, ok := w.place(e.X); ok {
				return w.addrsOfPlaces(extendAll(ps.ps, compat.Concat("[]", part)))
			}
			return w.value(e.X)
		case *types2.Pointer:
			if _, ok := underlying(u.Elem()).(*types2.Array); ok {
				return addrsOf(extendAll(w.value(e.X).derefOr("*", w.unknownFor(e.X)), compat.Concat("[]", part)))
			}
		case *types2.Slice:
			if part != "" {
				return addrsOf(extendAll(w.value(e.X).derefOr("%", w.unknownFor(e.X)), part))
			}
		}
		return w.value(e.X)
	case *ast.StarExpr:
		ps, _ := w.place(e)
		T := w.typeOf(e)
		w.viewLoad = w.a.holdsView(T, 0)
		w.useView(e)
		for _, p := range ps.ps {
			w.access(p, false, false, e.Pos(), T, ps.sync, ps.key)
		}
		w.viewLoad, w.vvalid = false, nil
		return w.placeValue(ps.ps)
	case *ast.UnaryExpr:
		switch e.Op {
		case token.AND:
			return w.addrOf(e.X)
		case token.ARROW:
			w.value(e.X)
			w.chanUse(e.X, e.Pos())
			w.parkPoint()
			return nil
		}
		w.value(e.X)
		return nil
	case *ast.BinaryExpr:
		w.value(e.X)
		w.value(e.Y)
		if e.Op == token.ADD && isString(w.typeOf(e)) && w.a.types[e].Value.IsNil() {
			w.sum.mayAlloc = true // a new string
		}
		return nil
	case *ast.CallExpr:
		res := w.call(e)
		if len(res) == 0 {
			return nil
		}
		return unionOrigins(res...)
	case *ast.TypeAssertExpr:
		return w.value(e.X)
	case *ast.KeyValueExpr:
		w.value(e.Key)
		return w.value(e.Value)
	case *ast.BadExpr, *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType,
		*ast.MapType, *ast.ChanType, *ast.Ellipsis:
		return nil
	}
	panic(pu.Box(compat.Sprintf("race: unsupported expression %T", e)))
}

// ownedField reports whether the field e selects is a view field that
// owns what it points to (ownedview.go): loading it is a use of memory the
// object owns, not a crossing of a view (section 19.3, case 4 of rule 2).
func (w *walker) ownedField(e *ast.SelectorExpr) bool {
	sel := w.a.selections[e]
	if sel == nil {
		return false
	}
	idx := sel.Index()
	T := w.typeOf(e.X)
	for _, i := range idx[:len(idx)-1] {
		if f := fieldOf(T, i); f != nil {
			T = f.Type()
		}
	}
	if el := pointerElem(T); el != nil {
		T = el
	}
	return w.a.ownsField(T, fieldOf(T, idx[len(idx)-1]))
}

// holdsView reports whether a value of type T holds a view: a pointer, a
// slice, an interface, a func value, or an aggregate that holds one.
// Memory an owner owns is not counted: it moves with the owner.
func (a *analyzer) holdsView(T types2.Type, depth int) bool {
	if T == nil || depth > 8 {
		return false
	}
	switch u := underlying(T).(type) {
	case *types2.Pointer, *types2.Slice, *types2.Interface, *types2.Signature:
		return true
	case *types2.Basic:
		return u.Kind() == types2.UnsafePointer
	case *types2.Array:
		return a.holdsView(u.Elem(), depth+1)
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			f := u.Field(i)
			if a.ownsField(T, f) {
				continue
			}
			if a.holdsView(f.Type(), depth+1) {
				return true
			}
		}
	case *types2.Wrapper:
		if u.Kind() == types2.WrapVolatile || u.Kind() == types2.WrapAligned {
			return a.holdsView(u.Elem(), depth+1)
		}
	}
	return false
}

// viewOf returns, when the memory e uses is reached through a view held in
// a local that was loaded from shared memory, the locks held from the
// load to here with no ending event (section 19.3, Views loaded under a
// lock). The view is the operand of the outermost dereference in e: a
// view loaded on the way, as in p.next.v, is loaded here.
func (w *walker) useView(e ast.Expr) {
	w.vvalid = w.viewOf(e)
	w.vpre = w.vvalid != nil && w.viewPre
}

func (w *walker) viewOf(e ast.Expr) *lockset {
	for {
		var x ast.Expr
		switch n := ast.Unparen(e).(type) {
		case *ast.StarExpr:
			x = n.X
		case *ast.SelectorExpr:
			if pointerElem(w.typeOf(n.X)) == nil {
				e = n.X
				continue
			}
			x = n.X
		case *ast.IndexExpr:
			switch underlying(w.typeOf(n.X)).(type) {
			case *types2.Array:
				e = n.X
				continue
			case *types2.Slice, *types2.Pointer:
				x = n.X
			default:
				return nil
			}
		default:
			return nil
		}
		return w.identView(x)
	}
}

// identView returns, when e is a local holding a view loaded from shared
// memory, the locks held from the load to here with no ending event.
func (w *walker) identView(e ast.Expr) *lockset {
	w.viewPre = false
	if dv := w.deferVars[e]; dv != nil && w.deferCall != nil {
		if held, ok := w.st.views[dv]; ok {
			w.viewPre = w.st.vpre[dv]
			m := held.meet(w.st.locks)
			return pu.New(m)
		}
		return nil
	}
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil
	}
	v, ok := w.a.objOf(id).(*types2.Var)
	if !ok || isGlobal(v) {
		return nil
	}
	held, ok := w.st.views[v]
	if !ok {
		return nil
	}
	w.viewPre = w.st.vpre[v]
	m := held.meet(w.st.locks)
	return pu.New(m)
}

// argViews returns, for each argument of call e (receiver first), the
// locks of the view it passes (identView), or nil when it passes none.
func (w *walker) argViews(e *ast.CallExpr, t callTarget) []*lockset {
	if t.sig == nil || t.sig.Variadic() {
		return nil
	}
	var out []*lockset
	found := false
	if t.hasRecv {
		var v *lockset
		if t.recvExpr != nil && pointerElem(w.typeOf(t.recvExpr)) != nil {
			v = w.identView(t.recvExpr)
		}
		found = v != nil
		out = compat.Append(out, v)
	}
	for _, a := range e.Args {
		v := w.identView(a)
		found = found || v != nil
		out = compat.Append(out, v)
	}
	if !found {
		return nil
	}
	return out
}

// loadedView reports whether a value with origins val holds a view loaded
// from memory: the value of a pointer stored in memory, or an address
// within what such a pointer points to. A parameter's own value, and an
// address taken within what it points to, were not loaded here.
func (w *walker) loadedView(val origins) bool {
	for _, o := range val {
		d := 0
		switch o.vp.root.kind {
		case rGlobal, rFresh, rLocal:
			d = 1 // memory: its value is loaded
		case rParam:
		default:
			continue
		}
		for _, st := range steps(o.vp.path) {
			if st == "*" || st == "%" || st[0] == '$' || st[0] == '#' {
				d++
			}
		}
		if d >= 2 || d == 1 && !strings.HasSuffix(o.vp.path, "&") {
			return true
		}
	}
	return false
}

// fieldValue returns the value of a field selection whose places are ps.
// A view field of an owner that owns what it points to (ownedview.go)
// points into memory the owner owns: everything it reaches is one place,
// the owner's.
func (w *walker) fieldValue(ps places, e *ast.SelectorExpr) origins {
	sel := w.a.selections[e]
	idx := sel.Index()
	T := w.typeOf(e.X)
	for _, i := range idx[:len(idx)-1] {
		if f := fieldOf(T, i); f != nil {
			T = f.Type()
		}
	}
	if w.a.ownsField(T, fieldOf(T, idx[len(idx)-1])) {
		var out origins
		for _, p := range ps.ps {
			out = origins(compat.Append(out, origin{vp: p.extend("#").addr()}))
		}
		return out.uniq()
	}
	return w.placeValue(ps.ps)
}

// placeValue returns the origins of the values stored at places ps.
func (w *walker) placeValue(ps []vpath) origins {
	var out origins
	for _, p := range ps {
		if p.root.kind == rLocal && localPath(p.path) {
			v := w.u.vars[p.root.v]
			for _, s := range steps(p.path) {
				if strings.HasPrefix(s, ".") {
					v = v.field(strings.SliceFrom(s, 1))
				}
			}
			out = origins(compat.Append(out, v...))
			continue
		}
		out = origins(compat.Append(out, origin{vp: p}))
	}
	return out.uniq()
}

func extendAll(ps []vpath, step string) []vpath {
	out := compat.MakeCap[vpath](0, len(ps))
	for _, p := range ps {
		out = compat.Append(out, p.extend(step))
	}
	return out
}

// places is the result of finding where an lvalue is: the places it may
// be, and whether it is inside a synchronised type.
type places struct {
	ps   []vpath
	sync bool
	key  string // "field:T.f": the first field after the last pointer followed, for the type rule
}

// place returns where an addressable expression is.
func (w *walker) place(e ast.Expr) (places, bool) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return w.place(e.X)
	case *ast.Ident:
		v, ok := w.a.objOf(e).(*types2.Var)
		if !ok {
			return places{}, false
		}
		if isGlobal(v) {
			w.cpuSite(e, v)
			return places{ps: compat.Clone([]vpath{{root: root{kind: rGlobal, v: v}}})}, true
		}
		return places{ps: compat.Clone([]vpath{{root: root{kind: rLocal, v: v}}})}, true
	case *ast.SelectorExpr:
		sel := w.a.selections[e]
		if sel == nil {
			return w.place(e.Sel)
		}
		if sel.Kind() != types2.FieldVal {
			return places{}, false
		}
		return w.fieldPlace(e.X, sel.Index())
	case *ast.IndexExpr:
		if w.isContainerIndex(e) {
			return places{}, false
		}
		w.value(e.Index)
		if xt := w.typeOf(e.X); types2.MapOf(xt) != nil {
			// The map's contents: one place for all its elements, like
			// the object of a synced[T] (checkMaps).
			return places{ps: w.value(e.X).derefOr(w.derefStep(xt), w.unknownFor(e.X))}, true
		}
		part := w.indexPart(e.Index)
		switch u := underlying(w.typeOf(e.X)).(type) {
		case *types2.Array:
			ps, ok := w.place(e.X)
			if !ok {
				return places{}, false
			}
			ps.ps = extendAll(ps.ps, compat.Concat("[]", part))
			return ps, true
		case *types2.Pointer:
			if _, ok := underlying(u.Elem()).(*types2.Array); ok {
				return places{ps: extendAll(w.value(e.X).derefOr("*", w.unknownFor(e.X)), compat.Concat("[]", part))}, true
			}
		case *types2.Slice:
			ps := w.value(e.X).derefOr("%", w.unknownFor(e.X))
			if part != "" {
				ps = extendAll(ps, part)
				w.noteView(e)
			}
			return places{ps: ps}, true
		}
		return places{}, false
	case *ast.StarExpr:
		return places{ps: w.value(e.X).derefOr(w.derefStep(w.typeOf(e.X)), w.unknownFor(e.X))}, true
	case *ast.CompositeLit:
		w.compLit(e)
		return places{ps: compat.Clone([]vpath{w.alloc(e)})}, true
	}
	return places{}, false
}

// fieldPlace follows a field selection's path from x.
func (w *walker) fieldPlace(x ast.Expr, path []int) (places, bool) {
	T := w.typeOf(x)
	var cur places
	if el := pointerElem(T); el != nil {
		cur = places{ps: w.value(x).derefOr(w.derefStep(T), w.unknownFor(x))}
		T = el
	} else if ps, ok := w.place(x); ok {
		cur = ps
	} else {
		return places{}, false
	}
	for i, idx := range path {
		f := fieldOf(T, idx)
		if f == nil {
			return places{}, false
		}
		if isSyncType(T) {
			cur.sync = true
		}
		if len(cur.ps) > 0 && objectBase(cur.ps[0]) == cur.ps[0] {
			// The first field of the object a pointer led to.
			S := T
			if el := pointerElem(S); el != nil {
				S = el
			}
			cur.key = compat.Concat("field:", typeKey(S), ".", f.Name())
		}
		cur.ps = extendAll(cur.ps, compat.Concat(".", f.Name()))
		T = f.Type()
		if i < len(path)-1 {
			if el := pointerElem(T); el != nil {
				// An embedded pointer: load it.
				cur.ps = w.placeValue(cur.ps).derefOr(w.derefStep(T), root{kind: rUnknown, pos: x.Pos(), idx: i})
				T = el
			}
		}
	}
	return cur, true
}

// fieldOf returns field idx of struct type T (or of what T points to).
func fieldOf(T types2.Type, idx int) *types2.Var {
	if el := pointerElem(T); el != nil {
		T = el
	}
	s, ok := underlying(T).(*types2.Struct)
	if !ok || idx >= s.NumFields() {
		return nil
	}
	return s.Field(idx)
}

// addrOf returns a pointer to e.
func (w *walker) addrOf(e ast.Expr) origins {
	e = ast.Unparen(e)
	if cl, ok := e.(*ast.CompositeLit); ok {
		w.compLit(cl)
		return compat.Clone(origins{{vp: w.alloc(cl).addr()}})
	}
	ps, ok := w.place(e)
	if !ok {
		w.value(e)
		return nil
	}
	for _, p := range ps.ps {
		if p.root.kind == rGlobal && !throughView(p.path) {
			w.a.addrTaken(p.root.v)
		}
	}
	return w.addrsOfPlaces(ps.ps)
}

// throughView reports whether a place at path from a variable is memory a
// view the variable holds points to, not the variable's own storage.
func throughView(path string) bool {
	i := strings.IndexAny(path, "*%$")
	j := strings.IndexByte(path, '#')
	return i >= 0 && (j < 0 || i < j)
}

func (a *analyzer) addrTaken(v *types2.Var) {
	if a.addrGlobals == nil {
		a.addrGlobals = map[*types2.Var]bool{}
	}
	a.addrGlobals[v] = true
}

// literalFields returns the fields that the key of a struct literal of
// type T selects, through embedded structs (types2.LiteralFields).
func (w *walker) literalFields(T types2.Type, key *ast.Ident) []*types2.Var {
	var pkg *types2.Package
	if f, ok := w.a.uses[key].(*types2.Var); ok {
		pkg = f.Pkg()
	}
	return types2.LiteralFields(T, pkg, key.Name)
}

// compLit walks a composite literal and returns its value's origins.
func (w *walker) compLit(e *ast.CompositeLit) origins {
	T := w.typeOf(e)
	if el := pointerElem(T); el != nil {
		T = el
	}
	var out origins
	switch u := underlying(T).(type) {
	case *types2.Struct:
		for i, el := range e.Elts {
			x := el
			name := ""
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				x = kv.Value
				if id, ok := kv.Key.(*ast.Ident); ok {
					name = id.Name
					if path := w.literalFields(T, id); len(path) > 1 {
						// A promoted field (language spec section
						// 11.3): held in the embedded fields around it.
						f := path[len(path)-1]
						v := w.value(x)
						w.conv(x, f.Type())
						for i := len(path) - 1; i >= 0; i-- {
							v = v.tagged(path[i].Name())
						}
						out.addAll(v)
						continue
					}
				}
			} else if i < u.NumFields() {
				name = u.Field(i).Name()
			}
			v := w.value(x)
			if name != "" {
				out.addAll(v.tagged(name))
				for j := 0; j < u.NumFields(); j++ {
					if u.Field(j).Name() == name {
						w.conv(x, u.Field(j).Type())
					}
				}
			}
		}
		return out
	case *types2.Slice:
		// The elements are in memory of the literal's own.
		site := w.alloc(e)
		for _, el := range e.Elts {
			w.value(el)
		}
		return compat.Clone(origins{{vp: site.addr()}})
	case *types2.Map:
		// A new map: its keys and elements are stored in its table.
		site := w.alloc(e)
		for _, el := range e.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				w.publishAt(w.value(kv.Key), kv.Key.Pos(), "stored in a map")
				w.publishAt(w.value(kv.Value), kv.Value.Pos(), "stored in a map")
			}
		}
		return compat.Clone(origins{{vp: site}})
	}
	for _, el := range e.Elts {
		out.addAll(w.value(el))
	}
	return out.untag()
}

// alloc records an allocation made here: memory nothing else can reach
// until it is published.
func (w *walker) alloc(n ast.Node) vpath {
	if w.created == nil {
		w.created = map[ast.Node]bool{}
	}
	w.created[n] = true
	w.sum.mayAlloc = true
	delete(w.st.pub, n)
	return vpath{root: root{kind: rFresh, node: n}}
}

// methodValue evaluates x.M used as a value. The method may be called at
// any time later, so what it does is counted here, with no locks held.
func (w *walker) methodValue(e *ast.SelectorExpr, sel *types2.Selection) origins {
	fn, _ := sel.Obj().(*types2.Func)
	if fn == nil {
		return nil
	}
	recv := w.value(e.X)
	if r := fn.Type().(*types2.Signature).Recv(); r != nil && pointerElem(r.Type()) != nil && pointerElem(w.typeOf(e.X)) == nil {
		recv = w.addrOf(e.X)
	}
	cands := []*function{w.a.funcs[fn.Origin()]}
	if r := fn.Type().(*types2.Signature).Recv(); r != nil && isInterface(r.Type()) {
		cands = w.a.methodCandidates(fn.Name(), fn.Type().(*types2.Signature))
	}
	free := &state{rel: released{all: true}, spawned: true, pub: map[ast.Node]bool{}}
	var out origins
	for _, f := range cands {
		if f == nil {
			continue
		}
		w.apply(f, compat.Clone([]origins{recv}), e.Pos(), exprText(e), free, nil)
		out.add(f.asValue())
	}
	return out
}

// multi evaluates an expression with several results.
func (w *walker) multi(e ast.Expr, n int) []origins {
	e = ast.Unparen(e)
	switch e := e.(type) {
	case *ast.CallExpr:
		res := w.call(e)
		for len(res) < n {
			res = compat.Append(res, nil)
		}
		return res
	case *ast.IndexExpr:
		if w.isContainerIndex(e) {
			w.value(e.Index)
			r := w.methodCall(e.X, types2.IndexOkMethodName, []origins{nil}, e.Pos())
			return compat.Clone([]origins{r, nil})
		}
	}
	v := w.value(e)
	out := compat.Clone([]origins{v})
	for len(out) < n {
		out = compat.Append(out, nil)
	}
	return out
}

func (w *walker) isContainerIndex(e *ast.IndexExpr) bool {
	tv, ok := w.a.types[e.X]
	return ok && tv.Type != nil && types2.IndexesByMethod(tv.Type)
}

// loopBody is the argument a range-over-func loop passes its iterator: the
// loop body, walked where it is written, so calls of it add nothing.
func loopBody(st *ast.RangeStmt) []origins {
	return compat.Clone([]origins{compat.Clone(origins{{vp: vpath{root: root{kind: rLit, node: st}}}})})
}

// isView reports whether T is a pointer or a slice.
func isView(T types2.Type) bool {
	switch underlying(T).(type) {
	case *types2.Pointer, *types2.Slice:
		return true
	}
	return false
}
