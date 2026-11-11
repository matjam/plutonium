// Translated by go2pu from internal/race/check.go at the go-cutover tag; edited by hand since.

package race

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/load"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	typerr "compiler/internal/types2/errors"
	"compiler/pu"
	"pluto/os"
	"pluto/sort"
	"pluto/strings"
)

// maxGroups bounds the tasks followed from the program's start.
const maxGroups = 2000

// A group is one task of the program, or one task statement's tasks: the
// accesses they make, with the locks held relative to the task's start.
type group struct {
	main     bool
	multi    bool // several tasks may run this at once
	handler  bool // an interrupt handler the program installs
	pos      token.Pos
	desc     string
	accesses []*access
	cpu      []*cpuEntry // its uses of per-CPU variables, which section 20.9 checks
}

func (g *group) name(a *analyzer) string {
	if g.main {
		return "the main task"
	}
	if g.handler {
		return compat.Sprintfb("the interrupt handler %s installed at %s", compat.ArgS(strings.TrimPrefix(g.desc, "handler ")), compat.ArgS(a.short(g.pos)))
	}
	return compat.Sprintfb("the task started by %s at %s", compat.ArgS(g.desc), compat.ArgS(a.short(g.pos)))
}

// check runs the three rules over the whole program.
func (a *analyzer) check() {
	a.finishWrites()
	prog := a.program.sum
	mainG := pu.New(group{main: true})
	var cpuUses []*cpuEntry
	a.addAllocators(prog, &state{pub: map[ast.Node]bool{}, spawned: true})
	for _, x := range a.expandAll(prog) {
		switch {
		case x.cpu != nil:
			e := pu.New(cpuEntry{x, mainG})
			mainG.cpu = compat.Append(mainG.cpu, e)
			cpuUses = compat.Append(cpuUses, e)
		case !x.pre:
			mainG.accesses = compat.Append(mainG.accesses, x)
		}
	}
	groups := []*group{mainG}
	cross := compat.Append([]*crossing(nil), prog.cross...)

	// Follow every task the program may start, and the tasks they start.
	queue := compat.Append([]*spawn(nil), prog.spawns...)
	seen := map[string]bool{}
	tw := newWalker(a, a.program)
	// A function marked //pu:export may be called by C code at any time,
	// before any Do there has returned: it is an entry of a party, with
	// nothing held (section 19.3, Lazy initialization). Its accesses are
	// counted with the main task's.
	for _, f := range a.exportedFuncs() {
		tw.sum = pu.New(summary{})
		base := &state{pub: map[ast.Node]bool{}, spawned: true}
		tw.apply(f, compat.Make[origins](len(f.params)), f.decl.Pos(), f.name, base, nil)
		for _, x := range a.expandAll(tw.sum) {
			if x.cpu == nil {
				mainG.accesses = compat.Append(mainG.accesses, x)
			}
		}
		cross = compat.Append(cross, tw.sum.cross...)
		queue = compat.Append(queue, tw.sum.spawns...)
		// A view the caller passes points to memory the compiler cannot
		// see: C code may pass the same object to calls on several
		// threads, or one other tasks use, so it crosses to the call and
		// its type decides (section 19.3, Crossings; Whatever the check
		// cannot follow).
		for _, prm := range f.params {
			if prm == nil {
				continue
			}
			name := prm.Name()
			if name == "" || name == "_" {
				name = "unnamed"
			}
			cross = compat.Append(cross, pu.New(crossing{pos: prm.Pos(), what: compat.Concat("the parameter ", name, " of the exported function ", f.name), typ: prm.Type(), fn: f.name, export: true, name: name}))
		}
	}
	for len(queue) > 0 && len(groups) < maxGroups {
		sp := queue[0]
		queue = queue[1:]
		for _, r := range sp.root {
			f := a.rootFunc(r)
			if f == nil {
				continue
			}
			k := compat.Concat(sp.key(), "|", r.vp.key())
			if seen[k] {
				continue
			}
			seen[k] = true
			tw.sum = pu.New(summary{})
			base := &state{pub: map[ast.Node]bool{}, spawned: true}
			if sp.handler && !sp.trap && a.cpuOn {
				base.locks = handlerRegions(f.decl.Pos())
			}
			tw.apply(f, sp.args, sp.pos, f.name, base, nil)
			a.addAllocators(tw.sum, base)
			pos, desc := a.userStart(sp)
			g := pu.New(group{multi: sp.multi, handler: sp.handler, pos: pos, desc: desc})
			for _, x := range a.expandAll(tw.sum) {
				if x.cpu != nil {
					e := pu.New(cpuEntry{x, g})
					g.cpu = compat.Append(g.cpu, e)
					cpuUses = compat.Append(cpuUses, e)
					continue
				}
				g.accesses = compat.Append(g.accesses, x)
			}
			groups = compat.Append(groups, g)
			cross = compat.Append(cross, tw.sum.cross...)
			queue = compat.Append(queue, tw.sum.spawns...)
		}
	}
	a.recordGlobalTypes(groups)
	written := a.writtenAfterInit(groups, false)
	a.notePlainWrites(groups)
	a.finishShared(groups)
	a.checkCrossings(cross, written, nil)
	a.guardedW = map[string]bool{}
	a.checkGlobals(groups)
	a.checkUnsendableGlobals(groups)
	loads := a.loadCrossings(groups)
	a.checkCrossings(loads, written, a.writtenAfterInit(groups, true))
	cross = compat.Append(cross, loads...)
	a.checkSynced(groups)
	a.checkMaps(groups, cross)
	sortedCPU(cpuUses)
	a.checkPerCPU(cpuUses, groups)
}

// expandAll returns the accesses of a task whose summary is s: its own,
// and those of every function it calls, each with the locks the task holds
// there. Each function is followed once per context (its arguments, the
// locks held at the call and whether a task has started).
func (a *analyzer) expandAll(s *summary) []*access {
	x := &expander{a: a, seen: map[expKey]bool{}, keys: map[accKey]bool{}}
	for _, acc := range s.accesses {
		x.emit(acc)
	}
	for _, e := range s.edges {
		x.expand(e.f, e.args, compose(nil, released{}, e.locks), e.pre)
	}
	if debug {
		perFn := map[*function]int{}
		for k := range x.seen {
			perFn[k.f]++
		}
		worst, n := "", 0
		for f, c := range perFn {
			if c > n {
				worst, n = f.name, c
			}
		}
		compat.Fprintfb(os.Stderr, "race: expanded %d contexts of %d functions, %d accesses; most: %s %d\n", compat.ArgI(int64(len(x.seen))), compat.ArgI(int64(len(perFn))), compat.ArgI(int64(len(x.out))), compat.ArgS(worst), compat.ArgI(int64(n)))
	}
	return x.out
}

// An expKey is a function followed in one context.
type expKey struct {
	f     *function
	locks string
	pre   bool
	args  string
}

type expander struct {
	a    *analyzer
	seen map[expKey]bool
	keys map[accKey]bool
	out  []*access
}

func (x *expander) emit(acc *access) {
	k := acc.key()
	if x.keys[k] {
		return
	}
	x.keys[k] = true
	x.out = compat.Append(x.out, acc)
}

