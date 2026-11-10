package race

import (
	"compiler/compat"
	"compiler/internal/constant"
	"compiler/internal/diag"
	"compiler/internal/scoped"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/sort"
	"pluto/strings"
)

// This file checks the rule of language spec section 19.3, Releasing a
// lock (E3504): every Unlock or RUnlock of a lock releases a lock that the
// calling party holds there, on every path, in the right mode. Locks are
// followed by their names (section 19.3, Locks and names): a variable that
// is not a pointer, or a local or parameter that holds one, followed by
// field selections and constant array indexes. Each function gets a
// summary: the locks it needs its caller to hold, named by its package
// variables, its pointer parameters and its captures, and the locks it
// leaves held. A call maps the summary to the caller's names. A function
// that may start a party (main, init, a task, a func value, a method
// reached through an interface, an interrupt handler) starts with nothing
// held, so it may need nothing.

// An rname is the name of a lock: a variable and a path of steps from it.
// The steps are "*" (what the variable points to, only right after the
// variable), "@" (the object of an owned box), ".f" (field f) and "[k]"
// (constant index k of an array).
type rname struct {
	v    *types2.Var
	path string
}

// rstepEnd returns where the step of path that starts at i ends.
func rstepEnd(path string, i int) int {
	switch path[i] {
	case '*', '@':
		return i + 1
	case '[':
		j := i + 1
		for j < len(path) && path[j] != ']' {
			j++
		}
		if j < len(path) {
			j++
		}
		return j
	}
	j := i + 1
	for j < len(path) && path[j] != '*' && path[j] != '@' && path[j] != '.' && path[j] != '[' {
		j++
	}
	return j
}

// under reports whether n is m or a part of it.
func (n rname) under(m rname) bool {
	if n.v != m.v || !strings.HasPrefix(n.path, m.path) {
		return false
	}
	if len(n.path) == len(m.path) {
		return true
	}
	c := n.path[len(m.path)]
	return c == '*' || c == '@' || c == '.' || c == '['
}

// text writes the name as source would.
func (n rname) text() string {
	s := n.v.Name()
	p := n.path
	for i := 0; i < len(p); {
		j := rstepEnd(p, i)
		switch p[i] {
		case '*':
			if j == len(p) {
				s = compat.Concat("*", s)
			}
		case '@':
		default:
			s = compat.Concat(s, strings.Slice(p, i, j))
		}
		i = j
	}
	return s
}

func (n rname) key() string {
	return compat.Concat(itoa(int(n.v.Pos())), "|", n.v.Name(), "|", n.path)
}

// An rlock is a lock held: its names and its mode. A lock held only on
// the paths where a deferred call that releases it is pending is held for
// that call alone.
type rlock struct {
	names     []rname
	excl      bool
	deferOnly bool
}

func (l *rlock) has(n rname) bool {
	for _, m := range l.names {
		if m == n {
			return true
		}
	}
	return false
}

func (l *rlock) key() string {
	var ks []string
	for _, n := range l.names {
		ks = compat.Append(ks, n.key())
	}
	sort.Strings(ks)
	k := compat.Join(ks, ",")
	if l.excl {
		k = compat.Concat(k, "|w")
	}
	if l.deferOnly {
		k = compat.Concat(k, "|d")
	}
	return k
}

// A rdefer is a deferred call that releases or takes locks.
type rdefer struct {
	pos  token.Pos
	reqs []rreq
	held []rheld
}

// A rtry is a TryLock whose result a bool variable holds.
type rtry struct {
	n    rname
	excl bool
}

// An rstate is what the check knows at a point of a function.
type rstate struct {
	dead    bool
	held    []*rlock
	alias   map[*types2.Var]rname // pointer locals and what they point to
	why     map[rname]string      // why a name is not held
	tries   map[*types2.Var]rtry
	pending []*rdefer
}

func newRstate() *rstate {
	return pu.New(rstate{alias: map[*types2.Var]rname{}, why: map[rname]string{}, tries: map[*types2.Var]rtry{}})
}

func deadRstate() *rstate {
	s := newRstate()
	s.dead = true
	return s
}

func (s *rstate) clone() *rstate {
	c := newRstate()
	c.dead = s.dead
	for _, l := range s.held {
		c.held = compat.Append(c.held, pu.New(rlock{names: compat.Append([]rname(nil), l.names...), excl: l.excl, deferOnly: l.deferOnly}))
	}
	for k, v := range s.alias {
		c.alias[k] = v
	}
	for k, v := range s.why {
		c.why[k] = v
	}
	for k, v := range s.tries {
		c.tries[k] = v
	}
	c.pending = compat.Append([]*rdefer(nil), s.pending...)
	return c
}

// find returns the lock that n names, or -1.
func (s *rstate) find(n rname) int {
	for i, l := range s.held {
		if l.has(n) {
			return i
		}
	}
	return -1
}

func (s *rstate) drop(i int) {
	s.held = compat.Append(s.held[:i:i], s.held[i+1:]...)
}

// expanded returns the locks held with the names the aliases give them
// too: after p := &s.mu, a lock named s.mu is named *p.
func (s *rstate) expanded() []*rlock {
	var out []*rlock
	for _, l := range s.held {
		c := pu.New(rlock{names: compat.Append([]rname(nil), l.names...), excl: l.excl, deferOnly: l.deferOnly})
		for x, base := range s.alias {
			for _, n := range l.names {
				if n.under(base) {
					m := rname{v: x, path: compat.Concat("*", strings.SliceFrom(n.path, len(base.path)))}
					if !c.has(m) {
						c.names = compat.Append(c.names, m)
					}
				}
			}
		}
		out = compat.Append(out, c)
	}
	return out
}

func (s *rstate) hasPending(pos token.Pos) bool {
	for _, d := range s.pending {
		if d.pos == pos {
			return true
		}
	}
	return false
}

// keepForDefer reports whether lock l of side a, not held on side b, is
// still needed by a deferred call pending on a only.
func keepForDefer(l *rlock, a, b *rstate) bool {
	for _, d := range a.pending {
		if b.hasPending(d.pos) {
			continue
		}
		for _, r := range d.reqs {
			if l.has(r.n) {
				return true
			}
		}
	}
	return false
}

func intersectNames(a, b []rname) []rname {
	var out []rname
	for _, n := range a {
		for _, m := range b {
			if n == m {
				out = compat.Append(out, n)
				break
			}
		}
	}
	return out
}

// rjoin returns the state after either of two paths.
func rjoin(a, b *rstate) *rstate {
	switch {
	case a == nil || a.dead:
		if b == nil {
			return deadRstate()
		}
		return b.clone()
	case b == nil || b.dead:
		return a.clone()
	}
	out := newRstate()
	for v, n := range a.alias {
		if m, ok := b.alias[v]; ok && m == n {
			out.alias[v] = n
		}
	}
	for v, t := range a.tries {
		if u, ok := b.tries[v]; ok && u == t {
			out.tries[v] = t
		}
	}
	ea, eb := a.expanded(), b.expanded()
	used := map[int]bool{}
	for _, la := range ea {
		matched := false
		for j, lb := range eb {
			if used[j] || la.excl != lb.excl {
				continue
			}
			if common := intersectNames(la.names, lb.names); len(common) > 0 {
				out.held = compat.Append(out.held, pu.New(rlock{names: common, excl: la.excl, deferOnly: la.deferOnly || lb.deferOnly}))
				used[j] = true
				matched = true
				break
			}
		}
		if !matched && keepForDefer(la, a, b) {
			out.held = compat.Append(out.held, pu.New(rlock{names: la.names, excl: la.excl, deferOnly: true}))
		}
	}
	for j, lb := range eb {
		if !used[j] && keepForDefer(lb, b, a) {
			out.held = compat.Append(out.held, pu.New(rlock{names: lb.names, excl: lb.excl, deferOnly: true}))
		}
	}
	for k, v := range a.why {
		out.why[k] = v
	}
	for k, v := range b.why {
		if _, ok := out.why[k]; !ok {
			out.why[k] = v
		}
	}
	for _, side := range [][]*rlock{ea, eb} {
		for _, l := range side {
			for _, n := range l.names {
				if out.find(n) < 0 || out.held[out.find(n)].deferOnly {
					if _, ok := out.why[n]; !ok {
						out.why[n] = "it is not held on every path to here"
					}
				}
			}
		}
	}
	out.pending = compat.Append([]*rdefer(nil), a.pending...)
	for _, d := range b.pending {
		if !a.hasPending(d.pos) {
			out.pending = compat.Append(out.pending, d)
		}
	}
	return out
}

