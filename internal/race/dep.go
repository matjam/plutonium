package race

import (
	"sort"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Dependent stores of per-CPU variables (language spec section 20.9,
// decision D37).
//
// A store to a per-CPU variable whose value, or whose controlling condition,
// depends on an earlier load of the same variable is a read-modify-write: the
// task may move to another processor between the load and the store. This
// file finds those stores, with a taint analysis over each function body that
// follows the values loaded from per-CPU variables through local variables,
// struct copies, arithmetic and calls, and the conditions that control a
// statement. The walker (walk.go, region.go) then checks that the load and
// the store are inside one region.
//
// The analysis is conservative where it cannot see:
//   - the result of a call depends on every argument and on the receiver, and
//     on every per-CPU variable the callee, or a function it calls, loads;
//   - a call through an interface depends on what every method of that name
//     loads; a call of a func value, or of a function without a body, depends
//     on its arguments only and the error says that the call was not seen;
//   - a value stored through a pointer, a slice, a map, a channel or into a
//     package variable is kept in one shared pool, read back by every load of
//     such memory;
//   - the statements that follow a branch that may leave the function, or the
//     loop, are controlled by its condition.

// A cpuLoad is a source of dependence: a load of a per-CPU variable by name,
// or a call of a function that loads it.
type cpuLoad struct {
	v      *types2.Var
	pos    token.Pos
	call   *ast.CallExpr // set for a call of a function that loads v
	callee string
	feeds  bool // some dependent store depends on it: the walker records the regions held here
}

// A depElem is one source a value depends on.
type depElem struct {
	src    int    // index into analyzer.cpuLoads, or -(i+1) for parameter i
	ctrl   bool   // reached through a controlling condition
	opaque string // the call, whose body cannot be seen, the dependence passed through
}

// A taint is the set of sources a value depends on.
type taint []depElem

func (t taint) has(e depElem) bool {
	for _, x := range t {
		if x == e {
			return true
		}
	}
	return false
}

// union returns t and u together; neither is changed.
func (t taint) union(u taint) taint {
	if len(u) == 0 {
		return t
	}
	if len(t) == 0 {
		return u
	}
	var out taint
	for _, e := range u {
		if !t.has(e) {
			if out == nil {
				out = append(taint(nil), t...)
			}
			out = append(out, e)
		}
	}
	if out == nil {
		return t
	}
	return out
}

// ctl marks every source as reached through a condition.
func (t taint) ctl() taint {
	if len(t) == 0 {
		return t
	}
	out := make(taint, 0, len(t))
	for _, e := range t {
		e.ctrl = true
		if !out.has(e) {
			out = append(out, e)
		}
	}
	return out
}

// through marks the sources as having passed through the unseen call name.
func (t taint) through(name string) taint {
	if len(t) == 0 {
		return t
	}
	out := make(taint, 0, len(t))
	for _, e := range t {
		if e.opaque == "" {
			e.opaque = name
		}
		if !out.has(e) {
			out = append(out, e)
		}
	}
	return out
}

// A cpuDepSite is a store whose value or condition depends on loads of the
// same variable.
type cpuDepSite struct {
	v      *types2.Var
	loads  []int  // the sources, sorted
	data   bool   // some dependence is by data
	opaque string // some dependence passes through this call, which cannot be seen
	callee string // set when the site is a call of a function that stores v
}

func (s *cpuDepSite) add(loads []int, data bool, opaque string) {
	for _, l := range loads {
		found := false
		for _, m := range s.loads {
			found = found || m == l
		}
		if !found {
			s.loads = append(s.loads, l)
		}
	}
	sort.Ints(s.loads)
	s.data = s.data || data
	if s.opaque == "" {
		s.opaque = opaque
	}
}

// cpuFnInfo is what the dependence analysis knows about one declared function.
type cpuFnInfo struct {
	calls     []*function // the functions it may call (an interface call: every method of the name)
	opaque    bool        // it calls something whose body cannot be seen
	loads     map[*types2.Var]bool
	stores    map[*types2.Var]bool // per-CPU variables it may store, with its callees
	dirStore  bool                 // it stores a per-CPU variable itself
	pflow     map[*types2.Var]bits // parameters whose value reaches a store of the variable
	neverDone bool                 // inferred: it never returns
	neverSeen bool
	neverBusy bool
}

// scanDeps runs the dependence analysis over the whole program.
func (a *analyzer) scanDeps() {
	a.cpuLoadAt = map[token.Pos]int{}
	a.cpuLoadCall = map[loadKey]int{}
	a.cpuDeps = map[token.Pos]*cpuDepSite{}
	a.cpuDepCalls = map[*ast.CallExpr]*cpuDepSite{}
	a.cpuFn = map[*function]*cpuFnInfo{}
	var fns []*function
	for _, u := range a.units {
		for _, f := range u.fns {
			if f.decl != nil && f.body != nil && !a.inRuntimePkg(f) {
				fns = append(fns, f)
			}
		}
	}
	for _, f := range fns {
		a.cpuFn[f] = a.scanFn(f)
	}
	// Propagate loads and stores up the calls.
	for changed := true; changed; {
		changed = false
		for _, f := range fns {
			fi := a.cpuFn[f]
			for _, g := range fi.calls {
				gi := a.cpuFn[g]
				if gi == nil {
					continue
				}
				for v := range gi.loads {
					if !fi.loads[v] {
						fi.loads[v] = true
						changed = true
					}
				}
				for v := range gi.stores {
					if !fi.stores[v] {
						fi.stores[v] = true
						changed = true
					}
				}
			}
		}
	}
	// A function needs the flow analysis when it stores a per-CPU variable
	// or calls a function that does.
	var relevant []*function
	for _, f := range fns {
		fi := a.cpuFn[f]
		if fi.dirStore || len(fi.stores) > 0 {
			relevant = append(relevant, f)
		}
	}
	for changed := true; changed; {
		changed = false
		for _, f := range relevant {
			d := &depWalk{a: a, f: f, fi: a.cpuFn[f]}
			d.run()
			changed = changed || d.changed
		}
	}
	for _, s := range a.cpuDeps {
		for _, l := range s.loads {
			a.cpuLoads[l].feeds = true
		}
	}
	for _, s := range a.cpuDepCalls {
		for _, l := range s.loads {
			a.cpuLoads[l].feeds = true
		}
	}
}

func (a *analyzer) inRuntimePkg(f *function) bool {
	if f.pkg == nil {
		return false
	}
	return f.pkg.Path == "pluto/rt" || len(f.pkg.Path) > 8 && f.pkg.Path[:8] == "pluto/rt" && f.pkg.Path[8] == '/'
}

// A loadKey names the load a call makes of a variable.
type loadKey struct {
	call *ast.CallExpr
	v    *types2.Var
}

// loadAt returns the number of the load of v by name at id.
func (a *analyzer) loadAt(id *ast.Ident, v *types2.Var) int {
	if n, ok := a.cpuLoadAt[id.Pos()]; ok {
		return n
	}
	n := len(a.cpuLoads)
	a.cpuLoads = append(a.cpuLoads, &cpuLoad{v: v, pos: id.Pos()})
	a.cpuLoadAt[id.Pos()] = n
	return n
}

// callLoad returns the number of the load of v that call e makes.
func (a *analyzer) callLoad(e *ast.CallExpr, v *types2.Var, callee string) int {
	k := loadKey{e, v}
	if n, ok := a.cpuLoadCall[k]; ok {
		return n
	}
	n := len(a.cpuLoads)
	a.cpuLoads = append(a.cpuLoads, &cpuLoad{v: v, pos: e.Pos(), call: e, callee: callee})
	a.cpuLoadCall[k] = n
	a.cpuCallLoads[e] = append(a.cpuCallLoads[e], n)
	return n
}

// scanFn reads one body: the calls it makes and the per-CPU variables it
// loads and stores by name.
func (a *analyzer) scanFn(f *function) *cpuFnInfo {
	fi := &cpuFnInfo{loads: map[*types2.Var]bool{}, stores: map[*types2.Var]bool{}, pflow: map[*types2.Var]bits{}}
	stored := map[*ast.Ident]bool{} // the identifiers that are the target of a plain store
	ast.Inspect(f.body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if s.Tok == token.ASSIGN || s.Tok == token.DEFINE {
				for _, l := range s.Lhs {
					if id, ok := ast.Unparen(l).(*ast.Ident); ok {
						stored[id] = true
					}
				}
			}
		case *ast.OrStmt:
			if s.Tok == token.ASSIGN || s.Tok == token.DEFINE {
				for _, l := range s.Lhs {
					if id, ok := ast.Unparen(l).(*ast.Ident); ok {
						stored[id] = true
					}
				}
			}
		case *ast.RangeStmt:
			if s.Tok == token.ASSIGN {
				for _, l := range []ast.Expr{s.Key, s.Value} {
					if id, ok := ast.Unparen(l).(*ast.Ident); ok && l != nil {
						stored[id] = true
					}
				}
			}
		}
		return true
	})
	seen := map[*function]bool{}
	ast.Inspect(f.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if v, ok := a.uses[n].(*types2.Var); ok && a.cpuVars[v] {
				if stored[n] {
					fi.stores[v] = true
					fi.dirStore = true
				} else {
					fi.loads[v] = true
				}
			}
		case *ast.AssignStmt:
			if n.Tok != token.ASSIGN && n.Tok != token.DEFINE {
				if id, ok := ast.Unparen(n.Lhs[0]).(*ast.Ident); ok {
					if v, ok := a.uses[id].(*types2.Var); ok && a.cpuVars[v] {
						fi.stores[v] = true
						fi.dirStore = true
					}
				}
			}
		case *ast.IncDecStmt:
			if id, ok := ast.Unparen(n.X).(*ast.Ident); ok {
				if v, ok := a.uses[id].(*types2.Var); ok && a.cpuVars[v] {
					fi.stores[v] = true
					fi.dirStore = true
				}
			}
		case *ast.CallExpr:
			cands, opaque, _ := a.callees(n)
			fi.opaque = fi.opaque || opaque
			for _, g := range cands {
				if !seen[g] {
					seen[g] = true
					fi.calls = append(fi.calls, g)
				}
			}
		}
		return true
	})
	return fi
}