// expand adds the accesses f makes of package variables and synced
// objects, called with env bound to its parameters, while the locks are
// held, then follows its own calls.
//
// The locks are the task's: every lock held at that point, so what the
// callers released no longer matters. The arguments matter only to name
// locks reached through parameters; a function whose accesses and calls
// name no such lock is followed once per set of locks held, whatever its
// arguments.
func (x *expander) expand(f *function, env []origins, locks lockset, pre bool) {
	if !x.a.hasShared(f) {
		return
	}
	needEnv := x.a.needsEnv(f)
	if !needEnv {
		env = nil
	} else {
		// Only the parameters that name locks, here or in the callees,
		// matter: the others are left out, so that calls that differ in
		// them share one expansion.
		used := x.a.envParams(f)
		trimmed := compat.Make[origins](len(env))
		for i := range env {
			// A lock named through a parameter is held only where the
			// argument is one place (substLocks), so an argument of many
			// places names no lock: it is left empty, which names none
			// either and releases every lock (substRel), the safe side.
			// Calls that differ only in such arguments then share one
			// expansion.
			if used.has(i) {
				if r := lockRoots(env[i]); len(r) == 1 {
					trimmed[i] = r
				}
			}
		}
		env = trimmed
	}
	locks = sharedLocks(locks)
	k := expKey{f: f, pre: pre}
	if len(locks) > 0 {
		k.locks = locks.String()
	}
	if env != nil {
		k.args = (&edge{args: env}).key()
	}
	if x.seen[k] {
		return
	}
	x.seen[k] = true
	if debug && len(x.seen)%20000 == 0 {
		a := k.args
		if len(a) > 1500 {
			a = strings.Slice(a, 0, 1500)
		}
		compat.Fprintfb(os.Stderr, "race: expanding: %d contexts, %d accesses; now %s locks %d bytes args %d bytes: %s\n", compat.ArgI(int64(len(x.seen))), compat.ArgI(int64(len(x.out))), compat.ArgS(f.name), compat.ArgI(int64(len(k.locks))), compat.ArgI(int64(len(k.args))), compat.ArgS(a))
	}
	s := f.sum
	for _, acc := range s.accesses {
		// What the parameters reach was instantiated where f was called.
		if acc.place.root.kind == rParam || !x.a.shareable(acc.place) {
			continue
		}
		na := pu.New(access{
			typ:    acc.typ,
			tkey:   acc.tkey,
			place:  acc.place,
			write:  acc.write,
			atomic: acc.atomic,
			locks:  compose(locks, substRel(acc.rel, env), substLocks(acc.locks, env)),
			pos:    acc.pos,
			pre:    pre && acc.pre,
			cpu:    acc.cpu,
			free:   acc.free,
			view:   acc.view,
			sync:   acc.sync,
		})
		if acc.hasValid {
			na.hasValid, na.vvalid = true, compose(locks, substRel(acc.rel, env), substLocks(acc.vvalid, env))
			na.vpre = pre && acc.vpre
		}
		x.emit(na)
	}
	for _, ce := range s.edges {
		var args []origins
		if needEnv {
			for _, a := range ce.args {
				var na origins
				for _, o := range a {
					for _, v := range substitute(o.vp, env) {
						na = origins(compat.Append(na, origin{vp: v, tag: o.tag}))
					}
				}
				args = compat.Append(args, na.uniq())
			}
		} else {
			args = ce.args
		}
		x.expand(ce.f, args, compose(locks, substRel(ce.rel, env), substLocks(ce.locks, env)), pre && ce.pre)
	}
}

// hasShared reports whether f, or a function it calls, uses a package
// variable or a synced object other than through its parameters.
func (a *analyzer) hasShared(f *function) bool {
	return a.reachProp(f, a.sharedMemo, func(f *function) bool {
		for _, acc := range f.sum.accesses {
			if acc.place.root.kind != rParam && a.shareable(acc.place) {
				return true
			}
		}
		return false
	})
}

// needsEnv reports whether f, or a function it calls, names a lock through
// a parameter where it uses shared data.
func (a *analyzer) needsEnv(f *function) bool {
	return a.reachProp(f, a.envMemo, func(f *function) bool {
		params := func(ls lockset, r released) bool {
			for _, l := range ls {
				if l.place.root.kind == rParam {
					return true
				}
			}
			for _, k := range r.keys {
				if k.root.kind == rParam {
					return true
				}
			}
			return r.all
		}
		for _, acc := range f.sum.accesses {
			if params(acc.locks, acc.rel) {
				return true
			}
		}
		for _, e := range f.sum.edges {
			if params(e.locks, e.rel) {
				return true
			}
		}
		return false
	})
}

// lockRoots keeps the origins through which a lock can guard what the
// checks compare: package variables and synced objects. A lock in other
// memory guards nothing a task shares, so which one it is does not matter.
func lockRoots(o origins) origins {
	var out origins
	for _, x := range o {
		if x.vp.root.kind == rGlobal || strings.Contains(x.vp.path, "$") {
			out = origins(compat.Append(out, x))
		}
	}
	return out
}

// sharedLocks keeps the locks of ls that lockRoots would.
func sharedLocks(ls lockset) lockset {
	var out lockset
	for _, l := range ls {
		if l.place.root.kind == rGlobal || l.place.root.kind == rRegion || strings.Contains(l.place.path, "$") {
			out = lockset(compat.Append(out, l))
		}
	}
	return out
}

// envParams returns the parameters of f that name locks where f, or a
// callee it passes them to, uses shared data.
func (a *analyzer) envParams(f *function) bits {
	if b, ok := a.envUsed[f]; ok {
		return b
	}
	var b bits
	a.envUsed[f] = b // a recursive call sees what is known so far
	mark := func(ls lockset, r released) {
		for _, l := range ls {
			if l.place.root.kind == rParam {
				b = b.with(l.place.root.idx)
			}
		}
		for _, k := range r.keys {
			if k.root.kind == rParam {
				b = b.with(k.root.idx)
			}
		}
	}
	for _, acc := range f.sum.accesses {
		mark(acc.locks, acc.rel)
	}
	for _, e := range f.sum.edges {
		mark(e.locks, e.rel)
		if !a.needsEnv(e.f) {
			continue
		}
		used := a.envParams(e.f)
		for i, arg := range e.args {
			if !used.has(i) {
				continue
			}
			for _, o := range arg {
				if o.vp.root.kind == rParam {
					b = b.with(o.vp.root.idx)
				}
			}
		}
	}
	a.envUsed[f] = b
	return b
}

// reachProp reports whether own holds of f or of a function its edges
// reach, memoised in memo.
func (a *analyzer) reachProp(f *function, memo map[*function]bool, own func(*function) bool) bool {
	if v, ok := memo[f]; ok {
		return v
	}
	// Mark the whole set reachable from f in one walk.
	var order []*function
	seen := map[*function]bool{f: true}
	work := []*function{f}
	found := false
	for len(work) > 0 {
		g := work[len(work)-1]
		work = work[:len(work)-1]
		if v, ok := memo[g]; ok {
			found = found || v
			continue
		}
		order = compat.Append(order, g)
		if own(g) {
			found = true
		}
		for _, e := range g.sum.edges {
			if !seen[e.f] {
				seen[e.f] = true
				work = compat.Append(work, e.f)
			}
		}
	}
	if !found {
		for _, g := range order {
			memo[g] = false
		}
	}
	memo[f] = found
	return found
}

// userStart names where a task starts in the program's own code: the go
// statement, or, for a task the standard library starts (WaitGroup.Task,
// time.AfterFunc), the call of the library function that leads to it.
func (a *analyzer) userStart(sp *spawn) (token.Pos, string) {
	if !a.inStd(sp.pos) {
		return sp.pos, sp.desc
	}
	for i := len(sp.via) - 1; i >= 0; i-- {
		if f := sp.via[i]; f.pos.IsValid() && !a.inStd(f.pos) {
			return f.pos, compat.Concat("a call of ", f.callee)
		}
	}
	return sp.pos, sp.desc
}