func (s *rstate) equal(t *rstate) bool {
	if s.dead || t.dead {
		return s.dead == t.dead
	}
	if len(s.held) != len(t.held) || len(s.alias) != len(t.alias) || len(s.tries) != len(t.tries) || len(s.pending) != len(t.pending) {
		return false
	}
	keys := map[string]bool{}
	for _, l := range s.held {
		keys[l.key()] = true
	}
	for _, l := range t.held {
		if !keys[l.key()] {
			return false
		}
	}
	for v, n := range s.alias {
		if m, ok := t.alias[v]; !ok || m != n {
			return false
		}
	}
	for v, x := range s.tries {
		if y, ok := t.tries[v]; !ok || x != y {
			return false
		}
	}
	for _, d := range s.pending {
		if !t.hasPending(d.pos) {
			return false
		}
	}
	return true
}

// An rreq is a lock a function needs its caller to hold.
type rreq struct {
	n    rname
	excl bool
	pos  token.Pos
	head string // what releases it, for messages: "mu.Unlock() releases mu"
	desc string // the release for the callers' messages: "mu.Unlock() at main.p:7"
}

// An rheld is a lock a function leaves held.
type rheld struct {
	n    rname
	excl bool
}

// An rsum is the summary of a function for this check.
type rsum struct {
	reqs []rreq
	held []rheld
}

func (s *rsum) req(n rname) int {
	for i, r := range s.reqs {
		if r.n == n {
			return i
		}
	}
	return -1
}

func sameRsum(a, b *rsum) bool {
	if len(a.reqs) != len(b.reqs) || len(a.held) != len(b.held) {
		return false
	}
	for _, r := range a.reqs {
		if i := b.req(r.n); i < 0 || b.reqs[i].excl != r.excl {
			return false
		}
	}
	for _, h := range a.held {
		found := false
		for _, g := range b.held {
			found = found || g == h
		}
		if !found {
			return false
		}
	}
	return true
}

// A relCheck is the check of the whole program.
type relCheck struct {
	a       *analyzer
	sums    map[*function]*rsum
	callers map[*function][]*function
	entries map[*function]string // the functions that start with nothing held, and why
	bad     map[*types2.Var]bool // pointer locals that name nothing
	never   map[*function]int8
	exempt  map[*function]bool
	walked  []*function
}

// checkReleases runs the check.
func (a *analyzer) checkReleases() {
	rc := pu.New(relCheck{
		a:       a,
		sums:    map[*function]*rsum{},
		callers: map[*function][]*function{},
		entries: map[*function]string{},
		bad:     map[*types2.Var]bool{},
		never:   map[*function]int8{},
		exempt:  map[*function]bool{},
	})
	var seeds []*function
	for _, u := range a.units {
		if !a.reach[u] {
			continue
		}
		top := u.fns[len(u.fns)-1]
		if top.inits || top.program || rc.isExempt(top) {
			for _, f := range u.fns {
				rc.exempt[f] = true
			}
			continue
		}
		if top.body != nil {
			rc.findBad(top.body)
		}
		for _, f := range u.fns {
			if f.body != nil && rc.scan(f) {
				seeds = compat.Append(seeds, f)
			}
		}
	}
	if m := a.prog.Main; m != nil {
		if obj, ok := m.Types.Scope().Lookup("main").(*types2.Func); ok {
			if f := a.funcs[obj]; f != nil {
				rc.entry(f, "main starts with no lock held")
			}
		}
	}
	for _, p := range a.prog.Packages {
		for _, file := range p.Files {
			for _, d := range file.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "init" && fd.Recv == nil {
					if obj, ok := p.Info.Defs[fd.Name].(*types2.Func); ok {
						rc.entry(a.funcs[obj], "init starts with no lock held")
					}
				}
			}
		}
	}
	for _, h := range a.handlers {
		rc.entry(h, compat.Concat(h.name, " is an interrupt handler, which starts with no lock held"))
	}
	for _, f := range a.values {
		rc.entry(f, compat.Concat(f.name, " is used as a func value, and a function called through a func value or a method value counts as called with no lock held"))
	}
	// Summaries: walk the functions that use locks, then their callers
	// while their summaries change.
	queue := seeds
	queued := map[*function]bool{}
	for _, f := range seeds {
		queued[f] = true
	}
	walks := map[*function]int{}
	seen := map[*function]bool{}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		queued[f] = false
		if !seen[f] {
			seen[f] = true
			rc.walked = compat.Append(rc.walked, f)
		}
		cur := rc.sum(f)
		old := pu.New(rsum{reqs: compat.Append([]rreq(nil), cur.reqs...), held: compat.Append([]rheld(nil), cur.held...)})
		rc.walkFn(f, false)
		walks[f]++
		if sameRsum(old, rc.sum(f)) || walks[f] > 12 {
			continue
		}
		for _, c := range rc.callers[f] {
			if !queued[c] && !rc.exempt[c] {
				queued[c] = true
				queue = compat.Append(queue, c)
			}
		}
	}
	for _, f := range rc.walked {
		rc.walkFn(f, true)
	}
	for _, f := range rc.walked {
		why, ok := rc.entries[f]
		if !ok {
			continue
		}
		for _, r := range rc.sum(f).reqs {
			a.add(compat.Sprintf("rel|%d", int(r.pos)), diag.New(a.pos(r.pos), diag.RaceReleaseNotHeld, "%s, which is not held here: %s", pu.Box(r.head), pu.Box(why)))
		}
	}
}

func (rc *relCheck) sum(f *function) *rsum {
	s := rc.sums[f]
	if s == nil {
		s = pu.New(rsum{})
		rc.sums[f] = s
	}
	return s
}

func (rc *relCheck) entry(f *function, why string) {
	if f == nil {
		return
	}
	if _, ok := rc.entries[f]; !ok {
		rc.entries[f] = why
	}
}

// isExempt reports whether a declared function is outside the check: the
// runtime, and the trusted methods of the lock types and of sync.Cond.
func (rc *relCheck) isExempt(f *function) bool {
	if f.pkg == nil {
		return false
	}
	if p := f.pkg.Path; p == "pluto/rt" || strings.HasPrefix(p, "pluto/rt/") {
		return true
	}
	if f.obj == nil || f.pkg.Path != "pluto/sync" {
		return false
	}
	n := recvNamed(f.obj)
	if n == nil {
		return false
	}
	switch n.Obj().Name() {
	case "Mutex", "RWMutex", "rlocker", "Cond":
		return true
	}
	return false
}