// callees returns the functions a call may reach whose bodies are read, and
// whether the call also reaches something that cannot be seen: a func value,
// or a function without a body. name describes the callee for messages.
// The predeclared functions and conversions have no callee.
func (a *analyzer) callees(e *ast.CallExpr) (cands []*function, opaque bool, name string) {
	if tv, ok := a.types[e.Fun]; ok && tv.IsType() {
		return nil, false, ""
	}
	fun := ast.Unparen(stripIndex(e.Fun))
	var fn *types2.Func
	switch f := fun.(type) {
	case *ast.FuncLit:
		return nil, false, ""
	case *ast.Ident:
		switch o := a.uses[f].(type) {
		case *types2.Builtin:
			return nil, false, ""
		case *types2.Func:
			fn = o
		}
		name = f.Name
	case *ast.SelectorExpr:
		name = exprText(f)
		if sel := a.selections[f]; sel != nil {
			o, ok := sel.Obj().(*types2.Func)
			if !ok {
				return nil, true, name // a func-valued field
			}
			fn = o
			if types2.IsInterface(sel.Recv()) {
				for _, g := range a.byName[o.Name()] {
					if g.body == nil {
						opaque = true
						continue
					}
					cands = append(cands, g)
				}
				return cands, opaque || len(cands) == 0, name
			}
		} else if o, ok := a.uses[f.Sel].(*types2.Func); ok {
			fn = o
		}
	}
	if fn == nil {
		return nil, true, name
	}
	g := a.funcs[fn.Origin()]
	if g == nil || g.body == nil {
		return nil, true, name
	}
	return []*function{g}, false, name
}