func (a *analyzer) inStd(p token.Pos) bool {
	return strings.HasPrefix(a.fset.Position(p).Filename, load.StdPrefix)
}

// exportedFuncs returns the functions of the program's own packages marked
// //pu:export, in source order.
func (a *analyzer) exportedFuncs() []*function {
	var out []*function
	for _, f := range a.funcs {
		if f.decl == nil || f.decl.Doc == nil || f.decl.Body == nil || a.inStd(f.decl.Pos()) || f.sig.TypeParams() != nil {
			continue
		}
		for _, c := range f.decl.Doc.List {
			if strings.HasPrefix(c.Text, "//pu:export") {
				out = compat.Append(out, f)
				break
			}
		}
	}
	sort.SortFunc(out, func(x, y *function) bool { return x.decl.Pos() < y.decl.Pos() })
	return out
}

// rootFunc returns the function a task runs, from its origin.
func (a *analyzer) rootFunc(o origin) *function {
	switch o.vp.root.kind {
	case rFunc:
		return a.funcs[o.vp.root.fn.Origin()]
	case rLit:
		if lit, ok := o.vp.root.node.(*ast.FuncLit); ok {
			return a.lits[lit]
		}
	}
	return nil
}

// writtenAfterInit returns a function that reports where a package
// variable's place is written once tasks may run, or NoPos.
//
// With unguarded set, the writes a guard set guards do not count: a view
// of them that a party loads is valid only while it holds a lock of the
// guard set, which its uses are checked for (section 19.3, Rule 2 case 4,
// Views loaded under a lock).
func (a *analyzer) writtenAfterInit(groups []*group, unguarded bool) func(vpath) token.Pos {
	type wsite struct {
		place vpath
		pos   token.Pos
	}
	byVar := map[*types2.Var][]wsite{}
	for _, g := range groups {
		for _, x := range g.accesses {
			if x.write && x.place.root.kind == rGlobal && !(unguarded && a.guardedW[writeKey(x)]) {
				byVar[x.place.root.v] = compat.Append(byVar[x.place.root.v], wsite{x.place, x.pos})
			}
		}
	}
	return func(p vpath) token.Pos {
		for _, s := range byVar[p.root.v] {
			if overlaps(s.place, p) {
				return s.pos
			}
		}
		return token.NoPos
	}
}

// A plainSite is a plain write of a package variable's place once tasks
// may run.
type plainSite struct {
	place vpath
	pos   token.Pos
}

// plainSites holds the plain writes of each package variable.
type plainSites map[*types2.Var][]plainSite

// notePlainWrites records, for plainWritten, the writes of package
// variables once tasks may run that are not atomic: the atomic accesses of
// pluto/sync calls and atomic operations do not count.
func (a *analyzer) notePlainWrites(groups []*group) {
	a.plainWrites = plainSites{}
	for _, g := range groups {
		for _, x := range g.accesses {
			if x.write && !x.atomic && x.place.root.kind == rGlobal {
				a.plainWrites[x.place.root.v] = compat.Append(a.plainWrites[x.place.root.v], plainSite{x.place, x.pos})
			}
		}
	}
}

// plainWritten reports where a plain write of a package variable once
// tasks may run overlaps place p, or NoPos.
func (a *analyzer) plainWritten(p vpath) token.Pos {
	for _, s := range a.plainWrites[p.root.v] {
		if overlaps(s.place, p) {
			return s.pos
		}
	}
	return token.NoPos
}

// checkCrossings applies the sendable rule to every value that crosses to
// another task.
//
// For a load, guarded is what written is for the writes a guard set does
// not guard: a view loaded from a package variable, of memory in it or
// that it owns, is judged by it, as each use of the view is an access of
// that memory, which the guard rule and the rule for views loaded under a
// lock check.
func (a *analyzer) checkCrossings(cross []*crossing, written, guarded func(vpath) token.Pos) {
	for _, c := range cross {
		part := "it"
		if c.expr != nil {
			part = exprText(c.expr)
		} else if c.name != "" {
			part = c.name
		}
		vals := a.resolveVals(c.vals)
		for _, o := range vals {
			if c.load && o.vp.root.kind == rGlobal && a.cpuVars[o.vp.root.v] {
				// The writes of a per-CPU variable are not among the
				// accesses written counts: its type decides.
				vals = nil
				break
			}
		}
		if c.load {
			// What a view loaded through a pointer, or from a synced
			// object or a map, points to is not known: its type decides.
			for _, o := range c.vals {
				if o.vp.root.kind != rGlobal || strings.ContainsAny(o.vp.path, "*%$#@") {
					vals = nil
				}
			}
		}
		wr := written
		if c.load && guarded != nil && len(vals) > 0 && sameOrigins(vals, c.vals) {
			wr = guarded
		}
		p := a.sendable(c.typ, vals, part, wr)
		if p == nil {
			continue
		}
		if p.shared != nil {
			a.add(compat.Sprintfb("send|%d", compat.ArgI(int64(c.pos))), a.sharedError(c, p))
			continue
		}
		a.add(compat.Sprintfb("send|%d", compat.ArgI(int64(c.pos))), a.sendError(c, p))
	}
}

// writeKey names a write by its position and place.
func writeKey(x *access) string {
	return compat.Concat(itoa(int(x.pos)), "|", x.place.key())
}

// An entry is an access by one group.
type entry struct {
	x *access
	g *group
}

// conflict reports whether two accesses by groups may happen at once with
// one of them writing, without both being atomic.
func conflict(p, q entry) bool {
	if !p.x.write && !q.x.write {
		return false
	}
	if p.x.atomic && q.x.atomic {
		return false
	}
	if p.g == q.g && !p.g.multi {
		return false
	}
	return overlaps(p.x.place, q.x.place)
}

// guards returns the locks that may guard what an access writes: the
// mutexes it holds for writing that are one object for the whole program
// (package variables, not through pointers), keyed by place, and "atomic"
// for an atomic operation. rel turns a lock within the same synced object
// into a key relative to it.
func guards(x *access, excl bool, rel func(lock) (string, bool)) map[string]bool {
	out := map[string]bool{}
	if x.atomic {
		out["atomic"] = true
	}
	for _, l := range x.locks {
		if excl && !l.excl {
			continue
		}
		if k, ok := rel(l); ok {
			out[k] = true
		}
	}
	return out
}

// globalLock names a lock that is one object for the whole program.
func globalLock(l lock) (string, bool) {
	if l.place.root.kind == rGlobal && (!strings.ContainsAny(l.place.path, "*%$#&") || boxedName(l.place)) {
		return l.place.key(), true
	}
	return "", false
}

// boxedName reports whether package-variable place p reaches its memory
// by field selections and array elements, its only loads being of owned[T]
// boxes: a box is not a pointer, so only a write of what holds the box,
// which frees the object, changes what the path reaches (section 19.3,
// Locks and names).
func boxedName(p vpath) bool {
	if p.root.v == nil {
		return false
	}
	T := p.root.v.Type()
	path := p.path
	for path != "" {
		switch {
		case path[0] == '*':
			if !isOwnedBox(T) {
				return false
			}
			T = pointerElem(T)
			path = strings.SliceFrom(path, 1)
		case strings.HasPrefix(path, "[]"):
			a, ok := underlying(T).(*types2.Array)
			if !ok {
				return false
			}
			T = a.Elem()
			path = strings.SliceFrom(path, 2)
		case path[0] == '.':
			end := 1
			for end < len(path) && path[end] != '.' && path[end] != '*' && path[end] != '[' {
				end++
			}
			name := strings.Slice(path, 1, end)
			st, ok := underlying(T).(*types2.Struct)
			if !ok {
				return false
			}
			var next types2.Type
			for i := 0; i < st.NumFields(); i++ {
				if st.Field(i).Name() == name {
					next = st.Field(i).Type()
				}
			}
			if next == nil {
				return false
			}
			T = next
			path = strings.SliceFrom(path, end)
		default:
			return false
		}
		if T == nil {
			return false
		}
	}
	return true
}