// findBad finds the pointer locals that name nothing: those whose address
// is taken, that a function literal assigns, or that a scoped task
// captures.
func (rc *relCheck) findBad(body *ast.BlockStmt) {
	a := rc.a
	localPtr := func(e ast.Expr) *types2.Var {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok {
			return nil
		}
		v, ok := a.objOf(id).(*types2.Var)
		if !ok || isGlobal(v) || !ptrLike(v.Type()) {
			return nil
		}
		return v
	}
	var lits []*ast.FuncLit
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			lits = compat.Append(lits, n)
			ast.Inspect(n.Body, visit)
			lits = lits[:len(lits)-1]
			return false
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				if v := localPtr(n.X); v != nil {
					rc.bad[v] = true
				}
			}
		case *ast.AssignStmt:
			if len(lits) > 0 {
				lit := lits[len(lits)-1]
				for _, l := range n.Lhs {
					if v := localPtr(l); v != nil && (v.Pos() < lit.Pos() || v.Pos() >= lit.End()) {
						rc.bad[v] = true
					}
				}
			}
		case *ast.CallExpr:
			if name, _ := scoped.ScopeMethod(a.selections, n); name == "Task" {
				for _, arg := range n.Args {
					if lit, ok := ast.Unparen(arg).(*ast.FuncLit); ok {
						ast.Inspect(lit.Body, func(m ast.Node) bool {
							if id, ok := m.(*ast.Ident); ok {
								if v := localPtr(id); v != nil && (v.Pos() < lit.Pos() || v.Pos() >= lit.End()) {
									rc.bad[v] = true
								}
							}
							return true
						})
					}
				}
			}
		}
		return true
	}
	ast.Inspect(body, visit)
}

// ptrLike reports whether a variable of type T holds a pointer that a
// name may follow: a pointer, a shared or synced box, or a type
// parameter (whose type arguments are the lock pointers).
func ptrLike(T types2.Type) bool {
	if T == nil {
		return false
	}
	if _, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return true
	}
	switch u := underlying(T).(type) {
	case *types2.Pointer:
		return true
	case *types2.Wrapper:
		return u.Kind() == types2.WrapShared || u.Kind() == types2.WrapSynced
	}
	return false
}

// staticTarget returns the function a call calls, if it is known: the
// selector and selection of a method value call, and whether the call is
// of a method through an interface.
func (a *analyzer) staticTarget(c *ast.CallExpr) (f *function, se *ast.SelectorExpr, sel *types2.Selection, iface bool) {
	fun := ast.Unparen(c.Fun)
	if ix, ok := fun.(*ast.IndexExpr); ok {
		if _, isFn := underlying(a.typeOf(ix.X)).(*types2.Signature); isFn {
			fun = ast.Unparen(ix.X)
		}
	} else if ix, ok := fun.(*ast.IndexListExpr); ok {
		fun = ast.Unparen(ix.X)
	}
	switch e := fun.(type) {
	case *ast.FuncLit:
		return a.lits[e], nil, nil, false
	case *ast.Ident:
		if fn, ok := a.uses[e].(*types2.Func); ok {
			return a.funcs[fn.Origin()], nil, nil, false
		}
	case *ast.SelectorExpr:
		s := a.selections[e]
		if s == nil {
			if fn, ok := a.uses[e.Sel].(*types2.Func); ok {
				return a.funcs[fn.Origin()], nil, nil, false
			}
			return nil, nil, nil, false
		}
		fn, ok := s.Obj().(*types2.Func)
		if !ok {
			return nil, nil, nil, false
		}
		switch s.Kind() {
		case types2.MethodVal:
			if r := fn.Type().(*types2.Signature).Recv(); r != nil && isInterface(r.Type()) {
				return nil, e, s, true
			}
			if isTypeParamRecv(s) {
				return nil, e, s, false
			}
			return a.funcs[fn.Origin()], e, s, false
		case types2.MethodExpr:
			return a.funcs[fn.Origin()], nil, nil, false
		}
	}
	return nil, nil, nil, false
}

// relOp reports whether a call takes or releases a lock: "lock", "rlock",
// "unlock", "runlock", "trylock" or "tryrlock", with the method's
// selector. A call through a type parameter counts when the method has
// the name of one (tp).
func (a *analyzer) relOp(c *ast.CallExpr) (op string, se *ast.SelectorExpr, tp bool) {
	se, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr)
	if !ok {
		return "", nil, false
	}
	sel := a.selections[se]
	if sel == nil || sel.Kind() != types2.MethodVal {
		return "", nil, false
	}
	fn, ok := sel.Obj().(*types2.Func)
	if !ok {
		return "", nil, false
	}
	tp = isTypeParamRecv(sel)
	if !tp && !isLockType(recvNamed(fn)) {
		return "", nil, false
	}
	switch fn.Name() {
	case "Lock":
		return "lock", se, tp
	case "RLock":
		return "rlock", se, tp
	case "Unlock":
		return "unlock", se, tp
	case "RUnlock":
		return "runlock", se, tp
	case "TryLock":
		return "trylock", se, tp
	case "TryRLock":
		return "tryrlock", se, tp
	}
	return "", nil, false
}

// condWait reports whether c calls Wait of a sync.Cond, with the
// method's selector.
func (a *analyzer) condWait(c *ast.CallExpr) (*ast.SelectorExpr, bool) {
	se, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr)
	if !ok || len(c.Args) != 1 {
		return nil, false
	}
	sel := a.selections[se]
	if sel == nil || sel.Kind() != types2.MethodVal {
		return nil, false
	}
	fn, ok := sel.Obj().(*types2.Func)
	return se, ok && isCondWait(fn)
}

// isCondWait reports whether fn is sync.Cond.Wait.
func isCondWait(fn *types2.Func) bool {
	if fn == nil || fn.Name() != "Wait" {
		return false
	}
	n := recvNamed(fn)
	return n != nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "pluto/sync" && n.Obj().Name() == "Cond"
}

// isRelease reports whether fn is a trusted method that releases a lock
// its caller holds.
func isRelease(fn *types2.Func) bool {
	if isCondWait(fn) {
		return true
	}
	if fn == nil || (fn.Name() != "Unlock" && fn.Name() != "RUnlock") {
		return false
	}
	n := recvNamed(fn)
	if isLockType(n) {
		return true
	}
	return n != nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "pluto/sync" && n.Obj().Name() == "rlocker"
}

// scan records the static calls of f for its callees, marks the functions
// that start with nothing held, and reports the releases through
// interfaces and method values. It reports whether f uses a lock itself.
func (rc *relCheck) scan(f *function) bool {
	a := rc.a
	uses := false
	called := map[ast.Expr]bool{}
	ast.Inspect(f.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.TaskStmt:
			if op, se, _ := a.relOp(n.Call); op == "unlock" || op == "runlock" {
				a.add(compat.Sprintf("rel|%d", int(n.Call.Pos())), diag.New(a.pos(n.Call.Pos()), diag.RaceReleaseNotHeld, "%s() releases %s in a new task, which starts with no lock held", pu.Box(exprText(n.Call.Fun)), pu.Box(exprText(se.X))))
			} else if g, _, _, _ := a.staticTarget(n.Call); g != nil {
				rc.entry(g, compat.Concat(g.name, " runs as a task (at ", a.short(n.Task), "), which starts with no lock held"))
			}
		case *ast.CallExpr:
			called[ast.Unparen(stripIndex(n.Fun))] = true
			if op, _, _ := a.relOp(n); op != "" {
				uses = true
				return true
			}
			if se, ok := a.condWait(n); ok {
				uses = true
				called[se] = true
				return true
			}
			g, se, sel, iface := a.staticTarget(n)
			if g != nil && !slices_contains(rc.callers[g], f) {
				rc.callers[g] = compat.Append(rc.callers[g], f)
			}
			if iface {
				rc.ifaceCall(n, se, sel)
			}
		case *ast.SelectorExpr:
			if called[n] {
				return true
			}
			if sel := a.selections[n]; sel != nil && (sel.Kind() == types2.MethodVal || sel.Kind() == types2.MethodExpr) {
				if fn, ok := sel.Obj().(*types2.Func); ok && isRelease(fn) {
					a.add(compat.Sprintf("rel|%d", int(n.Pos())), diag.New(a.pos(n.Pos()), diag.RaceReleaseNotHeld, "%s used as a func value releases a lock wherever it is called, and a function called through a func value or a method value counts as called with no lock held, so the lock cannot be shown held there", pu.Box(exprText(n))))
				}
			}
		}
		return true
	})
	return uses
}