// ----------------------------------------------------------------------------
// The flow analysis of one body

type denv map[*types2.Var]taint

func (e denv) clone() denv {
	out := make(denv, len(e))
	for k, v := range e {
		out[k] = v
	}
	return out
}

// merge adds the taints of o to e.
func (e denv) merge(o denv) {
	for k, v := range o {
		e[k] = e[k].union(v)
	}
}

// A flow is the state at a point: what each local variable depends on, and
// whether the point can be reached.
type flow struct {
	env  denv
	dead bool
}

func (f *flow) clone() *flow { return &flow{env: f.env.clone(), dead: f.dead} }

func joinFlows(a, b *flow) *flow {
	switch {
	case a.dead:
		return b
	case b.dead:
		return a
	}
	out := a.clone()
	out.env.merge(b.env)
	return out
}

type depWalk struct {
	a  *analyzer
	f  *function
	fi *cpuFnInfo

	cur     *flow
	heap    taint // what has been stored through pointers, slices, maps, channels and into package variables
	pc      taint // the conditions that control the statement being read
	exitPC  taint // the conditions of branches that may leave the function or loop: they control all that follows
	weak    int   // in a function literal: an assignment adds, it does not replace
	addr    map[*types2.Var]bool
	brk     []denv // the states at break and continue of the enclosing loops and switches
	gotoEnv denv
	changed bool
}

func (d *depWalk) run() {
	d.addr = d.addrTaken()
	passes := 1
	if len(gotoTargets(d.f.body)) > 0 {
		passes = 3
	}
	for i := 0; i < passes; i++ {
		d.cur = &flow{env: denv{}}
		d.pc, d.exitPC = nil, nil
		for k, p := range d.f.params {
			if p.Name() == "" || p.Name() == "_" {
				continue
			}
			d.cur.env[p] = taint{{src: -(k + 1)}}
		}
		d.block(d.f.body)
	}
}