func intersect(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		if b[k] {
			out[k] = true
		}
	}
	return out
}

// checkGlobals applies the package variable rule.
func (a *analyzer) checkGlobals(groups []*group) {
	byVar := map[*types2.Var][]entry{}
	var vars []*types2.Var
	for _, g := range groups {
		for _, x := range g.accesses {
			if x.place.root.kind != rGlobal || strings.Contains(x.place.path, "$") {
				continue
			}
			v := x.place.root.v
			if byVar[v] == nil {
				vars = compat.Append(vars, v)
			}
			byVar[v] = compat.Append(byVar[v], entry{x, g})
		}
	}
	sort.SortFunc(vars, func(a, b *types2.Var) bool { return a.Pos() < b.Pos() })
	for _, v := range vars {
		base := globalLock
		if a.oneStatic(v) {
			base = staticLock(v)
		}
		rel := func(l lock) (string, bool) {
			if l.place.root.kind == rLocal && l.place.root.v == objectVar {
				return compat.Concat("obj:", l.place.path), true
			}
			return base(l)
		}
		a.checkPlaces(objectLocks(byVar[v], base), rel, func(x *access) string { return a.placeText(x.place) })
	}
}

// objectVar stands for "the object the access is in" in relative lock
// places (objectLocks).
var objectVar = types2.NewVar(token.NoPos, nil, "the object", nil)

// objectLocks names, in each access through a pointer, the locks held of
// the object the access is in relative to that object: a lock and a datum
// of the same struct are of the same object however it is reached
// (section 19.3, Rule 3), so two accesses of a field of one object
// holding the object's own mutex hold one lock, and accesses of two
// objects do not meet. A lock base already names is left as it is.
// The object must be the one the variable itself points to (def* for a
// package variable def): a lock reached through a pointer loaded from
// memory (g.p.mu) is not a name (section 19.3, Locks and names, row 23).
func objectLocks(es []entry, base func(lock) (string, bool)) []entry {
	out := es
	cloned := false
	for i, e := range es {
		ob := objectBase(e.x.place)
		if !hasDeref(ob.path) || strings.Contains(ob.path, "#") || len(steps(ob.path)) != 1 {
			continue
		}
		rewrite := func(ls lockset) (lockset, bool) {
			var ns lockset
			changed := false
			for _, l := range ls {
				if _, ok := base(l); !ok && l.place.root == ob.root && prefixOf(ob.path, l.place.path) && !hasDeref(strings.SliceFrom(l.place.path, len(ob.path))) {
					l = lock{place: vpath{root: root{kind: rLocal, v: objectVar}, path: strings.SliceFrom(l.place.path, len(ob.path))}, excl: l.excl}
					changed = true
				}
				ns = compat.Append(ns, l)
			}
			return ns, changed
		}
		ls, c1 := rewrite(e.x.locks)
		vs, c2 := rewrite(e.x.vvalid)
		if !c1 && !c2 {
			continue
		}
		if !cloned {
			out, cloned = compat.Clone(es), true
		}
		x := pu.New(*e.x)
		x.locks, x.vvalid = ls, vs
		out[i] = entry{x, e.g}
	}
	return out
}

// checkUnsendableGlobals reports a package variable whose type is not
// sendable, one that holds a shared[T], used by two parties once tasks may
// run (section 19.3, Rule 1, E2010): copying a reference out of it, or
// dropping one, changes a count with plain arithmetic.
func (a *analyzer) checkUnsendableGlobals(groups []*group) {
	byVar := map[*types2.Var][]entry{}
	var vars []*types2.Var
	for _, g := range groups {
		for _, x := range g.accesses {
			if x.place.root.kind != rGlobal || x.place.root.v == nil || x.pre {
				continue
			}
			v := x.place.root.v
			if types2.Unsendable(v.Type()) == nil {
				continue
			}
			if byVar[v] == nil {
				vars = compat.Append(vars, v)
			}
			byVar[v] = compat.Append(byVar[v], entry{x, g})
		}
	}
	sort.SortFunc(vars, func(a, b *types2.Var) bool { return a.Pos() < b.Pos() })
	for _, v := range vars {
		es := byVar[v]
		sort.SortStableFunc(es, func(a, b entry) bool { return a.x.pos < b.x.pos })
		pi, qi := -1, -1
		for i := range es {
			for j := range es {
				if i != j && (es[i].g != es[j].g || es[i].g.multi) {
					pi, qi = i, j
					break
				}
			}
			if pi < 0 && es[i].g.multi {
				pi, qi = i, i
			}
			if pi >= 0 {
				break
			}
		}
		if pi < 0 {
			continue
		}
		p, q := es[pi], es[qi]
		sh := types2.TypeString(types2.Unsendable(v.Type()), a.qualifier)
		d := diag.New(a.pos(p.x.pos), diag.CheckerCode(typerr.NotSendable), "%s holds a %s, whose count is not atomic, and two tasks use it; use synced[T] (spec section 19.3)", pu.Box(v.Name()), pu.Box(sh))
		other := compat.Sprintfb("%s at %s", compat.ArgS(q.g.name(a)), compat.ArgS(a.where(q.x)))
		if pi == qi {
			other = "another task that runs the same code"
		}
		d.Text = compat.Sprintfb("The package variable %s is not sendable: it holds a %s, whose count changes with plain arithmetic whenever a reference is copied out of it or dropped. Here %s uses it at %s, and %s uses it too, so both may change the count at once.", compat.ArgS(v.Name()), compat.ArgS(sh), compat.ArgS(p.g.name(a)), compat.ArgS(a.where(p.x)), compat.ArgS(other))
		d.Fix = pu.New(diag.Fix{Intro: "Use a synced[T] instead of the shared[T]: its count is atomic, so several tasks may hold references."})
		a.add(compat.Concat("unsendable|", v.Name(), "|", itoa(int(v.Pos()))), d)
	}
}

// oneStatic reports whether package variable v holds one static object
// and nothing else: what v points to is then one object for the whole
// program.
func (a *analyzer) oneStatic(v *types2.Var) bool {
	if !a.staticVar(vpath{root: root{kind: rGlobal, v: v}}) {
		return false
	}
	var n ast.Node
	for _, s := range a.gstores[v] {
		for _, o := range s.vals {
			if n != nil && o.vp.root.node != n {
				return false
			}
			n = o.vp.root.node
		}
	}
	return n != nil
}

// staticLock names, besides the locks globalLock names, the locks of the
// static object package variable v points to: a lock and a datum of that
// object are of the same object (section 19.3, Rule 2).
func staticLock(v *types2.Var) func(lock) (string, bool) {
	return func(l lock) (string, bool) {
		p := l.place
		if p.root.kind == rGlobal && p.root.v == v && strings.HasPrefix(p.path, "*") && !strings.ContainsAny(strings.SliceFrom(p.path, 1), "*%$#&") {
			return p.key(), true
		}
		return globalLock(l)
	}
}

