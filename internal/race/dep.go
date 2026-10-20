package race

import (
	"sort"

	"github.com/matjam/plutonium/internal/directive"
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
// struct copies, arithmetic, memory and calls, and the conditions that control
// a statement. The walker (walk.go, region.go) then checks that the load and
// the store are inside one region.
//
// The analysis is conservative by default: a store of a per-CPU variable V
// that follows a load of V is a dependent store unless the stored value and
// every condition that controls it are provably independent of everything
// loaded from V. Where the analysis cannot see, it assumes dependence:
//   - the result of a call depends on every argument and on the receiver, and
//     on every per-CPU variable the callee, or a function it calls, loads;
//   - everything a call is given or returns is also written into one shared
//     pool (the heap), read back by every load of memory, of a package
//     variable, of a variable whose address is taken and of a captured
//     variable: a callee, seen or not, may have stored it through a pointer;
//   - a call of a function without a body, of a func value, of a method value
//     or of an interface method with no body to read, stores through every
//     pointer it is given, and from every value it is given;
//   - a store through a pointer taken with &V, or from a parameter that a
//     caller fills with &V, is a store of V;
//   - a per-CPU variable that is stored carries what was stored to every later
//     load of it, like a package variable;
//   - the statements that follow a branch that may leave the function, or the
//     loop, and those that follow a loop, are controlled by its condition;
//   - a function literal that is not simply called where the program can see
//     it (assigned to a variable that is only called, or called where it is
//     written) may run at any time, so it sees whatever its captured
//     variables hold at any point of the function;
//   - a task statement starts code that runs later, on any processor: no
//     region of the starter covers it.

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
	src    int         // index into analyzer.cpuLoads, or -(i+1) for parameter i
	ctrl   bool        // reached through a controlling condition
	opaque string      // the call, whose body cannot be seen, the dependence passed through
	ptr    *types2.Var // a pointer to this per-CPU variable; src is then the load made where its address was taken
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

func (t taint) hasPtr() bool {
	for _, e := range t {
		if e.ptr != nil {
			return true
		}
	}
	return false
}

// noPtr returns t without the pointers to per-CPU variables.
func (t taint) noPtr() taint {
	if !t.hasPtr() {
		return t
	}
	out := make(taint, 0, len(t))
	for _, e := range t {
		if e.ptr == nil {
			out = append(out, e)
		}
	}
	return out
}

// reads returns the loads that reading through the pointers of t makes.
func (t taint) reads() taint {
	if !t.hasPtr() {
		return nil
	}
	var out taint
	for _, e := range t {
		if e.ptr != nil {
			if l := (depElem{src: e.src}); !out.has(l) {
				out = append(out, l)
			}
		}
	}
	return out
}

// deref returns what a value read through the pointers of t depends on.
func (t taint) deref() taint { return t.noPtr().union(t.reads()) }

// ctl marks every source as reached through a condition. A pointer is not
// part of the condition.
func (t taint) ctl() taint {
	if len(t) == 0 {
		return t
	}
	out := make(taint, 0, len(t))
	for _, e := range t {
		if e.ptr != nil {
			continue
		}
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
		if e.opaque == "" && e.ptr == nil {
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
	task   bool   // the store is made by a task started here, which no region of the starter covers
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

// A depKey names the store of a per-CPU variable at a position.
type depKey struct {
	pos token.Pos
	v   *types2.Var
}

// A pstoreInfo says what a function stores through the pointer that is its
// parameter: from which of its parameters, and from what it loads itself.
type pstoreInfo struct {
	from bits
	own  bool
}

// cpuFnInfo is what the dependence analysis knows about one declared function
// or literal.
type cpuFnInfo struct {
	calls    []*function // the functions it may call (an interface call: every method of the name)
	fvSigs   []*types2.Signature
	opaque   bool // it calls something whose body cannot be seen
	loads    map[*types2.Var]bool
	stores   map[*types2.Var]bool // per-CPU variables it may store, with its callees
	addr     map[*types2.Var]bool // per-CPU variables whose address it takes, with its callees: it may hand out a pointer to them
	dirStore bool                 // it stores a per-CPU variable itself
	pflow    map[*types2.Var]bits // parameters whose value reaches a store of the variable
	pstore   map[int]*pstoreInfo  // parameters that point to memory it stores to

	// What the function has stored in memory, in each of its variables, and in
	// each per-CPU variable. The pools only grow, and are kept from one walk
	// of the body to the next, since a store later in the text may be read
	// earlier in the run (a loop, a deferred call, a literal).
	heap    taint
	ever    denv
	cpuHeap map[*types2.Var]taint
}

// scanDeps runs the dependence analysis over the whole program.
func (a *analyzer) scanDeps() {
	a.cpuLoadAt = map[token.Pos]int{}
	a.cpuLoadCall = map[loadKey]int{}
	a.cpuDeps = map[depKey]*cpuDepSite{}
	a.cpuDepCalls = map[*ast.CallExpr][]*cpuDepSite{}
	a.cpuFn = map[*function]*cpuFnInfo{}
	a.cpuNever = map[*function]int8{}
	a.cpuLeave = map[*function]bool{}
	a.computeLeave()
	var fns, all []*function
	for _, u := range a.units {
		for _, f := range u.fns {
			if f.body == nil || a.inRuntimePkg(f) {
				continue
			}
			all = append(all, f)
			if f.decl != nil {
				fns = append(fns, f)
			}
		}
	}
	for _, f := range all {
		a.cpuFn[f] = a.scanFn(f)
	}
	// Every function with a body gets the flow analysis: a store through a
	// pointer, a call through a func value or a literal that is called later
	// can reach a per-CPU variable from a function that never names one.
	for {
		a.propagateCPU(all)
		changed := false
		for _, f := range fns {
			d := new(depWalk)
			d.a, d.f, d.fi = a, f, a.cpuFn[f]
			d.run()
			changed = changed || d.changed
		}
		if !changed {
			break
		}
	}
	for _, s := range a.cpuDeps {
		for _, l := range s.loads {
			a.cpuLoads[l].feeds = true
		}
	}
	for _, ss := range a.cpuDepCalls {
		for _, s := range ss {
			for _, l := range s.loads {
				a.cpuLoads[l].feeds = true
			}
		}
	}
}

// propagateCPU adds what the callees of each function load, store and take
// the address of to the function, until nothing changes.
func (a *analyzer) propagateCPU(all []*function) {
	merge := func(dst, src map[*types2.Var]bool) bool {
		ch := false
		for v := range src {
			if !dst[v] {
				dst[v] = true
				ch = true
			}
		}
		return ch
	}
	for changed := true; changed; {
		changed = false
		for _, f := range all {
			fi := a.cpuFn[f]
			add := func(g *function) {
				if gi := a.cpuFn[g]; gi != nil {
					changed = merge(fi.loads, gi.loads) || changed
					changed = merge(fi.stores, gi.stores) || changed
					changed = merge(fi.addr, gi.addr) || changed
				}
			}
			for _, g := range fi.calls {
				add(g)
			}
			for _, sig := range fi.fvSigs {
				for _, g := range a.valueFns(sig) {
					add(g)
				}
			}
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

// storeRoot returns the identifier of the variable that an assignment target
// is the whole of or a part of (a field, an array element), or nil if the
// target is memory reached through a pointer, a slice or a map.
func (a *analyzer) storeRoot(e ast.Expr) *ast.Ident {
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			return x
		case *ast.SelectorExpr:
			sel := a.selections[x]
			switch {
			case sel == nil:
				return x.Sel // a qualified identifier
			case sel.Kind() == types2.FieldVal && pointerElem(a.typeOf(x.X)) == nil:
				e = x.X
			default:
				return nil
			}
		case *ast.IndexExpr:
			if _, ok := underlying(a.typeOf(x.X)).(*types2.Array); !ok {
				return nil
			}
			e = x.X
		default:
			return nil
		}
	}
}

// scanFn reads one body: the calls it makes and the per-CPU variables it
// loads and stores by name.
func (a *analyzer) scanFn(f *function) *cpuFnInfo {
	fi := &cpuFnInfo{
		loads: map[*types2.Var]bool{}, stores: map[*types2.Var]bool{}, addr: map[*types2.Var]bool{},
		pflow: map[*types2.Var]bits{}, pstore: map[int]*pstoreInfo{},
		ever: denv{}, cpuHeap: map[*types2.Var]taint{},
	}
	stored := map[*ast.Ident]bool{} // the identifiers that are the target of a plain store
	mark := func(l ast.Expr) {
		if l == nil {
			return
		}
		if id := a.storeRoot(l); id != nil {
			stored[id] = true
		}
	}
	ast.Inspect(f.body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if s.Tok == token.ASSIGN || s.Tok == token.DEFINE {
				for _, l := range s.Lhs {
					mark(l)
				}
			}
		case *ast.OrStmt:
			if s.Tok == token.ASSIGN || s.Tok == token.DEFINE {
				for _, l := range s.Lhs {
					mark(l)
				}
			}
		case *ast.RangeStmt:
			if s.Tok == token.ASSIGN {
				mark(s.Key)
				mark(s.Value)
			}
		}
		return true
	})
	seen := map[*function]bool{}
	storeOf := func(l ast.Expr) {
		if id := a.storeRoot(l); id != nil {
			if v, ok := a.uses[id].(*types2.Var); ok && a.cpuVars[v] {
				fi.stores[v] = true
				fi.dirStore = true
			}
		}
	}
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
				storeOf(n.Lhs[0])
			}
		case *ast.IncDecStmt:
			storeOf(n.X)
		case *ast.RangeStmt:
			// A range over a func calls it with the loop body.
			if sig, ok := underlying(a.typeOf(n.X)).(*types2.Signature); ok {
				fi.opaque = true
				dup := false
				for _, s := range fi.fvSigs {
					dup = dup || types2.Identical(s, sig)
				}
				if !dup {
					fi.fvSigs = append(fi.fvSigs, sig)
				}
			}
		case *ast.CallExpr:
			ci := a.callInfo(n)
			fi.opaque = fi.opaque || ci.opaque
			for _, g := range ci.cands {
				if !seen[g] {
					seen[g] = true
					fi.calls = append(fi.calls, g)
				}
			}
			if ci.fv && ci.sig != nil {
				dup := false
				for _, s := range fi.fvSigs {
					dup = dup || types2.Identical(s, ci.sig)
				}
				if !dup {
					fi.fvSigs = append(fi.fvSigs, ci.sig)
				}
			}
		}
		return true
	})
	return fi
}