// addrTaken finds the local variables whose address is taken, which others
// may then change through a pointer.
func (d *depWalk) addrTaken() map[*types2.Var]bool {
	out := map[*types2.Var]bool{}
	root := func(e ast.Expr) {
		for {
			switch x := ast.Unparen(e).(type) {
			case *ast.Ident:
				if v, ok := d.a.objOf(x).(*types2.Var); ok {
					out[v] = true
				}
				return
			case *ast.SelectorExpr:
				e = x.X
			case *ast.IndexExpr:
				e = x.X
			case *ast.SliceExpr:
				e = x.X
			default:
				return
			}
		}
	}
	ast.Inspect(d.f.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				root(n.X)
			}
		case *ast.SliceExpr:
			root(n.X)
		case *ast.CallExpr:
			if se, ok := ast.Unparen(n.Fun).(*ast.SelectorExpr); ok {
				if sel := d.a.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
					if fn, ok := sel.Obj().(*types2.Func); ok {
						if r := fn.Type().(*types2.Signature).Recv(); r != nil && pointerElem(r.Type()) != nil && pointerElem(d.a.typeOf(se.X)) == nil {
							root(se.X)
						}
					}
				}
			}
		}
		return true
	})
	return out
}

// effPC is every condition that controls the statement being read.
func (d *depWalk) effPC() taint { return d.pc.union(d.exitPC) }

func (d *depWalk) block(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	d.stmts(b.List)
}

func (d *depWalk) stmts(list []ast.Stmt) {
	for _, s := range list {
		if d.cur.dead {
			// Only a label that a goto may reach revives the flow.
			if _, ok := s.(*ast.LabeledStmt); !ok {
				continue
			}
		}
		d.stmt(s)
	}
}

func (d *depWalk) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case nil, *ast.EmptyStmt, *ast.BadStmt:
	case *ast.LabeledStmt:
		if d.gotoEnv != nil {
			if d.cur.dead {
				d.cur = &flow{env: denv{}}
			}
			d.cur.env.merge(d.gotoEnv)
		}
		d.stmt(s.Stmt)
	case *ast.BranchStmt:
		switch s.Tok {
		case token.BREAK, token.CONTINUE:
			for _, b := range d.brk {
				b.merge(d.cur.env)
			}
		case token.GOTO:
			if d.gotoEnv == nil {
				d.gotoEnv = denv{}
			}
			d.gotoEnv.merge(d.cur.env)
		}
		if s.Tok != token.FALLTHROUGH {
			d.cur.dead = true
		}
	case *ast.ExprStmt:
		d.ex(s.X)
	case *ast.DeclStmt:
		d.declStmt(s)
	case *ast.AssignStmt:
		d.assign(s)
	case *ast.OrStmt:
		t := d.callTaint(s.Call)
		for _, l := range s.Lhs {
			d.assignTo(l, t, s.Pos(), s.Tok == token.DEFINE)
		}
		d.guarded(t, func() {
			d.stmt(s.Clause)
		}, func() bool { return exitsStmt(d, s.Clause) })
	case *ast.IncDecStmt:
		d.ex(s.X)
	case *ast.BlockStmt:
		d.block(s)
	case *ast.IfStmt:
		d.stmt(s.Init)
		t := d.ex(s.Cond)
		start := d.cur
		d.cur = start.clone()
		d.branch(t, func() { d.block(s.Body) })
		thenFlow := d.cur
		d.cur = start.clone()
		if s.Else != nil {
			d.branch(t, func() { d.stmt(s.Else) })
		}
		d.cur = joinFlows(thenFlow, d.cur)
		if exitsStmt(d, s.Body) || s.Else != nil && exitsStmt(d, s.Else) {
			d.exitPC = d.exitPC.union(t.ctl())
		}
	case *ast.ForStmt:
		d.stmt(s.Init)
		d.loop(func() taint {
			if s.Cond != nil {
				return d.ex(s.Cond)
			}
			return nil
		}, func() { d.block(s.Body) }, func() { d.stmt(s.Post) })
	case *ast.RangeStmt:
		xt := d.ex(s.X)
		d.loop(func() taint {
			t := xt
			for _, e := range []ast.Expr{s.Key, s.Value} {
				if e == nil {
					continue
				}
				d.assignTo(e, t, s.Pos(), s.Tok == token.DEFINE)
			}
			return t
		}, func() { d.block(s.Body) }, nil)
	case *ast.SwitchStmt:
		d.stmt(s.Init)
		var t taint
		if s.Tag != nil {
			t = d.ex(s.Tag)
		}
		for _, c := range s.Body.List {
			for _, e := range c.(*ast.CaseClause).List {
				t = t.union(d.ex(e))
			}
		}
		d.clauses(t, s.Body.List, func(c ast.Stmt) []ast.Stmt { return c.(*ast.CaseClause).Body })
	case *ast.TypeSwitchStmt:
		d.stmt(s.Init)
		var t taint
		switch as := s.Assign.(type) {
		case *ast.AssignStmt:
			t = d.ex(as.Rhs[0])
		case *ast.ExprStmt:
			t = d.ex(as.X)
		}
		for _, c := range s.Body.List {
			if v, ok := d.a.implicits[c].(*types2.Var); ok {
				d.cur.env[v] = t
			}
		}
		d.clauses(t, s.Body.List, func(c ast.Stmt) []ast.Stmt { return c.(*ast.CaseClause).Body })
	case *ast.SelectStmt:
		d.clauses(nil, s.Body.List, func(c ast.Stmt) []ast.Stmt {
			cc := c.(*ast.CommClause)
			if cc.Comm != nil {
				d.stmt(cc.Comm)
			}
			return cc.Body
		})
	case *ast.ReturnStmt:
		for _, r := range s.Results {
			d.ex(r)
		}
		d.cur.dead = true
	case *ast.DeferStmt:
		d.ex(s.Call)
	case *ast.TaskStmt:
		d.ex(s.Call)
	case *ast.SendStmt:
		d.ex(s.Chan)
		d.heap = d.heap.union(d.ex(s.Value)).union(d.effPC())
	}
}