// checkSynced applies the guard rule to the objects of synced references:
// any task holding a reference may use them, so every write conflicts
// with every other access.
func (a *analyzer) checkSynced(groups []*group) {
	byType := map[int][]entry{}
	var ids []int
	for _, g := range groups {
		for _, x := range g.accesses {
			_, id, _, ok := syncedSplit(x.place)
			if !ok || a.isMapID(id) {
				continue
			}
			if byType[id] == nil {
				ids = compat.AppendPlain(ids, id)
			}
			// Every access to a synced object may meet one by another
			// task: count it as a task that runs several times.
			byType[id] = compat.Append(byType[id], entry{x, pu.New(group{multi: true, pos: g.pos, desc: g.desc, main: g.main})})
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		es := byType[id]
		// Places within the object, relative to it, so that accesses
		// through different references compare.
		norm := compat.Make[entry](len(es))
		for i, e := range es {
			_, _, sub, _ := syncedSplit(e.x.place)
			x := pu.New(*e.x)
			x.place = vpath{root: root{kind: rLocal, v: syncedVar}, path: compat.Concat(compat.Sprintfb("$%d", compat.ArgI(int64(id))), sub)}
			// Locks: those within the same object, relative to it; and
			// package-level ones.
			base, _, _, _ := syncedSplit(e.x.place)
			prefix := compat.Concat(base.path, compat.Sprintfb("$%d", compat.ArgI(int64(id))))
			var ls, vs lockset
			for _, l := range e.x.locks {
				var np vpath
				if l.place.root == base.root && prefixOf(prefix, l.place.path) {
					np = vpath{root: root{kind: rLocal, v: syncedVar}, path: compat.Concat(compat.Sprintfb("$%d", compat.ArgI(int64(id))), strings.SliceFrom(l.place.path, len(prefix)))}
				} else if _, ok := globalLock(l); ok {
					np = l.place
				} else {
					continue
				}
				ls = ls.with(np, l.excl)
				if e.x.vvalid.find(l.place) >= 0 {
					vs = vs.with(np, l.excl)
				}
			}
			x.locks, x.vvalid = ls, vs
			norm[i] = entry{x, e.g}
		}
		a.checkPlaces(norm, func(l lock) (string, bool) {
			if l.place.root.kind == rLocal && l.place.root.v == syncedVar {
				return l.place.path, true
			}
			return globalLock(l)
		}, func(x *access) string { return a.syncedText(id, x.place.path) })
	}
}

// isMapID reports whether type number id is a map type's: the place is a
// map's contents, which checkMaps checks.
func (a *analyzer) isMapID(id int) bool {
	_, ok := a.typeIDs[id].(*types2.Map)
	return ok
}

// checkMaps applies the guard rule to the contents of maps (language spec
// section 19.3, gap G16): a map is a counted reference that may cross to
// other tasks, so the contents of every map of a type that crosses, as a
// task statement's argument, a value sent on a channel, or inside one, or
// that a task reaches through a package variable or memory it did not
// allocate, are one piece of data. Every write to it must hold one lock,
// and every access must hold it too: a package-level lock, or a lock in
// the struct the map is a field of. Maps of other types never reach a
// second task, so each stays with the task that made it. Accesses that
// fill a map before the function that made it hands it to anyone do not
// count, nor does main's use before it starts a task.
func (a *analyzer) checkMaps(groups []*group, cross []*crossing) {
	// Allocations that cross: a map made at such a site may be shared
	// between the tasks of one task statement.
	crossed := map[ast.Node]bool{}
	for _, c := range cross {
		for _, o := range c.vals {
			if o.vp.root.kind == rFresh {
				crossed[o.vp.root.node] = true
			}
		}
	}
	marked := map[int]bool{}
	for id := range a.crossMaps {
		marked[id] = true
	}
	byType := map[int][]entry{}
	var ids []int
	single := map[*group]*group{} // a multi group's accesses to maps each task makes for itself
	for _, g := range groups {
		for _, x := range g.accesses {
			base, id, _, ok := syncedSplit(x.place)
			if !ok || !a.isMapID(id) {
				continue
			}
			own := base.root.kind == rFresh && base.path == "" && !crossed[base.root.node]
			if !g.main && !own {
				marked[id] = true // a task reaches a map it did not make
			}
			eg := g
			if own && g.multi {
				// Each task the task statement starts makes its own map
				// here: they do not meet each other in it.
				if single[g] == nil {
					single[g] = pu.New(group{pos: g.pos, desc: g.desc})
				}
				eg = single[g]
			}
			if byType[id] == nil {
				ids = compat.AppendPlain(ids, id)
			}
			byType[id] = compat.Append(byType[id], entry{x, eg})
		}
	}
	if marked[a.typeID(genericMap)] {
		for _, id := range ids {
			marked[id] = true
		}
	}
	gid, hasGeneric := -1, false
	for _, id := range ids {
		if a.typeIDs[id] == genericMap {
			gid, hasGeneric = id, true
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		if !marked[id] || a.typeIDs[id] == genericMap {
			continue
		}
		es := byType[id]
		if hasGeneric {
			es = compat.Append(compat.Append([]entry(nil), es...), byType[gid]...)
		}
		norm := compat.Make[entry](len(es))
		for i, e := range es {
			base, _, _, _ := syncedSplit(e.x.place)
			x := pu.New(*e.x)
			x.place = vpath{root: root{kind: rLocal, v: mapVar}, path: compat.Sprintfb("$%d", compat.ArgI(int64(id)))}
			// Locks: package-level ones, and those of the struct the map
			// is a field of, relative to it.
			holder, field := splitField(base)
			var ls, vs lockset
			for _, l := range e.x.locks {
				var np vpath
				if field && l.place.root == holder.root && prefixOf(holder.path, l.place.path) && !strings.ContainsAny(strings.SliceFrom(l.place.path, len(holder.path)), "*%$#") {
					np = vpath{root: root{kind: rLocal, v: mapVar}, path: strings.SliceFrom(l.place.path, len(holder.path))}
				} else if _, ok := globalLock(l); ok {
					np = l.place
				} else {
					continue
				}
				ls = ls.with(np, l.excl)
				if e.x.vvalid.find(l.place) >= 0 {
					vs = vs.with(np, l.excl)
				}
			}
			x.locks, x.vvalid = ls, vs
			norm[i] = entry{x, e.g}
		}
		T := a.typeIDs[id]
		a.checkPlaces(norm, func(l lock) (string, bool) {
			if l.place.root.kind == rLocal && l.place.root.v == mapVar {
				return compat.Concat("rel:", l.place.path), true
			}
			return globalLock(l)
		}, func(*access) string { return compat.Concat("an element of a ", types2.TypeString(T, a.qualifier)) })
	}
}

// splitField splits the place of a map at its last field: the struct that
// holds the map, and true, when the map is a field of one.
func splitField(p vpath) (vpath, bool) {
	ss := steps(p.path)
	if len(ss) == 0 || !strings.HasPrefix(ss[len(ss)-1], ".") {
		return p, false
	}
	n := len(p.path) - len(ss[len(ss)-1])
	return vpath{root: p.root, path: strings.Slice(p.path, 0, n)}, true
}

// mapVar stands for "the struct holding the map" in relative places.
var mapVar = types2.NewVar(token.NoPos, nil, "the map's holder", nil)

// syncedVar stands for "the synced object" in relative places.
var syncedVar = types2.NewVar(token.NoPos, nil, "the synced object", nil)

// checkPlaces checks the accesses to one variable or synced type: every
// access that may meet a write by another task must hold a lock that every
// such write holds.
func (a *analyzer) checkPlaces(es []entry, rel func(lock) (string, bool), text func(*access) string) {
	// The accesses each one conflicts with. A write is shared when it
	// conflicts with some access (section 19.3, The guard set). A write in
	// a task that may run several times meets itself.
	conf := compat.Make[[]int](len(es))
	shared := map[int]bool{}
	for i, p := range es {
		for j, q := range es {
			if (i == j && !p.g.multi) || !conflict(p, q) {
				continue
			}
			conf[i] = compat.AppendPlain(conf[i], j)
		}
		if p.x.write && len(conf[i]) > 0 {
			shared[i] = true
		}
	}
	// The guard set of the datum a shared write writes: the locks that it
	// and every shared write of the same datum hold in write mode. Every
	// shared write counts, not only the ones that meet each other: a
	// party that is the only writer still needs one lock at all its
	// writes for a reader to know which to hold.
	gs := map[int]map[string]bool{}
	guardSet := func(i int) map[string]bool {
		if g, ok := gs[i]; ok {
			return g
		}
		g := guards(es[i].x, true, rel)
		if !es[i].x.atomic {
			for _, j := range conf[i] {
				if es[j].x.sync {
					// A pluto/sync call on memory this write changes: no
					// lock orders the two (section 19.3, Accesses).
					gs[i] = map[string]bool{}
					return gs[i]
				}
			}
		}
		for j, q := range es {
			if j != i && shared[j] && overlaps(es[i].x.place, q.x.place) {
				g = intersect(g, guards(q.x, true, rel))
			}
		}
		gs[i] = g
		return g
	}
	for i, p := range es {
		if shared[i] && len(guardSet(i)) > 0 && a.guardedW != nil {
			a.guardedW[writeKey(p.x)] = true
		}
	}
	for i, p := range es {
		if !shared[i] || len(guardSet(i)) > 0 {
			continue
		}
		// Report the write that holds no lock if there is one, against
		// an access it meets.
		pg := guards(p.x, true, rel)
		target, other := p, es[conf[i][0]]
		if !p.x.atomic {
			for _, j := range conf[i] {
				if es[j].x.sync {
					other = es[j] // the pluto/sync call that leaves it no guard
					break
				}
			}
		}
		var also *entry // a write of the same party that holds other locks
		for _, j := range conf[i] {
			q := es[j]
			if !q.x.write {
				continue
			}
			qg := guards(q.x, true, rel)
			if len(intersect(pg, qg)) > 0 {
				continue
			}
			if len(pg) > 0 && len(qg) == 0 {
				target, other = q, p
			} else {
				other = q
			}
			break
		}
		tg := guards(target.x, true, rel)
		if len(tg) > 0 && (!other.x.write || len(intersect(tg, guards(other.x, true, rel))) > 0) {
			// No write the reported one meets breaks the guard: one of
			// its own party's does.
			for j, q := range es {
				if shared[j] && q.x != target.x && overlaps(target.x.place, q.x.place) && len(intersect(tg, guards(q.x, true, rel))) == 0 {
					also = pu.New(q)
					break
				}
			}
		}
		d := a.noLockError(target, other, es, rel, text)
		if also != nil {
			ag := guards(also.x, true, rel)
			held := "with no lock held"
			if len(ag) > 0 {
				var names []string
				for k := range ag {
					names = compat.Append(names, a.lockText(k, rel, es))
				}
				sort.Strings(names)
				held = compat.Concat("holding ", compat.Join(names, " and "))
			}
			d.Text = compat.Concat(d.Text, compat.Sprintfb(" %s also writes it at %s %s, so the locks that guard it would differ from write to write.", compat.ArgS(capitalize(also.g.name(a))), compat.ArgS(a.where(also.x)), compat.ArgS(held)))
		}
		a.add(compat.Concat("nolock|", text(p.x)), d)
		return
	}
	// An access through a view loaded from shared memory needs a lock of
	// the guard set held from the load to it with no ending event
	// (section 19.3, Views loaded under a lock). A view loaded before the
	// start of sharing was not loaded by a crossing load.
	ended := map[int]bool{}
	for i, p := range es {
		if !p.x.hasValid || p.x.vpre {
			continue
		}
		vg := guards(pu.New(access{locks: p.x.vvalid, atomic: p.x.atomic}), p.x.write, rel)
		var g map[string]bool
		if p.x.write {
			if shared[i] && len(guardSet(i)) > 0 && len(intersect(vg, guardSet(i))) == 0 {
				g = guardSet(i)
			}
		} else {
			for _, j := range conf[i] {
				if es[j].x.write && len(guardSet(j)) > 0 && len(intersect(vg, guardSet(j))) == 0 {
					g = guardSet(j)
					break
				}
			}
		}
		if g == nil {
			continue
		}
		ended[i] = true
		a.add(compat.Sprintfb("viewend|%d", compat.ArgI(int64(p.x.pos))), a.viewEndedError(p, es, g, rel, text))
	}
	for i, p := range es {
		if p.x.write || ended[i] {
			continue
		}
		// The read needs a lock of the guard set of each datum a write it
		// may meet writes.
		// A write whose guard set is empty is E3502 already.
		held := guards(p.x, false, rel)
		var g map[string]bool
		for _, j := range conf[i] {
			if es[j].x.write && len(guardSet(j)) > 0 && len(intersect(held, guardSet(j))) == 0 {
				g = guardSet(j)
				break
			}
		}
		if g == nil {
			continue
		}
		// The writes it may meet that hold that guard set, for the message.
		var writes []entry
		for _, j := range conf[i] {
			if es[j].x.write && sameKeys(guardSet(j), g) {
				writes = compat.Append(writes, es[j])
			}
		}
		a.add(compat.Sprintfb("read|%d", compat.ArgI(int64(p.x.pos))), a.unguardedError(p, writes, g, rel, text))
	}
}

// ----------------------------------------------------------------------------
// Messages

// placeText writes a package variable's place as the program would.
func (a *analyzer) placeText(p vpath) string {
	if isAllocMem(p) {
		return compat.Concat("the memory allocated from ", a.placeText(allocMemOwner(p)))
	}
	name := p.root.v.Name()
	if pkg := p.root.v.Pkg(); pkg != nil && a.prog.Main != nil && pkg != a.prog.Main.Types {
		name = compat.Concat(pkg.Name(), ".", name)
	}
	return compat.Concat(name, pathText(p.path))
}

// syncedText writes a place in a synced object.
func (a *analyzer) syncedText(id int, path string) string {
	T := a.typeIDs[id]
	_, _, sub, _ := syncedSplit(vpath{path: path})
	return compat.Concat("the field ", strings.TrimPrefix(pathText(sub), "."), " of a synced[", types2.TypeString(T, a.qualifier), "]")
}

func pathText(p string) string {
	var b compat.Builder
	ss := steps(p)
	for i, s := range ss {
		switch {
		case strings.HasPrefix(s, "."):
			b.WriteString(s)
		case s == "[]" || s == "%":
			if i+1 < len(ss) && strings.HasPrefix(ss[i+1], "@c") {
				b.WriteString(compat.Concat("[", strings.SliceFrom(ss[i+1], 2), "]"))
			} else {
				b.WriteString("[i]")
			}
		case s == "*" || s[0] == '$':
			if i == len(ss)-1 {
				return compat.Concat("*(", b.String(), ")")
			}
		}
	}
	return b.String()
}

// lockText names a lock as the program would.
func (a *analyzer) lockText(k string, rel func(lock) (string, bool), es []entry) string {
	if k == "atomic" {
		return "atomic operations"
	}
	for _, e := range es {
		for _, l := range e.x.locks {
			if lk, ok := rel(l); ok && lk == k {
				if l.place.root.kind == rGlobal {
					return a.placeText(l.place)
				}
				return strings.TrimPrefix(pathText(strings.TrimLeft(l.place.path, "$0123456789")), ".")
			}
		}
	}
	return k
}

// where describes an access: its position, and the call that leads to it
// when it is in another function.
func (a *analyzer) where(x *access) string {
	return compat.Concat(a.short(x.pos), " in ", a.funcAt(x.pos))
}

func (a *analyzer) noLockError(p, other entry, es []entry, rel func(lock) (string, bool), text func(*access) string) *diag.Diagnostic {
	place := text(p.x)
	verb := "written"
	party := "another task"
	if p.g.handler || other.g.handler {
		party = "another task or an interrupt handler"
	}
	d := diag.New(a.pos(p.x.pos), diag.RaceNoLock, "%s is %s here with no lock that guards it, while %s may use it", pu.Box(place), pu.Box(verb), pu.Box(party))
	held := guards(p.x, true, rel)
	shared := guards(p.x, false, rel)
	b := pu.Alloc[compat.Builder]()
	compat.Fprintfb(b, "%s is %s at %s by %s", compat.ArgS(place), compat.ArgS(verb), compat.ArgS(a.where(p.x)), compat.ArgS(p.g.name(a)))
	switch {
	case len(held) == 0 && len(shared) > 0:
		k := ""
		for x := range shared {
			k = x
		}
		compat.Fprintfb(b, " while holding %s only for reading", compat.ArgS(a.lockText(k, rel, es)))
	case len(held) == 0:
		b.WriteString(" with no lock held")
	default:
		var names []string
		for k := range held {
			names = compat.Append(names, a.lockText(k, rel, es))
		}
		sort.Strings(names)
		compat.Fprintfb(b, " holding %s", compat.ArgS(compat.Join(names, " and ")))
	}
	ov := "uses"
	if other.x.write {
		ov = "writes"
	}
	if other.g == p.g && p.g.handler {
		compat.Fprintfb(b, ", and the same handler running on another processor %s it at %s", compat.ArgS(ov), compat.ArgS(a.where(other.x)))
	} else if other.g == p.g {
		compat.Fprintfb(b, ", and another task started there %s it at %s", compat.ArgS(ov), compat.ArgS(a.where(other.x)))
	} else {
		compat.Fprintfb(b, ", and %s %s it at %s", compat.ArgS(other.g.name(a)), compat.ArgS(ov), compat.ArgS(a.where(other.x)))
	}
	og := guards(other.x, true, rel)
	if len(og) > 0 && other.x.write {
		var names []string
		for k := range og {
			names = compat.Append(names, a.lockText(k, rel, es))
		}
		sort.Strings(names)
		compat.Fprintfb(b, " holding %s", compat.ArgS(compat.Join(names, " and ")))
	}
	if p.g.handler || other.g.handler {
		b.WriteString(". An interrupt handler may run at any point where interrupts are enabled, on any processor and on several at once, so these may run at the same time, one of them writing, which is a data race: the other may see a value half written, or lose an update. Volatile accesses and disabled interrupts exclude only the same processor.")
	} else {
		b.WriteString(". Two tasks may run these at the same time, one of them writing, which is a data race: the other may see a value half written, or lose an update.")
	}
	_, chanOp := underlying(other.x.typ).(*types2.Chan)
	if other.x.sync && chanOp {
		b.WriteString("\nThe use there is a channel operation, which synchronizes only with other operations on the channel: no lock orders it with a write that frees, moves or overwrites the channel, whatever locks the write holds.")
	} else if other.x.sync {
		b.WriteString("\nThe use there is a call of a pluto/sync method, which synchronizes only with other such calls: no lock orders it with a write that frees, moves or overwrites the value, whatever locks the write holds.")
	} else {
		b.WriteString("\nThe compiler infers which lock guards shared data from the locks held where it is written: a lock that every write holds. Here no one lock is held at every write.")
	}
	d.Text = b.String()
	d.Fix = pu.New(diag.Fix{Intro: "Hold one sync.Mutex at every access, writes and reads (a sync.RWMutex's RLock is enough for reads)", Code: compat.Concat("mu.Lock()\n", strings.TrimPrefix(exprLine(place), ""), "\nmu.Unlock()")})
	if p.g.handler || other.g.handler {
		d.Fix.Intro = "Hold one rt.Lock at every access, writes and reads (a handler cannot take a sync.Mutex)"
	}
	if !strings.HasPrefix(place, "an element of a map") {
		d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "Or, for a number, use an atomic type from pluto/sync/atomic, such as atomic.Int64, and its methods at every access."})})
	}
	return d
}