func slices_contains(fs []*function, f *function) bool {
	for _, g := range fs {
		if g == f {
			return true
		}
	}
	return false
}

// ifaceCall checks a call of a method through an interface: a lock the
// call may release cannot be named, and the methods it may reach start
// with nothing held.
func (rc *relCheck) ifaceCall(c *ast.CallExpr, se *ast.SelectorExpr, sel *types2.Selection) {
	a := rc.a
	fn := sel.Obj().(*types2.Func)
	it, _ := underlying(fn.Type().(*types2.Signature).Recv().Type()).(*types2.Interface)
	if it == nil {
		return
	}
	for _, cand := range a.methodCandidates(fn.Name(), fn.Type().(*types2.Signature)) {
		r := cand.sig.Recv()
		if r == nil || cand.obj == nil {
			continue
		}
		T := r.Type()
		if !types2.Implements(T, it) {
			if _, isPtr := T.(*types2.Pointer); isPtr || !types2.Implements(types2.NewPointer(T), it) {
				continue
			}
		}
		if isRelease(cand.obj) {
			a.add(compat.Sprintf("rel|%d", int(c.Pos())), diag.New(a.pos(c.Pos()), diag.RaceReleaseNotHeld, "%s() releases a lock through the interface %s, so the compiler cannot name the lock or show it held: call the method on the lock's own type (*sync.Mutex) or use a type parameter", pu.Box(exprText(se)), pu.Box(types2.TypeString(sel.Recv(), nil))))
			continue
		}
		rc.entry(cand, compat.Concat(cand.name, " may be called through an interface, and a function called through an interface counts as called with no lock held"))
	}
}

// neverReturns reports whether a call of f never returns.
func (rc *relCheck) neverReturns(f *function) bool {
	if f == nil {
		return false
	}
	switch rc.never[f] {
	case 1, 2:
		return rc.never[f] == 2
	case 3:
		return false
	}
	rc.never[f] = 1
	res := false
	if f.body == nil {
		res = rc.a.noReturnDirective(f)
	} else if n := len(f.body.List); n > 0 {
		hasRet := false
		ast.Inspect(f.body, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ReturnStmt:
				hasRet = true
			}
			return !hasRet
		})
		if !hasRet {
			switch last := f.body.List[n-1].(type) {
			case *ast.ForStmt:
				res = last.Cond == nil && !hasBreak(last.Body)
			case *ast.SelectStmt:
				res = len(last.Body.List) == 0
			case *ast.ExprStmt:
				if c, ok := ast.Unparen(last.X).(*ast.CallExpr); ok {
					res = rc.callNever(c)
				}
			}
		}
	}
	if res {
		rc.never[f] = 2
	} else {
		rc.never[f] = 3
	}
	return res
}

func (rc *relCheck) callNever(c *ast.CallExpr) bool {
	if id, ok := ast.Unparen(c.Fun).(*ast.Ident); ok {
		if b, ok := rc.a.uses[id].(*types2.Builtin); ok {
			return b.Name() == "panic"
		}
	}
	g, _, _, _ := rc.a.staticTarget(c)
	return rc.neverReturns(g)
}

// ----------------------------------------------------------------------------
// The walk of one function

type rtarget struct {
	label  string
	loop   bool
	breaks *rstate
	conts  *rstate
}

type rwalker struct {
	rc      *relCheck
	a       *analyzer
	fn      *function
	sum     *rsum
	st      *rstate
	exit    *rstate
	targets []*rtarget
	gotos   map[string]bool
	gotoAt  map[string]*rstate // the states at the gotos to each label
	prevAt  map[string]*rstate // the same, from the walk before
	report  bool
	quiet   int
}

// walkFn walks f until its summary settles, then once more to report if
// report is set.
func (rc *relCheck) walkFn(f *function, report bool) {
	var prev map[string]*rstate
	for i := 0; i < 6; i++ {
		old := pu.New(rsum{reqs: compat.Append([]rreq(nil), rc.sum(f).reqs...), held: compat.Append([]rheld(nil), rc.sum(f).held...)})
		w := rc.newWalker(f, false, prev)
		w.walk()
		stable := sameRsum(old, rc.sum(f)) && sameGotoStates(prev, w.gotoAt)
		prev = w.gotoAt
		if stable {
			break
		}
	}
	if report {
		w := rc.newWalker(f, true, prev)
		w.walk()
	}
}

func sameGotoStates(a, b map[string]*rstate) bool {
	if len(a) != len(b) {
		return false
	}
	for k, s := range a {
		t, ok := b[k]
		if !ok || !s.equal(t) {
			return false
		}
	}
	return true
}

func (rc *relCheck) newWalker(f *function, report bool, prev map[string]*rstate) *rwalker {
	w := pu.New(rwalker{rc: rc, a: rc.a, fn: f, sum: rc.sum(f), report: report, gotoAt: map[string]*rstate{}, prevAt: prev})
	if w.prevAt == nil {
		w.prevAt = map[string]*rstate{}
	}
	w.st = newRstate()
	for _, r := range w.sum.reqs {
		w.st.held = compat.Append(w.st.held, pu.New(rlock{names: compat.Append([]rname(nil), r.n), excl: r.excl}))
	}
	return w
}

func (w *rwalker) walk() {
	w.gotos = gotoTargets(w.fn.body)
	w.block(w.fn.body)
	w.finish()
}

// finish applies the deferred calls and records the locks left held.
func (w *rwalker) finish() {
	w.exit = rjoin(w.exit, w.st)
	w.st = w.exit
	if w.st.dead {
		return
	}
	pending := w.st.pending
	w.st.pending = nil
	for i := len(pending) - 1; i >= 0; i-- {
		d := pending[i]
		for _, r := range d.reqs {
			w.release(r.n, r.excl, d.pos, compat.Concat("the deferred call at ", w.a.short(d.pos), ": ", r.head), r.desc, true)
		}
		for _, h := range d.held {
			w.acquire(h.n, h.excl)
		}
	}
	var held []rheld
	for _, l := range w.st.held {
		if l.deferOnly {
			continue
		}
		for _, n := range l.names {
			if w.outer(n) {
				held = compat.Append(held, rheld{n: n, excl: l.excl})
				break
			}
		}
	}
	w.sum.held = held
}

func (w *rwalker) errorf(pos token.Pos, msg string) {
	if !w.report || w.quiet > 0 {
		return
	}
	w.a.add(compat.Sprintf("rel|%d", int(pos)), diag.New(w.a.pos(pos), diag.RaceReleaseNotHeld, "%s", pu.Box(msg)))
}

// outer reports whether a name can be held by the caller at entry: one
// that starts from a package variable, from what a pointer parameter
// points to, or from a capture of a literal.
func (w *rwalker) outer(n rname) bool {
	if isGlobal(n.v) {
		return true
	}
	for _, p := range w.fn.params {
		if p == n.v {
			return strings.HasPrefix(n.path, "*")
		}
	}
	if lit := w.fn.lit; lit != nil {
		return n.v.Pos() < lit.Pos() || n.v.Pos() >= lit.End()
	}
	return false
}

// acquire records that the lock n names is held.
func (w *rwalker) acquire(n rname, excl bool) {
	if w.st.dead {
		return
	}
	if i := w.st.find(n); i >= 0 {
		l := w.st.held[i]
		if l.deferOnly {
			l.deferOnly = false
		}
		return
	}
	w.st.held = compat.Append(w.st.held, pu.New(rlock{names: compat.Append([]rname(nil), n), excl: excl}))
	delete(w.st.why, n)
}