// branch runs a branch controlled by the condition taint t.
func (d *depWalk) branch(t taint, body func()) {
	saved := d.pc
	d.pc = d.pc.union(t.ctl())
	body()
	d.pc = saved
}

// guarded runs body under the condition t; when it may leave the function or
// the loop, what follows is controlled by t too.
func (d *depWalk) guarded(t taint, body func(), exits func() bool) {
	start := d.cur
	d.cur = start.clone()
	d.branch(t, body)
	d.cur = joinFlows(start, d.cur)
	if exits() {
		d.exitPC = d.exitPC.union(t.ctl())
	}
}

// loop walks a loop twice, so that what the body assigns reaches its top.
func (d *depWalk) loop(head func() taint, body, post func()) {
	entry := d.cur
	acc := denv{}
	d.brk = append(d.brk, acc)
	var t taint
	for i := 0; i < 2; i++ {
		d.cur = entry.clone()
		t = head()
		d.branch(t, func() {
			body()
			if post != nil {
				d.cur.dead = false
				post()
			}
		})
		if i == 0 {
			next := entry.clone()
			next.dead = false
			next.env.merge(d.cur.env)
			next.env.merge(acc)
			entry = next
		}
	}
	d.brk = d.brk[:len(d.brk)-1]
	out := entry.clone()
	out.env.merge(d.cur.env)
	out.env.merge(acc)
	out.dead = false
	d.cur = out
}

// clauses walks the clauses of a switch or select under the taint t of what
// selects them.
func (d *depWalk) clauses(t taint, list []ast.Stmt, body func(ast.Stmt) []ast.Stmt) {
	start := d.cur
	acc := denv{}
	d.brk = append(d.brk, acc)
	out := &flow{dead: true}
	exits := false
	for _, c := range list {
		d.cur = start.clone()
		var stmts []ast.Stmt
		d.branch(t, func() {
			stmts = body(c)
			d.stmts(stmts)
		})
		for _, s := range stmts {
			exits = exits || exitsStmt(d, s)
		}
		out = joinFlows(out, d.cur)
	}
	d.brk = d.brk[:len(d.brk)-1]
	out = joinFlows(out, start)
	out.env.merge(acc)
	d.cur = out
	if exits {
		d.exitPC = d.exitPC.union(t.ctl())
	}
}

func (d *depWalk) declStmt(s *ast.DeclStmt) {
	gd, ok := s.Decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.VAR {
		return
	}
	for _, spec := range gd.Specs {
		vs := spec.(*ast.ValueSpec)
		if len(vs.Values) == 0 {
			for _, n := range vs.Names {
				d.assignTo(n, nil, n.Pos(), true)
			}
			continue
		}
		if len(vs.Values) == len(vs.Names) {
			var ts []taint
			for _, e := range vs.Values {
				ts = append(ts, d.ex(e))
			}
			for i, n := range vs.Names {
				d.assignTo(n, ts[i], n.Pos(), true)
			}
			continue
		}
		t := d.ex(vs.Values[0])
		for _, n := range vs.Names {
			d.assignTo(n, t, n.Pos(), true)
		}
	}
}