// exprLine writes a statement that writes place, for fixes.
func exprLine(place string) string {
	if strings.HasPrefix(place, "the field") {
		return "s.f = v"
	}
	if strings.HasPrefix(place, "an element of a map") {
		return "m[k] = v"
	}
	return compat.Concat(place, " = v")
}

// viewEndedError reports an access through a view that is not valid
// there: no lock of the guard set g stayed held from the load of the view
// to the access.
func (a *analyzer) viewEndedError(p entry, es []entry, g map[string]bool, rel func(lock) (string, bool), text func(*access) string) *diag.Diagnostic {
	place := text(p.x)
	var names []string
	for k := range g {
		names = compat.Append(names, a.lockText(k, rel, es))
	}
	sort.Strings(names)
	guard := compat.Join(names, " or ")
	verb := "read"
	if p.x.write {
		verb = "written"
	}
	d := diag.New(a.pos(p.x.pos), diag.RaceSendView, "%s is %s here through a view that is not valid here: %s, which guards it, was not held from the load of the view to here", pu.Box(place), pu.Box(verb), pu.Box(guard))
	d.Text = compat.Sprintf("%s is %s at %s through a view loaded from shared memory. A view loaded from memory a lock guards stays valid only while a lock held at the load stays held: an Unlock, an RUnlock or a Wait of it, or a call that may release it, ends the view, even if the lock is taken again. Once the lock is released another task may change, move or free what the view points to.", pu.Box(capitalize(place)), pu.Box(verb), pu.Box(a.where(p.x)))
	d.Fix = pu.New(diag.Fix{Intro: "Keep the lock held from the load of the view to its last use, or load the view again after taking the lock"})
	return d
}