// release checks a release of the lock n names, in write mode if excl, at
// pos. head says what releases it ("mu.Unlock() releases mu"), and desc
// is that release for callers' messages. A deferred release at the exit
// may find a lock held only for it.
func (w *rwalker) release(n rname, excl bool, pos token.Pos, head, desc string, deferred bool) {
	if w.st.dead {
		return
	}
	i := w.st.find(n)
	if i >= 0 {
		l := w.st.held[i]
		switch {
		case l.deferOnly && !deferred:
			w.errorf(pos, compat.Concat(head, ", which is not held here: it is not held on every path to here"))
		case l.excl && !excl:
			w.errorf(pos, compat.Concat(head, " in read mode, but it is held here in write mode: release it with Unlock"))
		case !l.excl && excl:
			w.errorf(pos, compat.Concat(head, " in write mode, but it is held here only in read mode: release it with RUnlock"))
		}
		if !deferred {
			for _, d := range w.st.pending {
				for _, r := range d.reqs {
					if l.has(r.n) {
						w.errorf(pos, compat.Concat(head, ", but the deferred call at ", w.a.short(d.pos), " releases it at the end of the function, so it must stay held from the defer statement on"))
					}
				}
			}
		}
		w.st.drop(i)
		for _, m := range l.names {
			w.st.why[m] = compat.Concat("it was released at ", w.a.short(pos))
		}
		return
	}
	why, hasWhy := w.st.why[n]
	if !hasWhy && w.outer(n) {
		if j := w.sum.req(n); j < 0 {
			w.sum.reqs = compat.Append(w.sum.reqs, rreq{n: n, excl: excl, pos: pos, head: head, desc: desc})
		} else if w.sum.reqs[j].excl != excl {
			w.errorf(pos, compat.Concat(head, " in a different mode from the release at ", w.a.short(w.sum.reqs[j].pos), ", so the caller cannot hold it in both"))
		}
		w.st.why[n] = compat.Concat("it was released at ", w.a.short(pos))
		return
	}
	if !hasWhy {
		why = "nothing takes it on the way here"
	}
	w.errorf(pos, compat.Concat(head, ", which is not held here: ", why))
}

// need checks that the lock n names is held at a defer statement, without
// releasing it.
func (w *rwalker) need(n rname, excl bool, pos token.Pos, head string) {
	if w.st.dead {
		return
	}
	if i := w.st.find(n); i >= 0 {
		l := w.st.held[i]
		if l.excl != excl {
			w.errorf(pos, compat.Concat(head, " at the end of the function in a mode other than the one it is held in here"))
		}
		return
	}
	why, hasWhy := w.st.why[n]
	if !hasWhy && w.outer(n) {
		if j := w.sum.req(n); j < 0 {
			w.sum.reqs = compat.Append(w.sum.reqs, rreq{n: n, excl: excl, pos: pos, head: head, desc: compat.Concat(head, " at ", w.a.short(pos))})
		}
		w.st.held = compat.Append(w.st.held, pu.New(rlock{names: compat.Append([]rname(nil), n), excl: excl}))
		return
	}
	if !hasWhy {
		why = "nothing takes it on the way here"
	}
	w.errorf(pos, compat.Concat(head, " at the end of the function, so it must be held at the defer statement and at every point after it, but it is not held here: ", why))
}

// ----------------------------------------------------------------------------
// Names

// nameOf returns the name of the memory e denotes.
func (w *rwalker) nameOf(e ast.Expr) (rname, bool) {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		v, ok := w.a.objOf(e).(*types2.Var)
		if !ok {
			return rname{}, false
		}
		return rname{v: v}, true
	case *ast.SelectorExpr:
		sel := w.a.selections[e]
		if sel == nil {
			if v, ok := w.a.uses[e.Sel].(*types2.Var); ok {
				return rname{v: v}, true
			}
			return rname{}, false
		}
		if sel.Kind() != types2.FieldVal {
			return rname{}, false
		}
		return w.fieldName(e.X, sel.Index())
	case *ast.IndexExpr:
		tv := w.a.types[e.Index]
		if tv.Value.IsNil() {
			return rname{}, false
		}
		k, exact := constant.Int64Val(constant.ToInt(tv.Value))
		if !exact {
			return rname{}, false
		}
		var n rname
		ok := false
		switch u := underlying(w.a.typeOf(e.X)).(type) {
		case *types2.Array:
			n, ok = w.nameOf(e.X)
		case *types2.Pointer:
			if _, isArr := underlying(u.Elem()).(*types2.Array); isArr {
				n, ok = w.placeOfPtr(e.X)
			}
		}
		if !ok {
			return rname{}, false
		}
		n.path = compat.Concat(n.path, "[", itoa(int(k)), "]")
		return n, true
	case *ast.StarExpr:
		return w.placeOfPtr(e.X)
	}
	return rname{}, false
}

// placeOfPtr returns the name of what the pointer e points to.
func (w *rwalker) placeOfPtr(e ast.Expr) (rname, bool) {
	switch e := ast.Unparen(e).(type) {
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return w.nameOf(e.X)
		}
	case *ast.Ident:
		v, ok := w.a.objOf(e).(*types2.Var)
		if !ok || isGlobal(v) || !ptrLike(v.Type()) || w.rc.bad[v] {
			return rname{}, false
		}
		if n, ok := w.st.alias[v]; ok {
			return n, true
		}
		return rname{v: v, path: "*"}, true
	}
	return rname{}, false
}

// fieldName returns the name of the field that path selects from x.
func (w *rwalker) fieldName(x ast.Expr, path []int) (rname, bool) {
	T := w.a.typeOf(x)
	var n rname
	ok := false
	switch {
	case ptrLike(T):
		n, ok = w.placeOfPtr(x)
		T = pointerElem(T)
	case isOwnedBox(T):
		n, ok = w.nameOf(x)
		n.path = compat.Concat(n.path, "@")
		T = pointerElem(T)
	default:
		n, ok = w.nameOf(x)
	}
	if !ok || T == nil {
		return rname{}, false
	}
	for i, idx := range path {
		f := fieldOf(T, idx)
		if f == nil {
			return rname{}, false
		}
		n.path = compat.Concat(n.path, ".", f.Name())
		T = f.Type()
		if i < len(path)-1 {
			if ptrLike(T) {
				return rname{}, false // an embedded pointer, loaded from memory
			}
			if isOwnedBox(T) {
				n.path = compat.Concat(n.path, "@")
				T = pointerElem(T)
			}
		}
	}
	return n, true
}

// recvName returns the name of the memory the receiver of the method
// call se points to (or is, for a value receiver).
func (w *rwalker) recvName(se *ast.SelectorExpr, sel *types2.Selection) (rname, bool) {
	path := sel.Index()
	if len(path) > 1 {
		F := embeddedType(w.a.typeOf(se.X), path[:len(path)-1])
		if ptrLike(F) {
			return rname{}, false // an embedded pointer, loaded from memory
		}
		n, ok := w.fieldName(se.X, path[:len(path)-1])
		if ok && isOwnedBox(F) {
			n.path = compat.Concat(n.path, "@")
		}
		return n, ok
	}
	X := w.a.typeOf(se.X)
	switch {
	case ptrLike(X):
		return w.placeOfPtr(se.X)
	case isOwnedBox(X):
		n, ok := w.nameOf(se.X)
		n.path = compat.Concat(n.path, "@")
		return n, ok
	}
	return w.nameOf(se.X)
}

// noName explains why the lock e reaches cannot be named.
func (w *rwalker) noName(e ast.Expr) string {
	text := exprText(e)
	why := ""
	ast.Inspect(e, func(n ast.Node) bool {
		if why != "" {
			return false
		}
		switch n := n.(type) {
		case *ast.IndexExpr:
			if tv := w.a.types[n.Index]; tv.Value.IsNil() {
				why = compat.Concat(exprText(n), " has a variable index, so the two calls may name different locks: take a pointer once, l := &", exprText(n), ", and use l for both")
			}
		case *ast.Ident:
			if v, ok := w.a.objOf(n).(*types2.Var); ok && ptrLike(v.Type()) {
				if isGlobal(v) {
					why = compat.Concat(n.Name, " is a package variable that holds a pointer, which another party may change between the two calls: copy it into a local first")
				} else if w.rc.bad[v] {
					why = compat.Concat(n.Name, " is a pointer local whose address is taken, or that a function literal assigns or a scoped task captures, so it names no lock")
				}
			}
		}
		return true
	})
	if why == "" {
		why = compat.Concat(text, " loads a pointer from memory, which another party may change between the two calls: copy the pointer into a local first")
	}
	return compat.Concat("the compiler cannot name the lock: ", why)
}