func (d *depWalk) assign(s *ast.AssignStmt) {
	if s.Tok != token.ASSIGN && s.Tok != token.DEFINE {
		// x op= y: a store that reads x; a per-CPU variable here needs a
		// region on its own account.
		t := d.ex(s.Rhs[0]).union(d.ex(s.Lhs[0]))
		d.assignTo(s.Lhs[0], t, s.Pos(), false)
		return
	}
	define := s.Tok == token.DEFINE
	if len(s.Lhs) == len(s.Rhs) {
		ts := make([]taint, len(s.Rhs))
		for i, r := range s.Rhs {
			ts[i] = d.ex(r)
		}
		for i, l := range s.Lhs {
			d.assignTo(l, ts[i], s.Pos(), define)
		}
		return
	}
	t := d.ex(s.Rhs[0])
	for _, l := range s.Lhs {
		d.assignTo(l, t, s.Pos(), define)
	}
}

// assignTo stores a value with taint t in lhs.
func (d *depWalk) assignTo(lhs ast.Expr, t taint, pos token.Pos, define bool) {
	lhs = ast.Unparen(lhs)
	t = t.union(d.effPC())
	if id, ok := lhs.(*ast.Ident); ok {
		if id.Name == "_" {
			return
		}
		obj := d.a.objOf(id)
		v, ok := obj.(*types2.Var)
		if !ok {
			return
		}
		switch {
		case d.a.cpuVars[v]:
			d.storeSite(v, t, id.Pos(), nil, "")
		case isGlobal(v):
			d.heap = d.heap.union(t)
		case d.weak > 0 || d.addr[v]:
			d.cur.env[v] = d.cur.env[v].union(t)
		default:
			d.cur.env[v] = t
		}
		return
	}
	// A part of a local variable, or other memory.
	e := lhs
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
			continue
		case *ast.SelectorExpr:
			if sel := d.a.selections[x]; sel != nil && sel.Kind() == types2.FieldVal && pointerElem(d.a.typeOf(x.X)) == nil {
				e = x.X
				continue
			}
		case *ast.IndexExpr:
			if _, ok := underlying(d.a.typeOf(x.X)).(*types2.Array); ok {
				e = x.X
				continue
			}
		}
		break
	}
	if id, ok := e.(*ast.Ident); ok {
		if v, ok := d.a.objOf(id).(*types2.Var); ok && !isGlobal(v) {
			d.cur.env[v] = d.cur.env[v].union(t)
			d.ex(lhs)
			return
		}
	}
	d.heap = d.heap.union(t)
	d.ex(lhs)
}

// storeSite records a store of per-CPU variable v of a value with taint t,
// at the identifier id, or at the call e of a function that stores it.
func (d *depWalk) storeSite(v *types2.Var, t taint, pos token.Pos, e *ast.CallExpr, callee string) {
	var loads []int
	data := false
	opaque := ""
	for _, el := range t {
		if el.src < 0 {
			// The caller's value decides: remember which parameter.
			if k := -el.src - 1; !d.fi.pflow[v].has(k) {
				d.fi.pflow[v] = d.fi.pflow[v].with(k)
				d.changed = true
			}
			continue
		}
		if d.a.cpuLoads[el.src].v != v {
			continue
		}
		loads = append(loads, el.src)
		data = data || !el.ctrl
		if el.opaque != "" {
			opaque = el.opaque
		}
	}
	if len(loads) == 0 {
		return
	}
	var site *cpuDepSite
	if e != nil {
		site = d.a.cpuDepCalls[e]
		if site == nil {
			site = &cpuDepSite{v: v, callee: callee}
			d.a.cpuDepCalls[e] = site
		}
	} else {
		site = d.a.cpuDeps[pos]
		if site == nil {
			site = &cpuDepSite{v: v}
			d.a.cpuDeps[pos] = site
		}
	}
	n := len(site.loads)
	site.add(loads, data, opaque)
	if len(site.loads) != n {
		d.changed = true
	}
}