func (a *analyzer) unguardedError(p entry, writes []entry, g map[string]bool, rel func(lock) (string, bool), text func(*access) string) *diag.Diagnostic {
	place := text(p.x)
	var names []string
	for k := range g {
		names = compat.Append(names, a.lockText(k, rel, writes))
	}
	sort.Strings(names)
	guard := compat.Join(names, " or ")
	var d *diag.Diagnostic
	if g["atomic"] && len(g) == 1 {
		d = diag.New(a.pos(p.x.pos), diag.RaceUnguarded, "%s is read here without an atomic operation, but other tasks change it with atomic operations", pu.Box(place))
	} else {
		d = diag.New(a.pos(p.x.pos), diag.RaceUnguarded, "%s is read here without holding %s, which guards it", pu.Box(place), pu.Box(guard))
	}
	b := pu.Alloc[compat.Builder]()
	compat.Fprintfb(b, "Every write to %s that another task may run at the same time holds %s:", compat.ArgS(place), compat.ArgS(guard))
	sort.SortFunc(writes, func(a, b entry) bool { return a.x.pos < b.x.pos })
	n := 0
	seen := map[token.Pos]bool{}
	for _, w := range writes {
		if seen[w.x.pos] || n == 3 {
			continue
		}
		seen[w.x.pos] = true
		sep := ","
		if n == 0 {
			sep = ""
		}
		compat.Fprintfb(b, "%s %s (%s)", compat.ArgS(sep), compat.ArgS(a.where(w.x)), compat.ArgS(w.g.name(a)))
		n++
	}
	compat.Fprintfb(b, ". So the compiler infers that %s guards %s, and every read must hold it too: a read without it may run while a write is half done.", compat.ArgS(guard), compat.ArgS(place))
	compat.Fprintfb(b, "\nHere %s reads it at %s", compat.ArgS(p.g.name(a)), compat.ArgS(a.where(p.x)))
	if held := guards(p.x, false, rel); len(held) > 0 {
		var hn []string
		for k := range held {
			hn = compat.Append(hn, a.lockText(k, rel, writes))
		}
		sort.Strings(hn)
		compat.Fprintfb(b, " holding only %s", compat.ArgS(compat.Join(hn, " and ")))
	} else {
		b.WriteString(" with no lock held")
	}
	b.WriteString(".")
	d.Text = b.String()
	lockName := names[0]
	if g["atomic"] {
		d.Fix = pu.New(diag.Fix{Intro: "Read it with the atomic type's Load method, as the writes use atomic operations"})
	} else {
		d.Fix = pu.New(diag.Fix{Intro: "Hold the lock while reading", Code: compat.Concat(lockName, ".Lock()\nv := ", strings.TrimPrefix(place, ""), "\n", lockName, ".Unlock()")})
	}
	return d
}