// ----------------------------------------------------------------------------
// Statements

func (w *rwalker) block(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	for _, st := range b.List {
		w.stmt(st, "")
	}
}

func (w *rwalker) stmt(st ast.Stmt, label string) {
	switch st := st.(type) {
	case nil, *ast.EmptyStmt, *ast.BadStmt:
	case *ast.LabeledStmt:
		if w.gotos[st.Label.Name] {
			w.st = rjoin(rjoin(w.st, w.prevAt[st.Label.Name]), w.gotoAt[st.Label.Name])
		}
		w.stmt(st.Stmt, st.Label.Name)
	case *ast.BranchStmt:
		w.branch(st)
	case *ast.ExprStmt:
		w.expr(st.X)
	case *ast.DeclStmt:
		gd, ok := st.Decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			return
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for _, v := range vs.Values {
				w.expr(v)
			}
			for i, name := range vs.Names {
				var rhs ast.Expr
				if len(vs.Values) == len(vs.Names) {
					rhs = vs.Values[i]
				}
				w.assign(name, rhs, st.Pos())
			}
		}
	case *ast.AssignStmt:
		for _, r := range st.Rhs {
			w.expr(r)
		}
		for i, l := range st.Lhs {
			w.lhsExpr(l)
			var rhs ast.Expr
			if (st.Tok == token.ASSIGN || st.Tok == token.DEFINE) && len(st.Lhs) == len(st.Rhs) {
				rhs = st.Rhs[i]
			}
			w.assign(l, rhs, st.Pos())
		}
	case *ast.OrStmt:
		w.expr(st.Call)
		after := w.st.clone()
		w.stmt(st.Clause, "")
		w.st = after
		for _, l := range st.Lhs {
			w.lhsExpr(l)
			w.assign(l, nil, st.Pos())
		}
	case *ast.IncDecStmt:
		w.expr(st.X)
	case *ast.BlockStmt:
		w.block(st)
	case *ast.IfStmt:
		w.stmt(st.Init, "")
		t, f := w.cond(st.Cond)
		w.st = t
		w.block(st.Body)
		after := w.st
		w.st = f
		w.stmt(st.Else, "")
		w.st = rjoin(after, w.st)
	case *ast.ForStmt:
		w.stmt(st.Init, "")
		w.loop(label, func() *rstate {
			if st.Cond == nil {
				return deadRstate()
			}
			t, f := w.cond(st.Cond)
			w.st = t
			return f
		}, func() { w.block(st.Body) }, func() { w.stmt(st.Post, "") })
	case *ast.RangeStmt:
		w.rangeStmt(st, label)
	case *ast.SwitchStmt:
		w.stmt(st.Init, "")
		w.expr(st.Tag)
		w.clauses(label, st.Body.List, true, func(c ast.Stmt) []ast.Stmt {
			cc := c.(*ast.CaseClause)
			for _, e := range cc.List {
				w.expr(e)
			}
			return cc.Body
		})
	case *ast.TypeSwitchStmt:
		w.stmt(st.Init, "")
		switch as := st.Assign.(type) {
		case *ast.AssignStmt:
			w.expr(as.Rhs[0])
		case *ast.ExprStmt:
			w.expr(as.X)
		}
		w.clauses(label, st.Body.List, true, func(c ast.Stmt) []ast.Stmt {
			return c.(*ast.CaseClause).Body
		})
	case *ast.SelectStmt:
		w.clauses(label, st.Body.List, false, func(c ast.Stmt) []ast.Stmt {
			cc := c.(*ast.CommClause)
			w.stmt(cc.Comm, "")
			return cc.Body
		})
	case *ast.ReturnStmt:
		for _, r := range st.Results {
			w.expr(r)
		}
		w.exit = rjoin(w.exit, w.st)
		w.st = deadRstate()
	case *ast.DeferStmt:
		w.deferStmt(st)
	case *ast.TaskStmt:
		// The arguments are evaluated here; the task starts with nothing
		// held (scan).
		if se, ok := ast.Unparen(st.Call.Fun).(*ast.SelectorExpr); ok {
			if sel := w.a.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
				w.expr(se.X)
			}
		}
		for _, arg := range st.Call.Args {
			w.expr(arg)
		}
	case *ast.SendStmt:
		w.expr(st.Chan)
		w.expr(st.Value)
	}
}

func (w *rwalker) branch(st *ast.BranchStmt) {
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
				t.conts = rjoin(t.conts, w.st)
			} else {
				t.breaks = rjoin(t.breaks, w.st)
			}
			break
		}
		w.st = deadRstate()
	case token.GOTO:
		name := st.Label.Name
		w.gotoAt[name] = rjoin(w.gotoAt[name], w.st)
		w.st = deadRstate()
	}
}

// loop walks a loop as the walker of the summaries does (walker.loop).
func (w *rwalker) loop(label string, cond func() *rstate, body, post func()) {
	entry := w.st
	try := func(record bool) (exit *rstate, back *rstate, t *rtarget) {
		if !record {
			w.quiet++
			defer func() { w.quiet-- }()
		}
		w.st = entry.clone()
		exit = cond()
		t = pu.New(rtarget{label: label, loop: true})
		w.targets = compat.Append(w.targets, t)
		body()
		w.targets = w.targets[:len(w.targets)-1]
		w.st = rjoin(w.st, t.conts)
		post()
		return exit, w.st, t
	}
	for i := 0; i < 6 && !entry.dead; i++ {
		_, back, _ := try(false)
		next := rjoin(entry, back)
		if next.equal(entry) {
			break
		}
		entry = next
	}
	exit, _, t := try(true)
	w.st = rjoin(exit, t.breaks)
}

// rangeStmt walks a range statement. The body of a range over a function
// counts as part of the function, run as often as the iterator likes.
func (w *rwalker) rangeStmt(st *ast.RangeStmt, label string) {
	w.expr(st.X)
	w.loop(label, func() *rstate {
		exit := w.st.clone()
		for _, e := range []ast.Expr{st.Key, st.Value} {
			if e == nil {
				continue
			}
			if st.Tok == token.ASSIGN {
				w.lhsExpr(e)
			}
			w.assign(e, nil, st.Pos())
		}
		return exit
	}, func() { w.block(st.Body) }, func() {})
}

// clauses walks the clauses of a switch or select (walker.clauses).
func (w *rwalker) clauses(label string, list []ast.Stmt, isSwitch bool, walk func(ast.Stmt) []ast.Stmt) {
	entry := w.st
	t := pu.New(rtarget{label: label})
	w.targets = compat.Append(w.targets, t)
	var out *rstate
	hasDefault := false
	var carry *rstate
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
		w.st = rjoin(entry.clone(), carry)
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
		out = rjoin(out, w.st)
	}
	w.targets = w.targets[:len(w.targets)-1]
	if isSwitch && !hasDefault {
		out = rjoin(out, entry)
	}
	if out == nil {
		out = deadRstate()
	}
	w.st = rjoin(out, t.breaks)
}