// A callInfo says what a call may reach.
type callInfo struct {
	cands  []*function // the functions whose bodies are read
	opaque bool        // it also reaches something that cannot be seen
	fv     bool        // through a func value: the candidates are the functions used as values of the type
	static *function   // the declared function a static call names, with or without a body
	sig    *types2.Signature
	name   string // the callee as written
}

// callInfo returns what a call may reach. The predeclared functions and
// conversions reach nothing; a call of a literal reaches the literal, which
// its enclosing function contains.
func (a *analyzer) callInfo(e *ast.CallExpr) callInfo {
	var ci callInfo
	if tv, ok := a.types[e.Fun]; ok && tv.IsType() {
		return ci
	}
	fun := ast.Unparen(stripIndex(e.Fun))
	var fn *types2.Func
	switch f := fun.(type) {
	case *ast.FuncLit:
		return ci
	case *ast.Ident:
		switch o := a.uses[f].(type) {
		case *types2.Builtin:
			return ci
		case *types2.Func:
			fn = o
		}
		ci.name = f.Name
	case *ast.SelectorExpr:
		ci.name = exprText(f)
		if sel := a.selections[f]; sel != nil {
			o, ok := sel.Obj().(*types2.Func)
			if !ok {
				return a.funcValueCall(e, ci) // a func-valued field
			}
			fn = o
			if types2.IsInterface(sel.Recv()) {
				for _, g := range a.byName[o.Name()] {
					if g.body == nil {
						ci.opaque = true
						continue
					}
					ci.cands = append(ci.cands, g)
				}
				ci.opaque = ci.opaque || len(ci.cands) == 0
				return ci
			}
		} else if o, ok := a.uses[f.Sel].(*types2.Func); ok {
			fn = o
		}
	}
	if fn == nil {
		return a.funcValueCall(e, ci)
	}
	g := a.funcs[fn.Origin()]
	ci.static = g
	if g == nil || g.body == nil {
		ci.opaque = true
		return ci
	}
	ci.cands = []*function{g}
	return ci
}