// ex walks an expression and returns what its value depends on.
func (d *depWalk) ex(e ast.Expr) taint {
	switch e := e.(type) {
	case nil:
		return nil
	case *ast.ParenExpr:
		return d.ex(e.X)
	case *ast.Ident:
		return d.ident(e)
	case *ast.BasicLit:
		return nil
	case *ast.FuncLit:
		d.lit(e)
		return nil
	case *ast.CompositeLit:
		var t taint
		for _, el := range e.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				t = t.union(d.ex(kv.Value))
				if _, isIdent := kv.Key.(*ast.Ident); !isIdent {
					t = t.union(d.ex(kv.Key))
				}
				continue
			}
			t = t.union(d.ex(el))
		}
		return t
	case *ast.SelectorExpr:
		sel := d.a.selections[e]
		if sel == nil {
			return d.ident(e.Sel) // a qualified identifier
		}
		t := d.ex(e.X)
		if sel.Kind() == types2.FieldVal && pointerElem(d.a.typeOf(e.X)) != nil {
			t = t.union(d.heap)
		}
		return t
	case *ast.IndexExpr:
		t := d.ex(e.X).union(d.ex(e.Index))
		if _, ok := underlying(d.a.typeOf(e.X)).(*types2.Array); !ok {
			t = t.union(d.heap)
		}
		return t
	case *ast.IndexListExpr:
		return d.ex(e.X)
	case *ast.SliceExpr:
		return d.ex(e.X).union(d.ex(e.Low)).union(d.ex(e.High)).union(d.ex(e.Max)).union(d.heap)
	case *ast.TypeAssertExpr:
		return d.ex(e.X)
	case *ast.StarExpr:
		return d.ex(e.X).union(d.heap)
	case *ast.UnaryExpr:
		switch e.Op {
		case token.AND:
			d.ex(e.X)
			return nil
		case token.ARROW:
			return d.ex(e.X).union(d.heap)
		}
		return d.ex(e.X)
	case *ast.BinaryExpr:
		return d.ex(e.X).union(d.ex(e.Y))
	case *ast.KeyValueExpr:
		return d.ex(e.Value)
	case *ast.CallExpr:
		return d.callTaint(e)
	}
	return nil
}

func (d *depWalk) ident(id *ast.Ident) taint {
	v, ok := d.a.objOf(id).(*types2.Var)
	if !ok {
		return nil
	}
	switch {
	case d.a.cpuVars[v]:
		return taint{{src: d.a.loadAt(id, v)}}
	case isGlobal(v):
		return d.heap
	case d.addr[v]:
		return d.cur.env[v].union(d.heap)
	}
	return d.cur.env[v]
}

// lit walks a function literal where it appears. It may run later, any
// number of times, so what it assigns is added to the variables, and what it
// returns or exits from ends only itself.
func (d *depWalk) lit(l *ast.FuncLit) {
	saved, savedPC, savedExit := d.cur, d.pc, d.exitPC
	savedBrk := d.brk
	d.cur = saved.clone()
	d.brk = nil
	d.weak++
	d.block(l.Body)
	d.weak--
	inner := d.cur.env
	d.cur, d.pc, d.exitPC, d.brk = saved, savedPC, savedExit, savedBrk
	d.cur.env.merge(inner)
}

// callTaint walks a call and returns what its results depend on.
func (d *depWalk) callTaint(e *ast.CallExpr) taint {
	if tv, ok := d.a.types[e.Fun]; ok && tv.IsType() {
		var t taint
		for _, a := range e.Args {
			t = t.union(d.ex(a))
		}
		return t
	}
	fun := ast.Unparen(stripIndex(e.Fun))
	var recv taint
	hasRecv := false
	if se, ok := fun.(*ast.SelectorExpr); ok {
		if sel := d.a.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
			recv = d.ex(se.X)
			hasRecv = true
		} else if sel != nil {
			recv = d.ex(se.X) // a func-valued field
		}
	} else if _, isIdent := fun.(*ast.Ident); !isIdent {
		recv = d.ex(fun) // a literal, or another expression
	}
	var args []taint
	if hasRecv {
		args = append(args, recv)
	}
	all := recv
	for _, a := range e.Args {
		t := d.ex(a)
		args = append(args, t)
		all = all.union(t)
	}
	cands, opaque, name := d.a.callees(e)
	if id, ok := fun.(*ast.Ident); ok {
		if b, ok := d.a.uses[id].(*types2.Builtin); ok {
			switch b.Name() {
			case "append", "copy":
				d.heap = d.heap.union(all)
			}
			return all
		}
	}
	// A call of a function that stores a per-CPU variable from one of its
	// parameters, or under a condition.
	pc := d.effPC()
	for _, g := range cands {
		gi := d.a.cpuFn[g]
		if gi == nil {
			continue
		}
		for v := range gi.stores {
			var t taint
			for k, a := range args {
				i := k
				if i >= len(g.params) {
					i = len(g.params) - 1
				}
				if i >= 0 && gi.pflow[v].has(i) {
					t = t.union(a)
				}
			}
			d.storeSite(v, t.union(pc), e.Pos(), e, g.name)
		}
	}
	base := all
	if opaque {
		base = all.through(name)
	}
	res := base.union(d.heap)
	if len(cands) == 0 {
		// A call of a func value: it may reach any function of its type that
		// the program uses as a value.
		if sig, ok := underlying(d.a.typeOf(e.Fun)).(*types2.Signature); ok {
			for _, v := range d.a.valueLoads(sig) {
				res = res.union(taint{{src: d.a.callLoad(e, v, name)}})
			}
		}
	}
	for _, g := range cands {
		if gi := d.a.cpuFn[g]; gi != nil {
			vs := make([]*types2.Var, 0, len(gi.loads))
			for v := range gi.loads {
				vs = append(vs, v)
			}
			sort.Slice(vs, func(i, j int) bool { return vs[i].Pos() < vs[j].Pos() })
			for _, v := range vs {
				res = res.union(taint{{src: d.a.callLoad(e, v, g.name)}})
			}
		}
	}
	return res
}