// cond walks a condition and returns the states where it is true and
// false: a TryLock, or a bool variable that holds the result of one,
// holds the lock where it succeeded.
func (w *rwalker) cond(e ast.Expr) (t, f *rstate) {
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
	var tr rtry
	found := false
	switch x := x.(type) {
	case *ast.CallExpr:
		if op, se, _ := w.a.relOp(x); op == "trylock" || op == "tryrlock" {
			w.expr(se.X)
			if n, ok := w.recvName(se, w.a.selections[se]); ok {
				tr, found = rtry{n: n, excl: op == "trylock"}, true
			}
		}
	case *ast.Ident:
		if v, ok := w.a.objOf(x).(*types2.Var); ok {
			tr, found = w.st.tries[v]
		}
	}
	if !found {
		w.expr(e)
		return w.st.clone(), w.st.clone()
	}
	held := w.st.clone()
	saved := w.st
	w.st = held
	w.acquire(tr.n, tr.excl)
	w.st = saved
	if neg {
		return w.st.clone(), held
	}
	return held, w.st.clone()
}

func (w *rwalker) deferStmt(st *ast.DeferStmt) {
	c := st.Call
	d := pu.New(rdefer{pos: st.Pos()})
	if op, se, tp := w.a.relOp(c); op != "" {
		w.expr(se.X)
		switch op {
		case "unlock", "runlock":
			if n, ok := w.lockName(c, se, tp); ok {
				head := compat.Concat(exprText(c.Fun), "() releases ", n.text())
				d.reqs = compat.Append(d.reqs, rreq{n: n, excl: op == "unlock", pos: c.Pos(), head: head, desc: compat.Concat(exprText(c.Fun), "() at ", w.a.short(c.Pos()))})
			}
		case "lock", "rlock":
			if n, ok := w.recvName(se, w.a.selections[se]); ok {
				d.held = compat.Append(d.held, rheld{n: n, excl: op == "lock"})
			}
		}
	} else if se, ok := w.a.condWait(c); ok {
		w.expr(se.X)
		w.expr(c.Args[0])
		if n, ok := w.lockerName(c, c.Args[0]); ok {
			head := compat.Concat(exprText(c.Fun), "(", exprText(c.Args[0]), ") releases ", n.text())
			d.reqs = compat.Append(d.reqs, rreq{n: n, excl: true, pos: c.Pos(), head: head, desc: compat.Concat(exprText(c.Fun), "() at ", w.a.short(c.Pos()))})
			d.held = compat.Append(d.held, rheld{n: n, excl: true})
		}
	} else {
		w.callArgs(c)
		g, se, sel, _ := w.a.staticTarget(c)
		if g != nil {
			if s := w.rc.sums[g]; s != nil {
				bind := w.binder(c, g, se, sel)
				for _, r := range s.reqs {
					m, ok := w.mapName(g, r.n, bind)
					if !ok {
						w.errorf(c.Pos(), compat.Concat("the call of ", g.name, " releases a lock (by ", r.desc, ") that this call does not name: ", w.noName(c)))
						continue
					}
					d.reqs = compat.Append(d.reqs, rreq{n: m, excl: r.excl, pos: c.Pos(), head: compat.Concat("the call of ", g.name, " releases ", m.text(), " (by ", r.desc, ")"), desc: r.desc})
				}
				for _, h := range s.held {
					if m, ok := w.mapName(g, h.n, bind); ok {
						d.held = compat.Append(d.held, rheld{n: m, excl: h.excl})
					}
				}
			}
		}
	}
	if len(d.reqs) == 0 && len(d.held) == 0 {
		return
	}
	for _, r := range d.reqs {
		w.need(r.n, r.excl, st.Pos(), compat.Concat("defer ", r.head))
	}
	if !w.st.hasPending(d.pos) {
		w.st.pending = compat.Append(w.st.pending, d)
	}
}

// lockName returns the name of the lock a release call releases, and
// reports the error if it has none.
func (w *rwalker) lockName(c *ast.CallExpr, se *ast.SelectorExpr, tp bool) (rname, bool) {
	sel := w.a.selections[se]
	if tp {
		T := sel.Recv()
		if p, ok := T.(*types2.Pointer); ok {
			T = p.Elem()
		}
		if tpar, ok := types2.Unalias(T).(*types2.TypeParam); ok {
			for _, arg := range w.a.tpArgs[tpar] {
				if isInterface(arg) {
					w.errorf(c.Pos(), compat.Concat(exprText(c.Fun), "() releases a lock through the type parameter ", tpar.Obj().Name(), ", which is instantiated with the interface ", types2.TypeString(arg, nil), ", so the compiler cannot name the lock or show it held: instantiate it with the lock's own type, such as *sync.Mutex"))
					return rname{}, false
				}
			}
		}
	}
	n, ok := w.recvName(se, sel)
	if !ok {
		w.errorf(c.Pos(), compat.Concat(exprText(c.Fun), "() releases a lock that cannot be shown held: ", w.noName(se.X)))
	}
	return n, ok
}

// ----------------------------------------------------------------------------
// Expressions and calls

// expr walks the calls in e in order.
func (w *rwalker) expr(e ast.Expr) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			w.call(n)
			return false
		}
		return true
	})
}

// lhsExpr walks the calls in the left side of an assignment.
func (w *rwalker) lhsExpr(e ast.Expr) {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
	case *ast.SelectorExpr:
		w.lhsExpr(e.X)
	case *ast.IndexExpr:
		w.lhsExpr(e.X)
		w.expr(e.Index)
	default:
		w.expr(e)
	}
}

// callArgs walks the receiver and the arguments of a call.
func (w *rwalker) callArgs(c *ast.CallExpr) {
	fun := ast.Unparen(c.Fun)
	switch f := fun.(type) {
	case *ast.FuncLit, *ast.Ident:
	case *ast.SelectorExpr:
		if sel := w.a.selections[f]; sel != nil {
			w.expr(f.X)
		}
	default:
		w.expr(fun)
	}
	for _, arg := range c.Args {
		w.expr(arg)
	}
}

func (w *rwalker) call(c *ast.CallExpr) {
	if w.st.dead {
		return
	}
	tv := w.a.types[c.Fun]
	if tv.IsType() || tv.IsBuiltin() {
		for _, arg := range c.Args {
			w.expr(arg)
		}
		if w.rc.callNever(c) {
			w.st = deadRstate()
		}
		return
	}
	if op, se, tp := w.a.relOp(c); op != "" {
		w.expr(se.X)
		switch op {
		case "lock", "rlock":
			if n, ok := w.recvName(se, w.a.selections[se]); ok {
				w.acquire(n, op == "lock")
			}
		case "unlock", "runlock":
			if n, ok := w.lockName(c, se, tp); ok {
				w.release(n, op == "unlock", c.Pos(), compat.Concat(exprText(c.Fun), "() releases ", n.text()), compat.Concat(exprText(c.Fun), "() at ", w.a.short(c.Pos())), false)
			}
		}
		return
	}
	if se, ok := w.a.condWait(c); ok {
		w.condWait(c, se)
		return
	}
	w.callArgs(c)
	g, se, sel, _ := w.a.staticTarget(c)
	if g == nil {
		return
	}
	if s := w.rc.sums[g]; s != nil && (len(s.reqs) > 0 || len(s.held) > 0) {
		bind := w.binder(c, g, se, sel)
		for _, r := range s.reqs {
			m, ok := w.mapName(g, r.n, bind)
			if !ok {
				w.errorf(c.Pos(), compat.Concat("the call of ", g.name, " releases a lock (by ", r.desc, ") that this call does not name: ", w.noName(c)))
				continue
			}
			w.release(m, r.excl, c.Pos(), compat.Concat("the call of ", g.name, " releases ", m.text(), " (by ", r.desc, ")"), r.desc, false)
		}
		for _, h := range s.held {
			if m, ok := w.mapName(g, h.n, bind); ok {
				w.acquire(m, h.excl)
			}
		}
	}
	if w.rc.neverReturns(g) {
		w.st = deadRstate()
	}
}