func (a *analyzer) funcValueCall(e *ast.CallExpr, ci callInfo) callInfo {
	ci.opaque, ci.fv = true, true
	ci.sig, _ = underlying(a.typeOf(e.Fun)).(*types2.Signature)
	return ci
}

// valueFns returns the functions and literals used as values that a call
// through a func value of type sig may reach: a function of that type, a
// method taken as a method value (its type without the receiver), and a
// method taken as a method expression (the receiver as the first parameter).
// A generic function is taken to match when it has as many parameters and
// results.
func (a *analyzer) valueFns(sig *types2.Signature) []*function {
	if sig == nil {
		return nil
	}
	var out []*function
	for _, f := range a.values {
		if f.sig == nil || f.body == nil {
			continue
		}
		switch {
		case f.sig.TypeParams().Len() > 0:
			if f.sig.Params().Len() == sig.Params().Len() || f.sig.Recv() != nil && f.sig.Params().Len()+1 == sig.Params().Len() {
				out = append(out, f)
			}
		case types2.Identical(f.sig, sig):
			out = append(out, f)
		case f.sig.Recv() != nil && types2.Identical(recvAsParam(f.sig), sig):
			out = append(out, f)
		}
	}
	return out
}

// recvAsParam returns the type of a method expression: the signature of a
// method with the receiver as its first parameter.
func recvAsParam(sig *types2.Signature) *types2.Signature {
	ps := []*types2.Var{types2.NewParam(token.NoPos, nil, "", sig.Recv().Type())}
	for i := 0; i < sig.Params().Len(); i++ {
		ps = append(ps, sig.Params().At(i))
	}
	return types2.NewSignatureType(nil, nil, nil, types2.NewTuple(ps...), sig.Results(), sig.Variadic())
}

// valueLoads returns the per-CPU variables that a function used as a value,
// of type sig, may load.
func (a *analyzer) valueLoads(sig *types2.Signature) []*types2.Var {
	return a.valueVars(sig, func(fi *cpuFnInfo) map[*types2.Var]bool { return fi.loads })
}

// valueStores returns the per-CPU variables that a function used as a value,
// of type sig, may store.
func (a *analyzer) valueStores(sig *types2.Signature) []*types2.Var {
	return a.valueVars(sig, func(fi *cpuFnInfo) map[*types2.Var]bool { return fi.stores })
}

func (a *analyzer) valueVars(sig *types2.Signature, pick func(*cpuFnInfo) map[*types2.Var]bool) []*types2.Var {
	set := map[*types2.Var]bool{}
	for _, f := range a.valueFns(sig) {
		if fi := a.cpuFn[f]; fi != nil {
			for v := range pick(fi) {
				set[v] = true
			}
		}
	}
	out := make([]*types2.Var, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pos() < out[j].Pos() })
	return out
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

// How the call being read is run.
type callMode int

const (
	modeCall  callMode = iota // now
	modeDefer                 // when the function returns
	modeTask                  // later, by another task
)

// An inlCtx is a literal that is read where it is called: its stores that
// depend on loads made outside it are stores of the call.
type inlCtx struct {
	call   *ast.CallExpr
	lo, hi token.Pos
	rets   taint
	name   string
}

type depWalk struct {
	a  *analyzer
	f  *function
	fi *cpuFnInfo

	headT   taint // what the head of the loop being read read (loop)
	cur     *flow
	pc      taint // the conditions that control the statement being read
	exitPC  taint // the conditions of branches that may leave the function or loop: they control all that follows
	weak    int   // in a function literal: an assignment adds, it does not replace
	addr    map[*types2.Var]bool
	brk     []denv // the states at break and continue of the enclosing loops and switches
	gotoEnv denv
	changed bool

	simple    map[*types2.Var]*ast.FuncLit // local variables that hold a literal and are only called
	simpleLit map[*ast.FuncLit]bool
	lits      []*ast.FuncLit // literals that may run at any time, being read
	inls      []*inlCtx      // literals that are read where they are called
	mode      callMode
	taskCall  *ast.CallExpr

	heapNow    taint // what this walk has written into memory so far
	cpuHeapNow map[*types2.Var]taint
	persist    bool // the body has a goto: reads see all that was ever written
}

func (d *depWalk) poolSize() int {
	n := len(d.fi.heap) + len(d.gotoEnv)
	for _, t := range d.fi.ever {
		n += len(t)
	}
	for _, t := range d.fi.cpuHeap {
		n += len(t)
	}
	return n
}