// exitsStmt reports whether a statement may leave the function or the loop it
// is in, or never finish: a return, a break, a continue, a goto, a panic, a
// loop without a condition, or a call of a function that never returns.
func exitsStmt(d *depWalk, s ast.Stmt) bool {
	if s == nil {
		return false
	}
	found := false
	ast.Inspect(s, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			found = true
		case *ast.BranchStmt:
			found = n.Tok != token.FALLTHROUGH
		case *ast.OrStmt:
			if _, ok := n.Clause.(*ast.ReturnStmt); ok {
				found = true
			}
		case *ast.ForStmt:
			found = n.Cond == nil
		case *ast.SelectStmt:
			found = len(n.Body.List) == 0
		case *ast.CallExpr:
			if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok {
				if b, ok := d.a.uses[id].(*types2.Builtin); ok && b.Name() == "panic" {
					found = true
					break
				}
			}
			cands, _, _ := d.a.callees(n)
			for _, g := range cands {
				found = found || d.a.neverReturns(g)
			}
		}
		return !found
	})
	return found
}

// neverReturns reports whether a function appears never to return: no return
// statement, and a body that ends in a panic, a loop without a condition or a
// call of such a function. A recursive chain counts as one that never returns.
func (a *analyzer) neverReturns(g *function) bool {
	gi := a.cpuFn[g]
	if gi == nil || g.body == nil {
		return false
	}
	if gi.neverSeen {
		return gi.neverDone
	}
	if gi.neverBusy {
		return true
	}
	gi.neverBusy = true
	res := false
	if n := len(g.body.List); n > 0 {
		hasRet := false
		ast.Inspect(g.body, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ReturnStmt:
				hasRet = true
			}
			return !hasRet
		})
		if !hasRet {
			d := &depWalk{a: a}
			switch last := g.body.List[n-1].(type) {
			case *ast.ForStmt:
				res = last.Cond == nil && !hasBreak(last.Body)
			case *ast.SelectStmt:
				res = len(last.Body.List) == 0
			case *ast.ExprStmt:
				if c, ok := last.X.(*ast.CallExpr); ok {
					res = exitsStmt(d, &ast.ExprStmt{X: c})
				}
			}
		}
	}
	gi.neverBusy = false
	gi.neverSeen, gi.neverDone = true, res
	return res
}

// hasBreak reports whether a loop body has a break that may leave the loop.
func hasBreak(body ast.Node) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.BranchStmt:
			found = found || n.Tok == token.BREAK || n.Tok == token.GOTO
		}
		return !found
	})
	return found
}

// valueLoads returns the per-CPU variables that a function used as a value,
// of type sig, may load.
func (a *analyzer) valueLoads(sig *types2.Signature) []*types2.Var {
	set := map[*types2.Var]bool{}
	for _, f := range a.values {
		if f.sig == nil || f.sig.Recv() != nil || !types2.Identical(f.sig, sig) {
			continue
		}
		for v := range a.fnLoads(f) {
			set[v] = true
		}
	}
	out := make([]*types2.Var, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pos() < out[j].Pos() })
	return out
}

// fnLoads returns the per-CPU variables a function or literal may load.
func (a *analyzer) fnLoads(f *function) map[*types2.Var]bool {
	if fi := a.cpuFn[f]; fi != nil {
		return fi.loads
	}
	if f.lit == nil {
		return nil
	}
	if a.litLoads == nil {
		a.litLoads = map[*function]map[*types2.Var]bool{}
	}
	if m, ok := a.litLoads[f]; ok {
		return m
	}
	m := map[*types2.Var]bool{}
	a.litLoads[f] = m
	ast.Inspect(f.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if v, ok := a.uses[n].(*types2.Var); ok && a.cpuVars[v] {
				m[v] = true
			}
		case *ast.CallExpr:
			cands, _, _ := a.callees(n)
			for _, g := range cands {
				for v := range a.fnLoads(g) {
					m[v] = true
				}
			}
		}
		return true
	})
	return m
}