func (a *analyzer) sendError(c *crossing, p *problem) *diag.Diagnostic {
	elem := types2.TypeString(p.elem, a.qualifier)
	var d *diag.Diagnostic
	if c.expr == nil || p.part == exprText(c.expr) {
		d = diag.New(a.pos(c.pos), diag.RaceSendView, "%s is a view of %s that may change while another task uses it, so it cannot be %s", pu.Box(p.part), pu.Box(elem), pu.Box(crossVerb(c)))
	} else {
		d = diag.New(a.pos(c.pos), diag.RaceSendView, "%s is a view of %s that may change while another task uses it, so %s cannot be %s", pu.Box(p.part), pu.Box(elem), pu.Box(exprText(c.expr)), pu.Box(crossVerb(c)))
	}
	b := pu.Alloc[compat.Builder]()
	if c.export {
		compat.Fprintfb(b, "C code may call %s at any time, from any thread, and pass it %s, a %s: the compiler cannot see which %s it points to, so other calls and other tasks may reach the same %s.", compat.ArgS(c.fn), compat.ArgS(p.part), compat.ArgS(types2.TypeString(p.typ, a.qualifier)), compat.ArgS(elem), compat.ArgS(elem))
	} else if c.load {
		compat.Fprintfb(b, "Loading %s here gives this task %s, a %s that other tasks may reach too: every task that loads it can reach the same %s.", compat.ArgS(c.name), compat.ArgS(p.part), compat.ArgS(types2.TypeString(p.typ, a.qualifier)), compat.ArgS(elem))
	} else {
		compat.Fprintfb(b, "%s gives another task %s, a %s: after this, both tasks can reach the same %s.", compat.ArgS(capitalize(c.what)), compat.ArgS(p.part), compat.ArgS(types2.TypeString(p.typ, a.qualifier)), compat.ArgS(elem))
	}
	if p.glob != nil {
		compat.Fprintfb(b, " It points to the package variable %s, which is written at %s while tasks run.", compat.ArgS(p.glob.Name()), compat.ArgS(a.short(p.gpos)))
	}
	switch {
	case p.syncFree:
		compat.Fprintfb(b, " A %s synchronizes itself through its methods, but only while it exists: that write frees, moves or overwrites it while the other task may still use it.", compat.ArgS(elem))
	case p.why != "":
		compat.Fprintfb(b, " %s is not safe to use from two tasks at once. %s", compat.ArgS(elem), compat.ArgS(p.why))
	case p.write != nil && p.write.expr == "an object of the type":
		compat.Fprintfb(b, " A %s is freed, dropped, moved or overwritten at %s (in %s) once tasks may run, so the compiler cannot tell that this one is not.", compat.ArgS(elem), compat.ArgS(a.short(p.write.pos)), compat.ArgS(p.write.fn))
	case p.write != nil:
		compat.Fprintfb(b, " A %s is changed through a pointer at %s (%s, in %s) without a lock of the same %s held, so the compiler cannot tell that this one stays the same while the other task uses it.", compat.ArgS(elem), compat.ArgS(a.short(p.write.pos)), compat.ArgS(p.write.expr), compat.ArgS(p.write.fn), compat.ArgS(elem))
	case p.glob == nil:
		compat.Fprintfb(b, " Nothing shows that this %s stays the same while the other task uses it.", compat.ArgS(elem))
	}
	b.WriteString("\nA view may cross to another task only if what it points to cannot change while both may use it, or every change is synchronized: a package variable that is not written once tasks run, a struct whose fields are never changed once it is shared or are used only under a lock of the same struct or atomically, memory of a pluto/sync type, or the object of a synced[T].")
	d.Text = b.String()
	d.Fix = pu.New(diag.Fix{Intro: "Pass a copy of the value instead of a pointer to it, or move an owner (such as a vec.Vec) to the task so that only it has the memory."})
	d.More = compat.Clone([]*diag.Fix{pu.New(diag.Fix{Intro: "To share changing data between tasks, put it in a synced[T] and hold a sync.Mutex in it at every access, or use a package variable guarded by a lock."})})
	return d
}

// sharedError reports a value that crosses to another task holding a
// shared[T] in an interface (language spec section 8.8): the error the type
// checker gives for a shared[T] it can see (E2010).
func (a *analyzer) sharedError(c *crossing, p *problem) *diag.Diagnostic {
	sh := types2.TypeString(p.shared, a.qualifier)
	held := types2.TypeString(p.typ, a.qualifier)
	what := "the value"
	if c.expr != nil {
		what = exprText(c.expr)
	}
	part := "it"
	if p.part != what {
		part = p.part
	}
	where := ""
	if p.site != nil {
		where = compat.Sprintfb(" (converted at %s, package %s)", compat.ArgS(a.short(p.site.pos)), compat.ArgS(p.site.pkg))
	}
	d := diag.New(a.pos(c.pos), diag.CheckerCode(typerr.NotSendable), "%s is not sendable: %s is an interface that may hold a %s%s, which contains %s, whose count is not atomic; use synced[T] (spec section 19.2)", pu.Box(what), pu.Box(part), pu.Box(held), pu.Box(where), pu.Box(sh))
	d.Text = compat.Sprintfb("%s gives another task %s. The program converts a %s to that interface type, and a %s contains a %s, directly or through a field, element or pointer. Both tasks could then copy or drop that %s, changing its count without atomic operations.", compat.ArgS(capitalize(c.what)), compat.ArgS(what), compat.ArgS(held), compat.ArgS(held), compat.ArgS(sh), compat.ArgS(sh))
	d.Fix = pu.New(diag.Fix{Intro: "Use a synced[T] instead of the shared[T]: its count is atomic, so it may cross between tasks."})
	return d
}

func crossVerb(c *crossing) string {
	if c.export {
		return "passed in by C code"
	}
	if c.load {
		return "loaded once tasks may run"
	}
	if strings.HasPrefix(c.what, "the value sent") {
		return "sent on a channel"
	}
	return "passed to a new task"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return compat.Concat(strings.ToUpper(strings.Slice(s, 0, 1)), strings.SliceFrom(s, 1))
}

func sortedKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = compat.Append(out, k)
	}
	sort.Strings(out)
	return out
}