func (d *depWalk) run() {
	d.addr = d.addrTaken()
	d.findSimple()
	minPasses := 1
	if len(gotoTargets(d.f.body)) > 0 {
		minPasses = 3
		d.persist = true
	}
	for pass := 0; pass < 100; pass++ {
		before := d.poolSize()
		d.cur = &flow{env: denv{}}
		d.pc, d.exitPC = nil, nil
		d.lits, d.inls, d.mode, d.taskCall = nil, nil, modeCall, nil
		d.heapNow, d.cpuHeapNow = nil, map[*types2.Var]taint{}
		for k, p := range d.f.params {
			if p.Name() == "" || p.Name() == "_" {
				continue
			}
			d.setVar(p, taint{{src: -(k + 1)}}, false)
		}
		d.block(d.f.body)
		if pass+1 >= minPasses && d.poolSize() == before {
			break
		}
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
		case *ast.RangeStmt:
			// A range over an array variable reads it where it is.
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

// findSimple finds the literals that the program can see run: those assigned
// to a local variable that is declared with them and is only ever called, in
// the function that holds the variable (not in a literal that may run later,
// not deferred, not started as a task). Such a literal is read at each call,
// with the state there.
func (d *depWalk) findSimple() {
	d.simple = d.a.findSimple(d.f.body)
	d.simpleLit = map[*ast.FuncLit]bool{}
	for _, lit := range d.simple {
		d.simpleLit[lit] = true
	}
}

func (a *analyzer) findSimple(root ast.Node) map[*types2.Var]*ast.FuncLit {
	simple := map[*types2.Var]*ast.FuncLit{}
	cand := map[*types2.Var]*ast.FuncLit{}
	defAt := map[*types2.Var][]*ast.FuncLit{}
	var stack []ast.Node
	var lits []*ast.FuncLit
	ancestors := func() []*ast.FuncLit { return append([]*ast.FuncLit(nil), lits...) }
	same := func(a, b []*ast.FuncLit) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	define := func(id *ast.Ident, rhs ast.Expr) {
		lit, ok := ast.Unparen(rhs).(*ast.FuncLit)
		if !ok {
			return
		}
		if v, ok := a.defs[id].(*types2.Var); ok {
			cand[v] = lit
			defAt[v] = ancestors()
		}
	}
	bad := map[*types2.Var]bool{}
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if _, ok := top.(*ast.FuncLit); ok {
				lits = lits[:len(lits)-1]
			}
			return true
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE && len(n.Lhs) == len(n.Rhs) {
				for i, l := range n.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						define(id, n.Rhs[i])
					}
				}
			}
		case *ast.ValueSpec:
			if len(n.Values) == len(n.Names) {
				for i, id := range n.Names {
					define(id, n.Values[i])
				}
			}
		case *ast.Ident:
			if v, ok := a.uses[n].(*types2.Var); ok {
				if _, ok := cand[v]; ok {
					okUse := false
					if len(stack) >= 1 {
						if call, ok := stack[len(stack)-1].(*ast.CallExpr); ok && ast.Unparen(call.Fun) == ast.Expr(n) {
							okUse = same(lits, defAt[v])
							if len(stack) >= 2 {
								switch p := stack[len(stack)-2].(type) {
								case *ast.DeferStmt:
									okUse = okUse && p.Call != call
								case *ast.TaskStmt:
									okUse = okUse && p.Call != call
								}
							}
						}
					}
					if !okUse {
						bad[v] = true
					}
				}
			}
		}
		stack = append(stack, n)
		if l, ok := n.(*ast.FuncLit); ok {
			lits = append(lits, l)
		}
		return true
	})
	for v, lit := range cand {
		if !bad[v] {
			simple[v] = lit
		}
	}
	return simple
}

// rangeCall returns the call of the func that a range over a func makes.
func (a *analyzer) rangeCall(s *ast.RangeStmt) *ast.CallExpr {
	if c := a.rangeCalls[s]; c != nil {
		return c
	}
	if a.rangeCalls == nil {
		a.rangeCalls = map[*ast.RangeStmt]*ast.CallExpr{}
	}
	c := &ast.CallExpr{Fun: s.X, Lparen: s.X.Pos()}
	a.rangeCalls[s] = c
	return c
}

// effPC is every condition that controls the statement being read.
func (d *depWalk) effPC() taint { return d.pc.union(d.exitPC) }

// heapAdd records that t was written into memory. The function's pool keeps
// everything written in any walk of the body; the walk's own pool only what
// was written before the point being read, which is what a read in the body
// can see, except in a literal that may run at any time, in a function with a
// goto, and in a loop, whose second pass sees the first's.
func (d *depWalk) heapAdd(t taint) {
	t = t.noPtr()
	d.fi.heap = d.fi.heap.union(t)
	d.heapNow = d.heapNow.union(t)
}

func (d *depWalk) heapRead() taint {
	if len(d.lits) > 0 || d.persist {
		return d.fi.heap
	}
	return d.heapNow
}

func (d *depWalk) cpuHeapRead(v *types2.Var) taint {
	if len(d.lits) > 0 || d.persist {
		return d.fi.cpuHeap[v]
	}
	return d.cpuHeapNow[v]
}