// condWait walks c.Wait(l) of a sync.Cond, which counts as l.Unlock()
// followed by l.Lock(): l must be held in write mode, and it is held
// again after the call.
func (w *rwalker) condWait(c *ast.CallExpr, se *ast.SelectorExpr) {
	w.expr(se.X)
	arg := c.Args[0]
	w.expr(arg)
	if w.st.dead {
		return
	}
	n, ok := w.lockerName(c, arg)
	if !ok {
		return
	}
	head := compat.Concat(exprText(c.Fun), "(", exprText(arg), ") releases ", n.text())
	if i := w.st.find(n); i >= 0 && !w.st.held[i].excl {
		w.errorf(c.Pos(), compat.Concat(head, " in write mode, but it is held here only in read mode: a Cond's Wait needs the lock held with Lock"))
		return
	}
	// A pending deferred release still finds the lock held: Wait takes
	// it again before it returns.
	pending := w.st.pending
	w.st.pending = nil
	w.release(n, true, c.Pos(), head, compat.Concat(exprText(c.Fun), "() at ", w.a.short(c.Pos())), false)
	w.st.pending = pending
	w.acquire(n, true)
}

// lockerName returns the name of the lock that the Locker argument e of
// a Cond's Wait reaches.
func (w *rwalker) lockerName(c *ast.CallExpr, e ast.Expr) (rname, bool) {
	T := w.a.typeOf(e)
	if tpar, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		for _, arg := range w.a.tpArgs[tpar] {
			if isInterface(arg) {
				w.errorf(c.Pos(), compat.Concat(exprText(c.Fun), "(", exprText(e), ") releases a lock through the type parameter ", tpar.Obj().Name(), ", which is instantiated with the interface ", types2.TypeString(arg, nil), ", so the compiler cannot name the lock or show it held: instantiate it with the lock's own type, such as *sync.Mutex"))
				return rname{}, false
			}
		}
		if n, ok := w.nameOf(e); ok {
			return n, true
		}
	} else if isInterface(T) {
		w.errorf(c.Pos(), compat.Concat(exprText(c.Fun), "(", exprText(e), ") releases the lock in a ", types2.TypeString(T, nil), " value, so the compiler cannot name the lock or show it held: pass the lock's own pointer, such as &mu"))
		return rname{}, false
	} else if ptrLike(T) {
		if n, ok := w.placeOfPtr(e); ok {
			return n, true
		}
	} else if n, ok := w.nameOf(e); ok {
		return n, true
	}
	w.errorf(c.Pos(), compat.Concat(exprText(c.Fun), "(", exprText(e), ") releases a lock that cannot be shown held: ", w.noName(e)))
	return rname{}, false
}

// binder returns the names of what the pointer parameters of g point to
// at call c, by parameter number (receiver first).
func (w *rwalker) binder(c *ast.CallExpr, g *function, se *ast.SelectorExpr, sel *types2.Selection) func(i int) (rname, bool) {
	return func(i int) (rname, bool) {
		if se != nil && g.lit == nil && g.sig.Recv() != nil {
			if i == 0 {
				return w.recvName(se, sel)
			}
			i--
		}
		if i >= len(c.Args) {
			return rname{}, false
		}
		return w.placeOfPtr(c.Args[i])
	}
}

// mapName maps a name of callee g's summary to the caller's names.
func (w *rwalker) mapName(g *function, n rname, bind func(i int) (rname, bool)) (rname, bool) {
	if isGlobal(n.v) {
		return n, true
	}
	for i, p := range g.params {
		if p != n.v {
			continue
		}
		if !strings.HasPrefix(n.path, "*") {
			return rname{}, false
		}
		base, ok := bind(i)
		if !ok {
			return rname{}, false
		}
		return rname{v: base.v, path: compat.Concat(base.path, strings.SliceFrom(n.path, 1))}, true
	}
	if g.lit == nil {
		return rname{}, false
	}
	// A capture: the caller's own name, through its aliases.
	if strings.HasPrefix(n.path, "*") {
		if base, ok := w.st.alias[n.v]; ok {
			return rname{v: base.v, path: compat.Concat(base.path, strings.SliceFrom(n.path, 1))}, true
		}
	}
	return n, true
}

// ----------------------------------------------------------------------------
// Assignments

// assign records an assignment of rhs (nil if unknown) to lhs: the names
// that start from what lhs held end, and a pointer local gets the name of
// what it now points to.
func (w *rwalker) assign(lhs ast.Expr, rhs ast.Expr, pos token.Pos) {
	if w.st.dead {
		return
	}
	lhs = ast.Unparen(lhs)
	if id, ok := lhs.(*ast.Ident); ok {
		if id.Name == "_" {
			return
		}
		v, ok := w.a.objOf(id).(*types2.Var)
		if !ok {
			return
		}
		w.assignVar(v, rhs, pos)
		return
	}
	n, ok := w.nameOf(lhs)
	if !ok {
		return
	}
	w.end(func(m rname) bool { return m.under(n) }, compat.Concat("it was given a new value at ", w.a.short(pos)))
}

// end ends the held names that match.
func (w *rwalker) end(match func(rname) bool, why string) {
	for i := 0; i < len(w.st.held); {
		l := w.st.held[i]
		var keep []rname
		for _, m := range l.names {
			if match(m) {
				w.st.why[m] = why
				for _, d := range w.st.pending {
					for _, r := range d.reqs {
						if r.n == m {
							w.errorf(d.pos, compat.Concat("defer ", r.head, " at the end of the function, but the name ends at ", strings.TrimPrefix(why, "it was given a new value at "), " before then"))
						}
					}
				}
			} else {
				keep = compat.Append(keep, m)
			}
		}
		if len(keep) == 0 {
			w.st.drop(i)
			continue
		}
		l.names = keep
		i++
	}
}

func (w *rwalker) assignVar(v *types2.Var, rhs ast.Expr, pos token.Pos) {
	// What the new value names, worked out before the old names end.
	var alias rname
	hasAlias := false
	var copied []rname // held names that the copy carries
	var try rtry
	hasTry := false
	if rhs != nil {
		r := ast.Unparen(rhs)
		if c, ok := r.(*ast.CallExpr); ok {
			if op, se, _ := w.a.relOp(c); op == "trylock" || op == "tryrlock" {
				if n, ok := w.recvName(se, w.a.selections[se]); ok {
					try, hasTry = rtry{n: n, excl: op == "trylock"}, true
				}
			}
		}
		if ptrLike(v.Type()) && !w.rc.bad[v] && !isGlobal(v) {
			if n, ok := w.placeOfPtr(r); ok {
				alias, hasAlias = n, true
				if id, isID := r.(*ast.Ident); isID {
					if u, ok := w.a.objOf(id).(*types2.Var); ok {
						for _, l := range w.st.held {
							for _, m := range l.names {
								if m.v == u && strings.HasPrefix(m.path, "*") {
									copied = compat.Append(copied, m)
								}
							}
						}
					}
				}
			}
		}
	}
	delete(w.st.tries, v)
	why := compat.Concat("the name ends where ", v.Name(), " is assigned at ", w.a.short(pos))
	if ptrLike(v.Type()) {
		// Pointer locals that point into what v points to keep their
		// locks under their own names.
		for x, base := range w.st.alias {
			if base.v != v {
				continue
			}
			for _, l := range w.st.held {
				for _, m := range l.names {
					if m.under(base) {
						nm := rname{v: x, path: compat.Concat("*", strings.SliceFrom(m.path, len(base.path)))}
						if !l.has(nm) {
							l.names = compat.Append(l.names, nm)
						}
					}
				}
			}
			delete(w.st.alias, x)
		}
	}
	w.end(func(m rname) bool { return m.v == v }, why)
	delete(w.st.alias, v)
	if hasAlias {
		w.st.alias[v] = alias
		for _, m := range copied {
			if i := w.st.find(m); i >= 0 {
				nm := rname{v: v, path: m.path}
				if !w.st.held[i].has(nm) {
					w.st.held[i].names = compat.Append(w.st.held[i].names, nm)
				}
			}
		}
	}
	if hasTry {
		w.st.tries[v] = try
	}
}