// setVar assigns a local variable. A literal or a pointer may read it later,
// so every value it ever holds is also kept.
func (d *depWalk) setVar(v *types2.Var, t taint, weak bool) {
	if weak || d.weak > 0 || d.addr[v] {
		d.cur.env[v] = d.cur.env[v].union(t)
	} else {
		d.cur.env[v] = t
	}
	d.fi.ever[v] = d.fi.ever[v].union(t)
	if d.addr[v] {
		d.heapAdd(t)
	}
}

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
		d.assignTo(s.X, d.readOwn(s.X), s.Pos(), false)
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
		t := d.loop(func() {
			d.headT = nil
			if s.Cond != nil {
				d.headT = d.ex(s.Cond)
			}
		}, func() { d.block(s.Body) }, func() { d.stmt(s.Post) })
		if s.Cond != nil {
			// The loop ends when the condition is false: what follows runs
			// only for what the condition read.
			d.exitPC = d.exitPC.union(t.ctl())
		}
	case *ast.RangeStmt:
		xt := d.ex(s.X)
		if _, ok := underlying(d.a.typeOf(s.X)).(*types2.Signature); ok {
			// A range over a func calls it with the loop body: what it loads
			// and stores is a call's.
			xt = xt.union(d.callTaint(d.a.rangeCall(s)))
		}
		keyT := xt
		if _, arr := underlying(d.a.typeOf(s.X)).(*types2.Array); arr {
			keyT = nil // the index of an array does not depend on what it holds
		} else {
			xt = xt.deref().union(d.heapRead())
			keyT = xt
		}
		d.loop(func() {
			for i, e := range []ast.Expr{s.Key, s.Value} {
				if e == nil {
					continue
				}
				t := xt
				if i == 0 {
					t = keyT
				}
				d.assignTo(e, t, s.Pos(), s.Tok == token.DEFINE)
			}
			d.headT = keyT
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
				d.setVar(v, t, false)
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
		var rt taint
		for _, r := range s.Results {
			rt = rt.union(d.ex(r))
		}
		if n := len(d.inls); n > 0 {
			d.inls[n-1].rets = d.inls[n-1].rets.union(rt)
		}
		d.cur.dead = true
	case *ast.DeferStmt:
		d.mode = modeDefer
		d.ex(s.Call)
		d.mode = modeCall
	case *ast.TaskStmt:
		d.mode, d.taskCall = modeTask, s.Call
		d.ex(s.Call)
		d.mode, d.taskCall = modeCall, nil
	case *ast.SendStmt:
		d.ex(s.Chan)
		d.heapAdd(d.ex(s.Value).union(d.effPC()))
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

// loop walks a loop twice, so that what the body assigns reaches its top. It
// returns the taint of the head condition at the last pass.
func (d *depWalk) loop(head func(), body, post func()) taint {
	entry := d.cur
	acc := denv{}
	d.brk = append(d.brk, acc)
	var t taint
	for i := 0; i < 2; i++ {
		d.cur = entry.clone()
		head() // leaves what it read in d.headT
		t = d.headT
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
	return t
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
		t := d.ex(s.Rhs[0]).union(d.readOwn(s.Lhs[0]))
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
		v, ok := d.a.objOf(id).(*types2.Var)
		if !ok {
			return
		}
		switch {
		case d.a.cpuVars[v]:
			d.storeSite(v, t, id.Pos(), nil, "")
			d.cpuHeapAdd(v, t)
		case isGlobal(v):
			d.heapAdd(t)
		default:
			d.setVar(v, t, false)
		}
		return
	}
	// A part of a variable, or other memory.
	if id := d.a.storeRoot(lhs); id != nil {
		if v, ok := d.a.objOf(id).(*types2.Var); ok {
			switch {
			case d.a.cpuVars[v]:
				d.storeSite(v, t, id.Pos(), nil, "")
				d.cpuHeapAdd(v, t)
			case isGlobal(v):
				d.heapAdd(t)
			default:
				d.setVar(v, t, true)
			}
			d.ex(lhs)
			return
		}
	}
	// Memory reached through a pointer, a slice or a map: if the pointer
	// points to a per-CPU variable, this is a store of it.
	d.ptrStoreSite(d.ptrBase(lhs), t, nil, lhs.Pos(), nil, "")
	d.heapAdd(t)
	d.ex(lhs)
}

// readOwn reads the target of x++ or x op= y, which the statement then
// stores. What it reads of the variable it stores, by name or through a
// pointer to it, is not a load the store depends on: reading and writing it in
// one statement is a read-modify-write that needs a region on its own account.
func (d *depWalk) readOwn(lhs ast.Expr) taint {
	t := d.ex(lhs)
	self := d.ptrBase(lhs).reads()
	if id := d.a.storeRoot(lhs); id != nil {
		if v, ok := d.a.objOf(id).(*types2.Var); ok && d.a.cpuVars[v] {
			self = self.union(taint{{src: d.a.loadAt(id, v)}})
		}
	}
	if len(self) == 0 {
		return t
	}
	out := make(taint, 0, len(t))
	for _, e := range t {
		if e.ptr != nil || !self.has(depElem{src: e.src}) {
			out = append(out, e)
		}
	}
	return out
}

// cpuHeapAdd records that t was stored in per-CPU variable v: a later load of
// v, in a store of another per-CPU variable, carries it. What was loaded from
// v itself is left out: a later load of v reads v afresh, and a store that
// follows it depends on that load.
func (d *depWalk) cpuHeapAdd(v *types2.Var, t taint) {
	var keep taint
	for _, e := range t {
		if e.ptr == nil && e.src >= 0 && d.a.cpuLoads[e.src].v == v {
			continue
		}
		if e.ptr != nil {
			continue
		}
		keep = append(keep, e)
	}
	d.fi.cpuHeap[v] = d.fi.cpuHeap[v].union(keep)
	d.cpuHeapNow[v] = d.cpuHeapNow[v].union(keep)
}

// ptrBase returns the pointers that the memory of an addressable expression
// is reached through: for a part of a per-CPU variable, a pointer to the
// variable, and for memory a pointer, a slice or a pointer to an array
// leads to, what that value depends on.
func (d *depWalk) ptrBase(x ast.Expr) taint {
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		if v, ok := d.a.objOf(x).(*types2.Var); ok && d.a.cpuVars[v] {
			if !d.fi.addr[v] {
				d.fi.addr[v] = true
				d.changed = true
			}
			return taint{{src: d.a.loadAt(x, v), ptr: v}}
		}
	case *ast.SelectorExpr:
		sel := d.a.selections[x]
		switch {
		case sel == nil:
			return d.ptrBase(x.Sel) // a qualified identifier
		case sel.Kind() == types2.FieldVal:
			if pointerElem(d.a.typeOf(x.X)) != nil {
				return d.ex(x.X)
			}
			return d.ptrBase(x.X)
		}
	case *ast.IndexExpr:
		switch underlying(d.a.typeOf(x.X)).(type) {
		case *types2.Array:
			return d.ptrBase(x.X)
		case *types2.Pointer, *types2.Slice:
			return d.ex(x.X)
		}
	case *ast.StarExpr:
		return d.ex(x.X)
	}
	return nil
}

// ptrStoreSite records a store of a value with taint tv through pointers
// with taint tp: a store of each per-CPU variable they point to, and, for
// the pointers that are parameters, a fact about the function that its
// callers use. own gives what the callee loads that goes into the value; pos
// and e say where the store is.
func (d *depWalk) ptrStoreSite(tp, tv taint, own func(*types2.Var) taint, pos token.Pos, e *ast.CallExpr, callee string) {
	for _, el := range tp {
		switch {
		case el.ptr != nil:
			t := tv
			if own != nil {
				t = t.union(own(el.ptr))
			}
			d.storeSite(el.ptr, t, pos, e, callee)
		case el.src < 0:
			k := -el.src - 1
			ps := d.fi.pstore[k]
			if ps == nil {
				ps = &pstoreInfo{}
				d.fi.pstore[k] = ps
				d.changed = true
			}
			for _, x := range tv {
				switch {
				case x.ptr != nil:
				case x.src < 0:
					if j := -x.src - 1; !ps.from.has(j) {
						ps.from = ps.from.with(j)
						d.changed = true
					}
				default:
					if !ps.own {
						ps.own = true
						d.changed = true
					}
				}
			}
		}
	}
}

// storeSite records a store of per-CPU variable v of a value with taint t,
// at the identifier or expression at pos, or at the call e of a function
// that stores it.
func (d *depWalk) storeSite(v *types2.Var, t taint, pos token.Pos, e *ast.CallExpr, callee string) {
	if !d.fi.stores[v] {
		d.fi.stores[v] = true
		d.changed = true
	}
	var loads []int
	data := false
	opaque := ""
	for _, el := range t {
		if el.ptr != nil {
			continue
		}
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
	// A store inside a literal that is read where it is called, of a value
	// loaded outside the literal, is a store of the call.
	for i := len(d.inls) - 1; i >= 0 && len(loads) > 0; i-- {
		c := d.inls[i]
		var in, out []int
		for _, n := range loads {
			if p := d.a.cpuLoads[n].pos; p >= c.lo && p < c.hi {
				in = append(in, n)
			} else {
				out = append(out, n)
			}
		}
		if len(in) > 0 {
			d.site(v, in, data, opaque, pos, e, callee)
		}
		loads, pos, e, callee = out, c.call.Pos(), c.call, c.name
	}
	if len(loads) > 0 {
		d.site(v, loads, data, opaque, pos, e, callee)
	}
}

func (d *depWalk) site(v *types2.Var, loads []int, data bool, opaque string, pos token.Pos, e *ast.CallExpr, callee string) {
	var site *cpuDepSite
	if e != nil {
		for _, s := range d.a.cpuDepCalls[e] {
			if s.v == v {
				site = s
			}
		}
		if site == nil {
			site = &cpuDepSite{v: v, callee: callee}
			d.a.cpuDepCalls[e] = append(d.a.cpuDepCalls[e], site)
		}
		if e == d.taskCall {
			site.task = true
		}
	} else {
		k := depKey{pos, v}
		site = d.a.cpuDeps[k]
		if site == nil {
			site = &cpuDepSite{v: v}
			d.a.cpuDeps[k] = site
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
		return d.lit(e)
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
		switch sel.Kind() {
		case types2.MethodVal:
			return d.recvTaint(e, sel)
		case types2.MethodExpr:
			return nil
		}
		t := d.ex(e.X)
		if pointerElem(d.a.typeOf(e.X)) != nil {
			t = t.deref().union(d.heapRead())
		}
		return t
	case *ast.IndexExpr:
		t := d.ex(e.X).union(d.ex(e.Index))
		if _, ok := underlying(d.a.typeOf(e.X)).(*types2.Array); !ok {
			t = t.deref().union(d.heapRead())
		}
		return t
	case *ast.IndexListExpr:
		return d.ex(e.X)
	case *ast.SliceExpr:
		t := d.ex(e.X).union(d.ex(e.Low)).union(d.ex(e.High)).union(d.ex(e.Max)).union(d.heapRead())
		if _, ok := underlying(d.a.typeOf(e.X)).(*types2.Array); ok {
			// Slicing an array takes its address.
			t = t.union(d.ptrBase(e.X))
		}
		return t
	case *ast.TypeAssertExpr:
		return d.ex(e.X)
	case *ast.StarExpr:
		return d.ex(e.X).deref().union(d.heapRead())
	case *ast.UnaryExpr:
		switch e.Op {
		case token.AND:
			t := d.ptrBase(e.X)
			xt := d.ex(e.X)
			if _, ok := ast.Unparen(e.X).(*ast.CompositeLit); ok {
				t = t.union(xt)
			}
			return t
		case token.ARROW:
			return d.ex(e.X).deref().union(d.heapRead())
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

// recvTaint is the receiver of a method value or call: for a method with a
// pointer receiver on a part of a per-CPU variable, a pointer to the variable.
func (d *depWalk) recvTaint(se *ast.SelectorExpr, sel *types2.Selection) taint {
	t := d.ex(se.X)
	if fn, ok := sel.Obj().(*types2.Func); ok {
		if r := fn.Type().(*types2.Signature).Recv(); r != nil && pointerElem(r.Type()) != nil && pointerElem(d.a.typeOf(se.X)) == nil {
			t = t.union(d.ptrBase(se.X))
		}
	}
	return t
}

func (d *depWalk) ident(id *ast.Ident) taint {
	v, ok := d.a.objOf(id).(*types2.Var)
	if !ok {
		return nil
	}
	switch {
	case d.a.cpuVars[v]:
		t := taint{{src: d.a.loadAt(id, v)}}.union(d.cpuHeapRead(v))
		return t
	case isGlobal(v):
		return d.heapRead()
	}
	t := d.cur.env[v]
	if d.addr[v] {
		t = t.union(d.heapRead())
	}
	if d.captured(v) {
		t = t.union(d.fi.ever[v])
	}
	return t
}

// captured reports whether v is declared outside the function literal that
// may run at any time and is being read: it holds whatever it ever held.
func (d *depWalk) captured(v *types2.Var) bool {
	if len(d.lits) == 0 {
		return false
	}
	l := d.lits[len(d.lits)-1]
	return v.Pos() < l.Pos() || v.Pos() >= l.End()
}

// lit walks a function literal that may run at any time, any number of times,
// where it appears: what it assigns is added to the variables, what it reads
// of the variables around it is whatever they ever held, and what it returns
// or exits from ends only itself. A literal that the program calls where it
// can see it is read at its calls instead.
func (d *depWalk) lit(l *ast.FuncLit) taint {
	if d.simpleLit[l] {
		return nil
	}
	saved, savedPC, savedExit := d.cur, d.pc, d.exitPC
	savedBrk, savedInls, savedMode := d.brk, d.inls, d.mode
	d.cur = saved.clone()
	d.brk, d.inls, d.mode = nil, nil, modeCall
	d.lits = append(d.lits, l)
	d.weak++
	d.block(l.Body)
	d.weak--
	d.lits = d.lits[:len(d.lits)-1]
	inner := d.cur.env
	d.cur, d.pc, d.exitPC, d.brk, d.inls, d.mode = saved, savedPC, savedExit, savedBrk, savedInls, savedMode
	d.cur.env.merge(inner)
	// A call of the literal through a func value is a call of a function of
	// its type that the program uses as a value: the loads it makes are
	// those of the call.
	return nil
}

// inlineCall reads a literal where it is called, with the arguments' taints
// as its parameters and the variables' state at the call.
func (d *depWalk) inlineCall(e *ast.CallExpr, l *ast.FuncLit, args []taint) taint {
	var params []*types2.Var
	if l.Type != nil && l.Type.Params != nil {
		for _, f := range l.Type.Params.List {
			for _, n := range f.Names {
				if v, ok := d.a.defs[n].(*types2.Var); ok {
					params = append(params, v)
				}
			}
		}
	}
	var all taint
	for _, t := range args {
		all = all.union(t)
	}
	for i, p := range params {
		t := all
		if len(args) == len(params) {
			t = args[i]
		}
		d.setVar(p, t, true)
	}
	savedPC, savedExit, savedBrk, savedMode := d.pc, d.exitPC, d.brk, d.mode
	ctx := &inlCtx{call: e, lo: l.Pos(), hi: l.End(), name: "a function literal"}
	if id, ok := ast.Unparen(e.Fun).(*ast.Ident); ok {
		ctx.name = id.Name
	}
	d.inls = append(d.inls, ctx)
	d.brk, d.mode = nil, modeCall
	d.weak++
	wasDead := d.cur.dead
	d.block(l.Body)
	d.cur.dead = wasDead
	d.weak--
	d.inls = d.inls[:len(d.inls)-1]
	d.pc, d.exitPC, d.brk, d.mode = savedPC, savedExit, savedBrk, savedMode
	return ctx.rets
}

// argTaints reads the arguments of a call.
func (d *depWalk) argTaints(e *ast.CallExpr) []taint {
	var out []taint
	for _, a := range e.Args {
		out = append(out, d.ex(a))
	}
	return out
}

// paramArgs returns what the arguments given for parameter k of g depend on.
func (d *depWalk) paramArgs(e *ast.CallExpr, g *function, args []taint, k int) taint {
	multi := len(e.Args) == 1
	if multi {
		if _, ok := d.a.typeOf(e.Args[0]).(*types2.Tuple); !ok {
			multi = false
		}
	}
	var t taint
	for i, a := range args {
		j := i
		if j >= len(g.params) {
			j = len(g.params) - 1
		}
		if j == k || multi {
			t = t.union(a)
		}
	}
	return t
}

// callTaint walks a call and returns what its results depend on.
func (d *depWalk) callTaint(e *ast.CallExpr) taint {
	mode := d.mode
	d.mode = modeCall
	if tv, ok := d.a.types[e.Fun]; ok && tv.IsType() {
		var t taint
		for _, a := range e.Args {
			t = t.union(d.ex(a))
		}
		return t
	}
	fun := ast.Unparen(stripIndex(e.Fun))
	if id, ok := fun.(*ast.Ident); ok {
		if b, ok := d.a.uses[id].(*types2.Builtin); ok {
			all := d.argTaints(e)
			var t taint
			for _, a := range all {
				t = t.union(a)
			}
			switch b.Name() {
			case "append", "copy":
				d.heapAdd(t)
			}
			return t
		}
	}
	if mode == modeCall {
		// A literal called where it is written, or held by a variable that is
		// only called.
		var lit *ast.FuncLit
		switch f := fun.(type) {
		case *ast.FuncLit:
			lit = f
		case *ast.Ident:
			if v, ok := d.a.objOf(f).(*types2.Var); ok {
				lit = d.simple[v]
			}
		}
		if lit != nil {
			return d.inlineCall(e, lit, d.argTaints(e))
		}
	}
	var recv taint
	hasRecv := false
	if se, ok := fun.(*ast.SelectorExpr); ok {
		if sel := d.a.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
			recv = d.recvTaint(se, sel)
			hasRecv = true
		} else if sel != nil {
			recv = d.ex(se.X) // a func-valued field
		}
	} else if id, isIdent := fun.(*ast.Ident); !isIdent {
		recv = d.ex(fun) // a literal, or another expression
	} else if _, isVar := d.a.objOf(id).(*types2.Var); isVar {
		recv = d.ex(fun) // a func-valued variable
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
	ci := d.a.callInfo(e)
	pc := d.effPC()
	// A callee whose body cannot be seen may store through every pointer it
	// is given, from everything it is given.
	unseen := ci.opaque
	for _, g := range ci.cands {
		unseen = unseen || d.a.cpuFn[g] == nil
	}
	base := all
	if unseen {
		base = all.through(ci.name)
	}
	// The result depends on the arguments, and on what the callee may read
	// through the pointers it is given.
	res := base.union(base.reads()).union(d.heapRead())
	var carried []*types2.Var // the per-CPU variables the callee may store: they carry the result
	for _, g := range ci.cands {
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
			carried = append(carried, v)
		}
		pks := make([]int, 0, len(gi.pstore))
		for k := range gi.pstore {
			pks = append(pks, k)
		}
		sort.Ints(pks)
		for _, k := range pks {
			ps := gi.pstore[k]
			var tv taint
			for j := range g.params {
				if ps.from.has(j) {
					tv = tv.union(d.paramArgs(e, g, args, j))
				}
			}
			tv = tv.union(pc)
			own := func(v *types2.Var) taint {
				if !ps.own || !gi.loads[v] {
					return nil
				}
				return taint{{src: d.a.callLoad(e, v, g.name)}}
			}
			d.ptrStoreSite(d.paramArgs(e, g, args, k), tv, own, e.Pos(), e, g.name)
		}
		vs := make([]*types2.Var, 0, len(gi.loads))
		for v := range gi.loads {
			vs = append(vs, v)
		}
		sort.Slice(vs, func(i, j int) bool { return vs[i].Pos() < vs[j].Pos() })
		for _, v := range vs {
			t := taint{{src: d.a.callLoad(e, v, g.name)}}
			res = res.union(t)
		}
		if mayHoldPtr(d.a.typeOf(e)) {
			as := make([]*types2.Var, 0, len(gi.addr))
			for v := range gi.addr {
				as = append(as, v)
			}
			sort.Slice(as, func(i, j int) bool { return as[i].Pos() < as[j].Pos() })
			for _, v := range as {
				res = res.union(taint{{src: d.a.callLoad(e, v, g.name), ptr: v}})
				if !d.fi.addr[v] {
					d.fi.addr[v] = true
					d.changed = true
				}
			}
		}
	}
	if unseen {
		d.ptrStoreSite(all, base.union(pc), nil, e.Pos(), e, ci.name)
	}
	if ci.fv {
		// A call of a func value: it may reach any function of its type that
		// the program uses as a value. What the function does with its
		// arguments, or under the caller's condition, is not seen: if one of
		// them stores a per-CPU variable, the call is taken to store it from
		// all the arguments.
		sig := ci.sig
		if sig == nil {
			sig, _ = underlying(d.a.typeOf(e.Fun)).(*types2.Signature)
		}
		if sig != nil {
			for _, v := range d.a.valueLoads(sig) {
				t := taint{{src: d.a.callLoad(e, v, ci.name)}}
				res = res.union(t)
			}
			for _, v := range d.a.valueStores(sig) {
				d.storeSite(v, base.union(pc), e.Pos(), e, ci.name)
				carried = append(carried, v)
			}
		}
	}
	// A callee that can leave its caller (a panic, rt.ExitTask) decides
	// whether what follows runs: it is controlled by what the callee loads.
	if d.a.callMayLeave(e) {
		for _, g := range ci.cands {
			if gi := d.a.cpuFn[g]; gi != nil {
				for v := range gi.loads {
					d.exitPC = d.exitPC.union(taint{{src: d.a.callLoad(e, v, g.name)}}.ctl())
				}
			}
		}
	}
	// Everything the call was given or returns may have been written into
	// memory by the callee, or into the per-CPU variables it stores.
	d.heapAdd(res.union(pc))
	for _, v := range carried {
		d.cpuHeapAdd(v, res.union(pc))
	}
	return res
}

// mayHoldPtr reports whether a value of type T can hold a pointer.
func mayHoldPtr(T types2.Type) bool {
	switch u := underlying(T).(type) {
	case nil:
		return false
	case *types2.Basic:
		return u.Kind() == types2.UnsafePointer
	case *types2.Tuple:
		for i := 0; i < u.Len(); i++ {
			if mayHoldPtr(u.At(i).Type()) {
				return true
			}
		}
		return false
	case *types2.Array:
		return mayHoldPtr(u.Elem())
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if mayHoldPtr(u.Field(i).Type()) {
				return true
			}
		}
		return false
	}
	return true
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
			found = d.a.callNeverReturns(n) || d.a.callMayLeave(n)
		}
		return !found
	})
	return found
}

// callNeverReturns reports whether a call may reach a function that never
// returns.
func (a *analyzer) callNeverReturns(e *ast.CallExpr) bool {
	ci := a.callInfo(e)
	for _, g := range ci.cands {
		if a.neverReturns(g) {
			return true
		}
	}
	if g := ci.static; g != nil && g.body == nil && a.noReturnDirective(g) {
		return true
	}
	if ci.fv {
		for _, g := range a.valueFns(ci.sig) {
			if a.neverReturns(g) {
				return true
			}
		}
	}
	return false
}

// callMayLeave reports whether a call may reach a function that can leave its
// caller without returning to it: one that panics or ends the task.
func (a *analyzer) callMayLeave(e *ast.CallExpr) bool {
	ci := a.callInfo(e)
	if g := ci.static; g != nil && a.mayLeave(g) {
		return true
	}
	for _, g := range ci.cands {
		if a.mayLeave(g) {
			return true
		}
	}
	if ci.fv {
		for _, g := range a.valueFns(ci.sig) {
			if a.mayLeave(g) {
				return true
			}
		}
	}
	return false
}

// mayLeave reports whether a function can leave its caller without
// returning to it: rt.ExitTask, a function with a panic statement, or one
// that calls such a function (computeLeave). A panic that is raised by the
// language (a bad index, a nil pointer) is not counted.
func (a *analyzer) mayLeave(g *function) bool { return a.cpuLeave[g] }

// computeLeave finds the functions that may leave their callers, for every
// function of the program with a body, the runtime's included.
func (a *analyzer) computeLeave() {
	type node struct {
		callees []*function
		sigs    []*types2.Signature
	}
	nodes := map[*function]*node{}
	var order []*function
	for _, u := range a.units {
		for _, f := range u.fns {
			if f.body == nil || nodes[f] != nil {
				continue
			}
			n := &node{}
			nodes[f] = n
			order = append(order, f)
			if f.obj != nil && f.obj.Name() == "ExitTask" && f.obj.Pkg() != nil && f.obj.Pkg().Path() == "pluto/rt" && f.sig.Recv() == nil {
				a.cpuLeave[f] = true
			}
			simple := a.findSimple(f.body)
			ast.Inspect(f.body, func(x ast.Node) bool {
				c, ok := x.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := ast.Unparen(c.Fun).(*ast.Ident); ok {
					if b, ok := a.uses[id].(*types2.Builtin); ok && b.Name() == "panic" {
						a.cpuLeave[f] = true
					}
					if v, ok := a.uses[id].(*types2.Var); ok && simple[v] != nil {
						return true // a literal that is read where it is called
					}
				}
				ci := a.callInfo(c)
				n.callees = append(n.callees, ci.cands...)
				if g := ci.static; g != nil && g.body == nil && a.noReturnDirective(g) {
					a.cpuLeave[f] = true
				}
				if ci.fv && ci.sig != nil {
					n.sigs = append(n.sigs, ci.sig)
				}
				return true
			})
		}
	}
	for changed := true; changed; {
		changed = false
		for _, f := range order {
			if a.cpuLeave[f] {
				continue
			}
			n := nodes[f]
			leaves := false
			for _, g := range n.callees {
				leaves = leaves || a.cpuLeave[g]
			}
			for _, sig := range n.sigs {
				for _, g := range a.valueFns(sig) {
					leaves = leaves || a.cpuLeave[g]
				}
			}
			if leaves {
				a.cpuLeave[f] = true
				changed = true
			}
		}
	}
}

// noReturnDirective reports whether a function without a body is marked
// //pu:noreturn.
func (a *analyzer) noReturnDirective(g *function) bool {
	if g.decl == nil || g.decl.Doc == nil {
		return false
	}
	for _, c := range g.decl.Doc.List {
		if n, _, ok := directive.Split(c.Text); ok && n == "noreturn" {
			return true
		}
	}
	return false
}

// neverReturns reports whether a function appears never to return: no return
// statement, and a body that ends in a panic, a loop without a condition or a
// call of such a function. A recursive chain counts as one that never returns.
func (a *analyzer) neverReturns(g *function) bool {
	if g.body == nil {
		return false
	}
	switch a.cpuNever[g] {
	case 1:
		return true // busy
	case 2:
		return true
	case 3:
		return false
	}
	a.cpuNever[g] = 1
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
	if res {
		a.cpuNever[g] = 2
	} else {
		a.cpuNever[g] = 3
	}
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
