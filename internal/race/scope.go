package race

import (
	"fmt"
	"go/constant"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Scoped tasks (language spec sections 19.4 and 19.3).
//
// A task started by s.Task on a sync.Scope of the function (internal/
// scoped) may use the function's frame: its variables, and what they point
// to. For the summaries of the rest of the analysis, such a task's code
// counts as part of the code that starts it, since it runs before the
// Scope's block ends: what it does to package variables, synced objects
// and memory the parameters reach is what the starting function does,
// except that it holds none of the starter's locks.
//
// That leaves the races among the parties of a Scope: the function that
// starts the tasks (the starter) and the tasks themselves, which run at the
// same time from each task's start to its join. The check of this file
// walks the starter and each task once more, recording every use of
// memory, frame included, with the locks held and, in the starter, the
// tasks that may be running at that point:
//
//   - A task is running from its s.Task call until s waits for it: a call
//     s.Wait(), or the end of the Scope's block, which drops the Scope.
//     The flow analysis follows a set of running tasks; a Scope's block
//     that has ended (the position is outside it, or before its
//     declaration in a later iteration of a loop) has joined its tasks.
//     Deferred calls run before any variable is dropped, so every task is
//     still running there. A task started while an earlier one of the same
//     call may run (a loop that does not wait) runs at the same time as
//     itself.
//   - A task's own variables, and the variables it takes (copies made by
//     s.Task, internal/scoped), are its own and race with nothing. A task
//     started inside a task's literal on another Scope is walked as part
//     of it; the tasks a task starts on its own Scope are checked with it
//     as their starter.
//   - Two uses by parties that may run at once conflict when one writes,
//     not both are atomic, they may reach the same memory, and no lock is
//     held by both (exclusively by a writer).
//   - They reach the same memory when their places overlap. Index and slice
//     expressions select parts of elements (path.go): different constant
//     elements; x[i] and windows x[i*k:(i+1)*k], x[i:i+c] of the variable i
//     of the loops that start a task (each task takes its own i, and each
//     iteration has a different one), which instances of the same task
//     never share; and x[:m] and x[m:] or x[m+d:], which never meet. A part
//     covers the element's own storage: what a pointer stored there points
//     to is not partitioned. Views of memory the starter's caller or the
//     heap provides (through parameters, package variables, or pointers the
//     analysis cannot follow) may point to the same memory as each other,
//     so two uses through different ones conflict if they may be of the
//     same type.
//
// A conflict is E3402, or E3403 when several instances of one task write
// elements the parts cannot tell apart, or E3404 when the starter moves or
// drops a variable a running task uses. A task that changes a variable it
// takes, which the starter uses afterwards, is E3407: the starter would not
// see the change.

// ----------------------------------------------------------------------------
// Running sets

// A runEntry is a running party and the Scopes it is still to be joined by.
type runEntry struct {
	party int
	mask  uint64
}

// A runSet is the parties that may be running, in party order.
type runSet []runEntry

func (r runSet) has(p int) bool {
	for _, e := range r {
		if e.party == p {
			return true
		}
	}
	return false
}

func (r runSet) union(s runSet) runSet {
	if len(s) == 0 {
		return r
	}
	if len(r) == 0 {
		return s
	}
	var out runSet
	i, j := 0, 0
	for i < len(r) || j < len(s) {
		switch {
		case j == len(s) || i < len(r) && r[i].party < s[j].party:
			out = append(out, r[i])
			i++
		case i == len(r) || s[j].party < r[i].party:
			out = append(out, s[j])
			j++
		default:
			out = append(out, runEntry{r[i].party, r[i].mask | s[j].mask})
			i++
			j++
		}
	}
	return out
}

func (r runSet) equal(s runSet) bool {
	if len(r) != len(s) {
		return false
	}
	for i := range r {
		if r[i] != s[i] {
			return false
		}
	}
	return true
}

// with adds party p, running until the Scopes of mask join it.
func (r runSet) with(p int, mask uint64) runSet {
	return r.union(runSet{{p, mask}})
}

// keep keeps the Scopes of mask, and the parties some of them still wait
// for.
func (r runSet) keep(mask uint64) runSet {
	var out runSet
	for _, e := range r {
		if m := e.mask & mask; m != 0 {
			out = append(out, runEntry{e.party, m})
		}
	}
	return out
}

// ----------------------------------------------------------------------------
// The check

// A frameAcc is a use of memory by a party.
type frameAcc struct {
	place  vpath
	write  bool
	atomic bool
	move   bool // the starter moves or drops the variable (E3404)
	drop   bool // by calling its Drop method
	locks  lockset
	pos    token.Pos
	via    token.Pos // the call in the party's own code that makes it
	typ    types2.Type
	run    runSet // the starter: the tasks that may be running
}

// A party is the starter or one task call.
type party struct {
	idx   int
	task  *scoped.Task // nil for the starter
	start runSet       // the parties that may be running when it starts
	mask  uint64       // the Scopes that join it
	self  bool         // it starts tasks of a Scope of the starter's in a loop of its own
	accs  []*frameAcc
	seen  map[string]bool
	taken map[*types2.Var]bool
	lits  []*ast.FuncLit // its literal and the task literals walked with it
	// required are the variables of the loops between the Scope's block
	// and the call: a part of each tells instances of the task apart.
	required map[*types2.Var]bool
	partial  bool // a loop between them has no variable that tells iterations apart
	takenW   []*frameAcc
}

// A frameEdge is a call made by a party, whose callee's uses of package
// variables and synced objects are expanded once the walk is done.
type frameEdge struct {
	party *party
	f     *function
	args  []origins
	locks lockset
	run   runSet
	pos   token.Pos
}

// A frameCheck checks the parties of the tasks that one function starts.
type frameCheck struct {
	a        *analyzer
	starter  *function
	scopes   []*scoped.Scope
	scopeBit map[*types2.Var]uint
	parties  []*party
	byCall   map[*ast.CallExpr]*party
	unit     *unit
	record   bool
	edges    []frameEdge
	varIDs   map[*types2.Var]int
	vars     []*types2.Var
}

// checkScopes runs the check for every function that starts scoped tasks.
func (a *analyzer) checkScopes() {
	starts := map[ast.Node][]*scoped.Task{}
	var order []ast.Node
	for _, t := range a.scoped.Tasks {
		if starts[t.Func] == nil {
			order = append(order, t.Func)
		}
		starts[t.Func] = append(starts[t.Func], t)
	}
	sort.Slice(order, func(i, j int) bool { return order[i].Pos() < order[j].Pos() })
	for _, n := range order {
		var f *function
		switch n := n.(type) {
		case *ast.FuncDecl:
			if obj, ok := a.defs[n.Name].(*types2.Func); ok {
				f = a.funcs[obj]
			}
		case *ast.FuncLit:
			f = a.lits[n]
		}
		if f == nil || f.body == nil || a.reach != nil && !a.reach[f.unit] {
			continue
		}
		ts := starts[n]
		sort.Slice(ts, func(i, j int) bool { return ts[i].Call.Pos() < ts[j].Call.Pos() })
		fc := newFrameCheck(a, f, ts)
		fc.run()
		fc.check()
	}
	a.checkConcCalls()
}

func newFrameCheck(a *analyzer, f *function, ts []*scoped.Task) *frameCheck {
	fc := &frameCheck{a: a, starter: f, scopeBit: map[*types2.Var]uint{}, byCall: map[*ast.CallExpr]*party{}, varIDs: map[*types2.Var]int{}}
	fc.parties = append(fc.parties, &party{idx: 0, seen: map[string]bool{}})
	bit := func(v *types2.Var) uint64 {
		b, ok := fc.scopeBit[v]
		if !ok {
			if len(fc.scopes) == 64 {
				return 1 << 63
			}
			b = uint(len(fc.scopes))
			fc.scopeBit[v] = b
			fc.scopes = append(fc.scopes, a.scoped.Scopes[v])
		}
		return 1 << b
	}
	for _, t := range ts {
		p := &party{idx: len(fc.parties), task: t, seen: map[string]bool{}, taken: map[*types2.Var]bool{}, mask: bit(t.Scope.Var)}
		fc.parties = append(fc.parties, p)
		fc.byCall[t.Call] = p
		fc.fold(p, t)
		fc.loopsOf(p, t)
		fc.nestedLoops(p)
	}
	// The tasks a task starts on the starter's Scopes run until those
	// join: their party waits for them too.
	for _, p := range fc.parties[1:] {
		for _, lit := range p.lits[1:] {
			if nt := a.scoped.Lits[lit]; nt != nil {
				if _, ok := fc.scopeBit[nt.Scope.Var]; ok || nt.Scope.Func == fc.starterNode() {
					p.mask |= bit(nt.Scope.Var)
					if len(nt.Loops) > 0 || len(p.lits) > 2 {
						p.self = true
					}
				}
			}
		}
	}
	// Variables of the frame: those of the code walked here are worked
	// out again, so that they carry the parts of the elements they view;
	// the rest come from the summaries.
	fc.unit = &unit{vars: map[*types2.Var]origins{}}
	body := f.body
	inLits := map[*types2.Var]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && a.scoped.Lits[lit] == nil {
			ast.Inspect(lit.Body, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok {
					if v, ok := a.uses[id].(*types2.Var); ok {
						inLits[v] = true
					}
				}
				return true
			})
			return false
		}
		return true
	})
	for v, o := range f.unit.vars {
		if v.Pos() >= body.Pos() && v.Pos() < body.End() && !inLits[v] {
			continue
		}
		fc.unit.vars[v] = append(origins(nil), o...)
	}
	return fc
}

// starterNode returns the syntax of the starter.
func (fc *frameCheck) starterNode() ast.Node {
	if fc.starter.lit != nil {
		return fc.starter.lit
	}
	return fc.starter.decl
}

// fold adds to p the literal of task t and the literals of the tasks it
// starts in turn, which are walked as part of it.
func (fc *frameCheck) fold(p *party, t *scoped.Task) {
	p.lits = append(p.lits, t.Lit)
	for v := range t.Taken {
		p.taken[v] = true
	}
	for _, nt := range fc.a.scoped.Tasks {
		if nt.Outer == t {
			fc.fold(p, nt)
		}
	}
}

// nestedLoops adds to p's required variables those of the loops around
// the tasks started inside its literal: an instance of p is one of each.
func (fc *frameCheck) nestedLoops(p *party) {
	for _, lit := range p.lits[1:] {
		nt := fc.a.scoped.Lits[lit]
		if nt == nil {
			continue
		}
		for _, L := range nt.Loops {
			if L.Var == nil || !p.taken[L.Var] {
				p.partial = true
				continue
			}
			p.required[L.Var] = true
		}
	}
}

// loopsOf works out the variables that tell p's instances apart: one for
// each loop around the call inside the Scope's block.
func (fc *frameCheck) loopsOf(p *party, t *scoped.Task) {
	B := t.Scope.Node
	p.required = map[*types2.Var]bool{}
	for _, L := range t.Loops {
		if B != nil && (L.Stmt.Pos() < B.Pos() || L.Stmt.End() > B.End()) {
			continue
		}
		if L.Var == nil || !p.taken[L.Var] {
			p.partial = true
			continue
		}
		p.required[L.Var] = true
	}
}

// varID numbers a variable for part steps.
func (fc *frameCheck) varID(v *types2.Var) int {
	if id, ok := fc.varIDs[v]; ok {
		return id
	}
	id := len(fc.vars)
	fc.varIDs[v] = id
	fc.vars = append(fc.vars, v)
	return id
}

// run walks every party until the variables stop changing, then once more
// to record.
func (fc *frameCheck) run() {
	for pass := 0; pass < 10; pass++ {
		fc.unit.grew = false
		fc.walkAll()
		if !fc.unit.grew {
			break
		}
	}
	fc.record = true
	fc.walkAll()
	fc.expandEdges()
}

func (fc *frameCheck) walkAll() {
	for _, p := range fc.parties {
		p.accs, p.seen, p.takenW = nil, map[string]bool{}, nil
		if p.task == nil {
			p.start = nil
		}
	}
	fc.edges = nil
	// The starter.
	w := fc.walker(fc.parties[0], fc.starter)
	for i, prm := range w.fn.params {
		w.setVar(prm, origins{{vp: vpath{root: root{kind: rParam, idx: i}}}})
	}
	w.gotos = gotoTargets(fc.starter.body)
	w.block(fc.starter.body)
	w.finish()
	// The tasks.
	for _, p := range fc.parties[1:] {
		lf := fc.a.lits[p.task.Lit]
		if lf == nil {
			continue
		}
		w := fc.walker(p, lf)
		w.st.spawned = true
		w.gotos = gotoTargets(lf.body)
		w.block(lf.body)
		w.finish()
	}
}

func (fc *frameCheck) walker(p *party, f *function) *walker {
	w := newWalker(fc.a, f)
	w.u = fc.unit
	w.frame = fc
	w.party = p
	return w
}

// expandEdges adds the uses of package variables and synced objects that
// the parties' callees make.
func (fc *frameCheck) expandEdges() {
	for _, e := range fc.edges {
		x := &expander{a: fc.a, seen: map[expKey]bool{}, keys: map[accKey]bool{}}
		x.expand(e.f, e.args, e.locks, false)
		for _, acc := range x.out {
			fc.add(e.party, &frameAcc{place: acc.place, write: acc.write, atomic: acc.atomic, locks: acc.locks, pos: acc.pos, via: e.pos, typ: acc.typ, run: e.run})
		}
	}
}

// alive returns the Scopes whose block holds position pos, past their
// declaration: the others have joined their tasks.
func (fc *frameCheck) alive(pos token.Pos) uint64 {
	var m uint64
	for i, sc := range fc.scopes {
		if sc == nil {
			continue
		}
		n := sc.Node
		if n == nil || !n.Pos().IsValid() || n.Pos() <= pos && pos < n.End() && pos > sc.Var.Pos() {
			m |= 1 << uint(i)
		}
	}
	return m
}

// running returns the tasks that may be running at pos in the starter.
func (w *walker) running(pos token.Pos) runSet {
	if w.noFilter {
		return w.st.running
	}
	return w.st.running.keep(w.frame.alive(pos))
}

// note records a use of memory by the party being walked.
func (fc *frameCheck) note(w *walker, x *frameAcc) {
	if !fc.record || w.quiet > 0 || w.st.dead {
		return
	}
	if w.party.task == nil {
		at := x.pos
		if x.via.IsValid() {
			at = x.via
		}
		x.run = w.running(at)
	}
	fc.add(w.party, x)
}

// add records x as a use by p, unless it is of p's own memory.
func (fc *frameCheck) add(p *party, x *frameAcc) {
	if len(p.lits) > 0 {
		if r := x.place.root; r.kind == rLocal && p.taken[r.v] {
			if x.write && !x.atomic && !strings.ContainsAny(x.place.path, "*%$#") {
				p.takenW = append(p.takenW, x)
			}
			return
		}
		if p.own(x.place.root) {
			return
		}
	}
	if x.place.root.kind == rFunc || x.place.root.kind == rLit {
		return
	}
	k := fmt.Sprintf("%s|%v%v%v|%s|%d|%s", x.place.key(), x.write, x.atomic, x.move, x.locks, x.pos, x.run.String())
	if p.seen[k] {
		return
	}
	p.seen[k] = true
	p.accs = append(p.accs, x)
}

func (r runSet) String() string {
	var b strings.Builder
	for _, e := range r {
		fmt.Fprintf(&b, "%d:%x,", e.party, e.mask)
	}
	return b.String()
}

// own reports whether memory with root r belongs to one instance of p: a
// variable declared in its literals, or memory allocated there.
func (p *party) own(r root) bool {
	var pos token.Pos
	switch r.kind {
	case rLocal, rUnknown:
		if r.v == nil {
			return false
		}
		pos = r.v.Pos()
	case rFresh:
		pos = r.node.Pos()
	default:
		return false
	}
	for _, lit := range p.lits {
		if lit.Pos() <= pos && pos < lit.End() {
			return true
		}
	}
	return false
}

// noteVar records a read of local variable v at id, or a move of it.
func (fc *frameCheck) noteVar(w *walker, v *types2.Var, id *ast.Ident) {
	p := vpath{root: root{kind: rLocal, v: v}}
	move := fc.a.moves != nil && fc.a.moves.Moves[id]
	fc.note(w, &frameAcc{place: p, write: move, move: move, pos: id.Pos(), typ: v.Type(), locks: w.st.locks})
}

// ----------------------------------------------------------------------------
// Calls

// scopedTaskCall walks a call s.Task(lit) of a scoped task.
func (w *walker) scopedTaskCall(e *ast.CallExpr, t *scoped.Task) {
	if fc := w.frame; fc != nil {
		if w.party.task == nil {
			if p := fc.byCall[e]; p != nil {
				if fc.record && w.quiet == 0 {
					p.start = p.start.union(w.running(e.Pos()))
				}
				w.st.running = w.st.running.with(p.idx, p.mask)
				return
			}
		}
		// A task a task starts on another Scope: part of the party.
		w.inlineLit(t.Lit, nil, true)
		return
	}
	// A summary: the task's code is the starter's, run with no locks
	// held, after a task has started.
	f := w.a.lits[t.Lit]
	if f == nil {
		return
	}
	n := len(w.fn.params)
	if d := w.fn.unit.fns[len(w.fn.unit.fns)-1]; len(d.params) > n {
		n = len(d.params)
	}
	args := make([]origins, n)
	for i := range args {
		args[i] = origins{{vp: vpath{root: root{kind: rParam, idx: i}}}}
	}
	base := &state{pub: map[ast.Node]bool{}, spawned: true}
	saved := w.conc
	w.conc = w.conc || w.a.concurrentTask(t)
	w.apply(f, args, e.Pos(), "the task started by "+exprText(e.Fun), base, nil)
	w.conc = saved
	w.st.spawned = true
	w.sum.maySpawn = true
}

// concurrentTask reports whether the code of task t may run while other
// code of its starter does: t is started in a loop, the starter starts
// other tasks too, or it calls a func value itself, which t may call too.
func (a *analyzer) concurrentTask(t *scoped.Task) bool {
	if len(t.Loops) > 0 {
		return true
	}
	n := 0
	for _, o := range a.scoped.Tasks {
		if o.Func == t.Func {
			n++
		}
	}
	if n > 1 {
		return true
	}
	var body ast.Node
	switch f := t.Func.(type) {
	case *ast.FuncDecl:
		body = f.Body
	case *ast.FuncLit:
		body = f.Body
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && a.scoped.Lits[lit] != nil {
			return false
		}
		if c, ok := n.(*ast.CallExpr); ok && !a.isStaticCall(c) {
			found = true
		}
		return !found
	})
	return found
}

// A concCall is a func value that a callee calls from several scoped tasks
// at once: it must not write what it captures, or package variables,
// without a lock.
type concCall struct {
	f     *function
	pos   token.Pos // the call that passes it
	name  string    // the callee
	param string    // its parameter
}

// noteConcCall records the functions and literals fv may hold, called from
// several tasks at once by the callee name, called at pos.
func (a *analyzer) noteConcCall(fv origins, pos token.Pos, name, param string) {
	for _, o := range funcOrigins(fv) {
		f := a.rootFunc(o)
		if f == nil {
			continue
		}
		k := fmt.Sprintf("%p|%d", f, pos)
		if a.concSeen == nil {
			a.concSeen = map[string]bool{}
		}
		if a.concSeen[k] {
			continue
		}
		a.concSeen[k] = true
		a.concCalls = append(a.concCalls, concCall{f: f, pos: pos, name: name, param: param})
	}
}

// checkConcCalls checks each function that runs in several tasks at once
// against itself: E3406 for a write, without a lock, of a variable it
// captures or of a package variable.
func (a *analyzer) checkConcCalls() {
	for _, c := range a.concCalls {
		if c.f.body == nil {
			continue
		}
		fc := &frameCheck{a: a, starter: c.f, scopeBit: map[*types2.Var]uint{}, byCall: map[*ast.CallExpr]*party{}, varIDs: map[*types2.Var]int{}, record: true}
		fc.unit = &unit{vars: map[*types2.Var]origins{}}
		for v, o := range c.f.unit.vars {
			fc.unit.vars[v] = append(origins(nil), o...)
		}
		p := &party{idx: 1, seen: map[string]bool{}, self: true}
		if c.f.lit != nil {
			p.lits = []*ast.FuncLit{c.f.lit}
		}
		fc.parties = []*party{{idx: 0, seen: map[string]bool{}}, p}
		w := fc.walker(p, c.f)
		for _, prm := range c.f.params {
			// Its own parameters: what they point to may differ from one
			// call to the next.
			w.setVar(prm, origins{{vp: vpath{root: root{kind: rUnknown, v: prm}}}})
		}
		w.st.spawned = true
		w.gotos = gotoTargets(c.f.body)
		w.block(c.f.body)
		w.finish()
		fc.expandEdges()
		for _, x := range p.accs {
			if !x.write || x.atomic {
				continue
			}
			if r := x.place.root; r.kind == rUnknown || r.kind == rParam || r.kind == rFresh {
				continue
			} else if r.kind == rLocal && fnSpan(c.f).Pos() <= r.v.Pos() && r.v.Pos() < fnSpan(c.f).End() {
				continue // its own variable
			}
			conflicted := false
			for _, y := range p.accs {
				if meets, _ := fc.meet(x, y, p, p, true); meets && !fc.common(x, y, p, p, true) {
					conflicted = true
					break
				}
			}
			if conflicted {
				fc.reportConc(c, x)
				break
			}
		}
	}
}

// scopeVar returns the Scope variable a Wait or Drop call names directly.
func (w *walker) scopeVar(recv ast.Expr) *types2.Var {
	return scoped.LocalScopeVar(w.a.uses, recv)
}

// newScope notes the declaration of a variable: when it is a Scope of the
// starter's, the previous Scope it held, in an earlier iteration of a loop
// around it, has joined its tasks where its block ended.
func (w *walker) newScope(v *types2.Var) {
	if w.frame == nil || w.party.task != nil {
		return
	}
	if b, ok := w.frame.scopeBit[v]; ok {
		w.st.running = w.st.running.keep(^(uint64(1) << b))
	}
}

// scopeWait walks s.Wait() or s.Drop(): the starter's tasks of s join.
func (w *walker) scopeWait(v *types2.Var) {
	if w.frame == nil || w.party.task != nil {
		return
	}
	if b, ok := w.frame.scopeBit[v]; ok {
		w.st.running = w.st.running.keep(^(uint64(1) << b))
	}
}

// inlineLit walks a function literal of the starter's unit where it is
// called (or, for a task, started), so that its uses of the frame are seen
// with the caller's locks (or none). args are its arguments.
func (w *walker) inlineLit(lit *ast.FuncLit, args []origins, task bool) []origins {
	f := w.a.lits[lit]
	if f == nil || w.inlined > maxDepth {
		return nil
	}
	savedSt, savedExit, savedDefers, savedTargets, savedGotos, savedFn := w.st, w.exit, w.defers, w.targets, w.gotos, w.fn
	if task {
		w.st = &state{pub: map[ast.Node]bool{}, spawned: true}
	} else {
		w.st = w.st.clone()
	}
	w.exit, w.defers, w.targets, w.gotos = nil, nil, nil, gotoTargets(lit.Body)
	w.fn = f
	w.inlined++
	for i, p := range f.params {
		if i < len(args) {
			w.setVar(p, args[i])
		}
	}
	w.block(lit.Body)
	w.exit = join(w.exit, w.st)
	w.st = w.exit
	if !w.st.dead {
		for i := len(w.defers) - 1; i >= 0; i-- {
			w.deferred(w.defers[i])
		}
	}
	var res []origins
	if f.sig != nil {
		for i := 0; i < f.sig.Results().Len(); i++ {
			res = append(res, w.u.vars[f.sig.Results().At(i)])
		}
	}
	after := w.st
	w.inlined--
	w.st, w.exit, w.defers, w.targets, w.gotos, w.fn = savedSt, savedExit, savedDefers, savedTargets, savedGotos, savedFn
	if !task && !after.dead {
		w.st.locks, w.st.rel = after.locks, after.rel
	}
	return res
}

// noteSub records, in a check of scoped tasks, a use that a callee makes
// of what its parameters reach.
func (fc *frameCheck) noteSub(w *walker, p vpath, x *access, locks lockset, call token.Pos) {
	fc.note(w, &frameAcc{place: p, write: x.write, atomic: x.atomic, locks: locks, pos: x.pos, via: call, typ: x.typ})
}

// noteEdge records a call whose callee's uses of package variables and
// synced objects are expanded at the end.
func (fc *frameCheck) noteEdge(w *walker, f *function, args []origins, locks lockset, call token.Pos) {
	if !fc.record || w.quiet > 0 {
		return
	}
	e := frameEdge{party: w.party, f: f, args: args, locks: locks, pos: call}
	if w.party.task == nil {
		e.run = w.running(call)
	}
	fc.edges = append(fc.edges, e)
}

// ----------------------------------------------------------------------------
// Parts

// indexPart returns the part step for the element an index expression
// selects: a constant, or in a check of scoped tasks the loop variable of
// the task being walked.
func (w *walker) indexPart(idx ast.Expr) string {
	if tv, ok := w.a.types[idx]; ok && tv.Value != nil {
		if n, exact := constant.Int64Val(constant.ToInt(tv.Value)); exact {
			return "@c" + itoa(int(n))
		}
		return ""
	}
	if v := w.taskIndex(idx); v != nil {
		return "@t" + itoa(w.frame.varID(v))
	}
	if w.frame != nil {
		if m := w.splitVar(idx); m != nil {
			return "@e" + itoa(w.frame.varID(m))
		}
	}
	return ""
}

// taskIndex returns the loop variable e names if it tells instances of the
// task being walked apart: one of its required variables.
func (w *walker) taskIndex(e ast.Expr) *types2.Var {
	if w.frame == nil || w.party == nil || w.party.task == nil {
		return nil
	}
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil
	}
	v, ok := w.a.uses[id].(*types2.Var)
	if !ok || !w.party.required[v] {
		return nil
	}
	return v
}

// windowIndex is taskIndex for the arithmetic of a window, which lowering
// checks for overflow only for int and int64 (internal/scoped.Checked).
func (w *walker) windowIndex(e ast.Expr) *types2.Var {
	if v := w.taskIndex(e); v != nil && scoped.IsInt64(v.Type()) {
		return v
	}
	return nil
}

// slicePart returns the part step for the elements a slice expression
// selects, in a check of scoped tasks: a window of a task's loop variable,
// or one side of a split at a variable.
func (w *walker) slicePart(e *ast.SliceExpr) string {
	if w.frame == nil || e.Slice3 {
		return ""
	}
	// Windows: x[i*k : i*k+k], x[i*k : (i+1)*k], x[i : i+c], x[i : min(i+c, n)].
	if e.Low != nil && e.High != nil {
		if i, k := w.mulIndex(e.Low); i != nil {
			if w.isWindowEnd(e.High, i, k) {
				return "@k" + itoa(w.frame.varID(i)) + "_" + w.stepKey(k)
			}
		}
		if i := w.windowIndex(e.Low); i != nil {
			if c := w.addend(e.High, i); c != nil && w.loopStepIs(i, c) {
				return "@w" + itoa(w.frame.varID(i)) + "_" + w.stepKey(c)
			}
		}
	}
	// Splits: x[:m] (and x[a:m]) and x[m:] or x[m+d:] (and x[m:b]).
	if e.High != nil {
		if m := w.splitVar(e.High); m != nil {
			return "@lo" + itoa(w.frame.varID(m))
		}
	}
	if e.Low != nil {
		if m := w.splitVar(e.Low); m != nil {
			return "@hi" + itoa(w.frame.varID(m)) + "_0"
		}
		if b, ok := ast.Unparen(e.Low).(*ast.BinaryExpr); ok && b.Op == token.ADD {
			if m := w.splitVar(b.X); m != nil {
				if d, ok := w.posConst(b.Y); ok {
					return "@hi" + itoa(w.frame.varID(m)) + "_" + itoa(int(d))
				}
			}
		}
	}
	return ""
}

// splitVar returns the local variable e names if it can mark a split: not
// one that the party being walked takes.
func (w *walker) splitVar(e ast.Expr) *types2.Var {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil
	}
	v, ok := w.a.uses[id].(*types2.Var)
	if !ok || isGlobal(v) || w.party.taken[v] {
		return nil
	}
	// A task's own variable may hold a different value in each instance.
	for _, p := range w.frame.parties {
		if p.own(root{kind: rLocal, v: v}) {
			return nil
		}
	}
	return v
}

// posConst returns e's value if it is a positive integer constant.
func (w *walker) posConst(e ast.Expr) (int64, bool) {
	tv, ok := w.a.types[e]
	if !ok || tv.Value == nil {
		return 0, false
	}
	n, exact := constant.Int64Val(constant.ToInt(tv.Value))
	return n, exact && n > 0
}

// mulIndex matches i*k or k*i, with i a task's loop variable and k a
// positive constant or a variable.
func (w *walker) mulIndex(e ast.Expr) (*types2.Var, ast.Expr) {
	b, ok := ast.Unparen(e).(*ast.BinaryExpr)
	if !ok || b.Op != token.MUL {
		return nil, nil
	}
	if i := w.windowIndex(b.X); i != nil && w.stepKey(b.Y) != "" {
		return i, b.Y
	}
	if i := w.windowIndex(b.Y); i != nil && w.stepKey(b.X) != "" {
		return i, b.X
	}
	return nil, nil
}

// isWindowEnd matches i*k+k and (i+1)*k.
func (w *walker) isWindowEnd(e ast.Expr, i *types2.Var, k ast.Expr) bool {
	b, ok := ast.Unparen(e).(*ast.BinaryExpr)
	if !ok {
		return false
	}
	switch b.Op {
	case token.ADD:
		if j, k2 := w.mulIndex(b.X); j == i && w.stepKey(k2) == w.stepKey(k) && w.stepKey(b.Y) == w.stepKey(k) {
			return true
		}
	case token.MUL:
		for _, pair := range [][2]ast.Expr{{b.X, b.Y}, {b.Y, b.X}} {
			s, ok := ast.Unparen(pair[0]).(*ast.BinaryExpr)
			if !ok || s.Op != token.ADD || w.stepKey(pair[1]) != w.stepKey(k) {
				continue
			}
			if w.windowIndex(s.X) == i {
				if n, ok := w.posConst(s.Y); ok && n == 1 {
					return true
				}
			}
		}
	}
	return false
}

// addend matches i+c and min(i+c, n) and returns c.
func (w *walker) addend(e ast.Expr, i *types2.Var) ast.Expr {
	e = ast.Unparen(e)
	if c, ok := e.(*ast.CallExpr); ok && len(c.Args) >= 1 {
		if id, ok := ast.Unparen(c.Fun).(*ast.Ident); ok {
			if b, ok := w.a.uses[id].(*types2.Builtin); ok && b.Name() == "min" {
				for _, a := range c.Args {
					if x := w.addend(a, i); x != nil {
						return x
					}
				}
			}
		}
		return nil
	}
	b, ok := e.(*ast.BinaryExpr)
	if !ok || b.Op != token.ADD {
		return nil
	}
	if w.windowIndex(b.X) == i && w.stepKey(b.Y) != "" {
		return b.Y
	}
	if w.windowIndex(b.Y) == i && w.stepKey(b.X) != "" {
		return b.X
	}
	return nil
}

// stepKey names a window size or step: a positive constant, or a local
// variable (whose value lowering checks is positive where the loop's step
// is, internal/scoped.LoopVar).
func (w *walker) stepKey(e ast.Expr) string {
	if n, ok := w.posConst(e); ok {
		return "c" + itoa(int(n))
	}
	if id, ok := ast.Unparen(e).(*ast.Ident); ok {
		if v, ok := w.a.uses[id].(*types2.Var); ok && !isGlobal(v) {
			return "v" + itoa(w.frame.varID(v))
		}
	}
	return ""
}

// loopStepIs reports whether the loop of variable i steps by c.
func (w *walker) loopStepIs(i *types2.Var, c ast.Expr) bool {
	for _, L := range w.party.task.Loops {
		if L.Var == i {
			return L.Step != nil && w.stepKey(L.Step) == w.stepKey(c)
		}
	}
	return false
}

// selfDisjoint reports whether two uses by different instances of party p
// select parts that tell the instances apart: parts of each of p's
// required variables, identical in both.
func (fc *frameCheck) selfDisjoint(p *party, pa, pb string) bool {
	if p.partial || len(p.required) == 0 || strings.IndexByte(pa, '@') < 0 {
		return false
	}
	covered := map[int]bool{}
	alignParts(pa, pb, func(x, y string) bool {
		if x == y && (x[1] == 't' || x[1] == 'k' || x[1] == 'w') {
			n := 0
			for k := 2; k < len(x) && x[k] >= '0' && x[k] <= '9'; k++ {
				n = n*10 + int(x[k]-'0')
			}
			covered[n] = true
		}
		return false
	})
	for v := range p.required {
		if !covered[fc.varIDs[v]] {
			return false
		}
	}
	return true
}

// splitApart reports whether two part steps cannot meet at a split at
// one variable m: x[:m] and x[m:] (or x[m+d:]), and the element x[m] and
// x[:m] or x[m+d:] with d at least 1.
func splitApart(x, y string) bool {
	if strings.HasPrefix(y, "@lo") || strings.HasPrefix(x, "@hi") {
		x, y = y, x
	}
	switch {
	case strings.HasPrefix(x, "@lo"):
		m := x[3:]
		if strings.HasPrefix(y, "@hi") {
			n, _, _ := strings.Cut(y[3:], "_")
			return m == n
		}
		return strings.HasPrefix(y, "@e") && y[2:] == m
	case strings.HasPrefix(x, "@e") && strings.HasPrefix(y, "@hi"):
		n, d, _ := strings.Cut(y[3:], "_")
		return n == x[2:] && d != "0"
	}
	return false
}

// ----------------------------------------------------------------------------
// Conflicts

// concurrent reports whether parties p and q may run at the same time.
func (fc *frameCheck) concurrent(p, q *party) bool {
	if p == q {
		return p.task != nil && (p.start.has(p.idx) || p.self)
	}
	return p.start.has(q.idx) || q.start.has(p.idx)
}

// external reports whether a place is memory the starter's caller, the
// heap or a package variable provides through a view: it may be the same
// memory as another such place reached another way.
func external(p vpath) bool {
	switch p.root.kind {
	case rParam, rUnknown, rGlobal:
		return strings.ContainsAny(p.path, "*%$#")
	}
	return false
}

// typesMeet reports whether memory of types S and T may overlap: one is
// the other, or holds it in a field or element.
func typesMeet(S, T types2.Type) bool {
	if S == nil || T == nil {
		return false
	}
	return holds(S, T, 0) || holds(T, S, 0)
}

func holds(S, T types2.Type, depth int) bool {
	if types2.Identical(S, T) {
		return true
	}
	if depth > 6 {
		return false
	}
	switch u := underlying(S).(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if holds(u.Field(i).Type(), T, depth+1) {
				return true
			}
		}
	case *types2.Array:
		return holds(u.Elem(), T, depth+1)
	}
	return false
}

// meet reports whether uses x (by p) and y (by q) may reach the same
// memory; self says they are by different instances of one task. alias
// says they meet only because two views may point to the same memory.
func (fc *frameCheck) meet(x, y *frameAcc, p, q *party, self bool) (meets, alias bool) {
	if x.place.root == y.place.root {
		pa, pb := stripOwned(x.place.path), stripOwned(y.place.path)
		sa, sb := stripParts(pa), stripParts(pb)
		if !within(sa, sb) && !within(sb, sa) {
			return false, false
		}
		if partsDisjoint(pa, pb, splitApart) {
			return false, false
		}
		if self && fc.selfDisjoint(p, pa, pb) {
			return false, false
		}
		return true, false
	}
	if external(x.place) && external(y.place) && typesMeet(x.typ, y.typ) {
		return true, true
	}
	return false, false
}

// common reports whether x and y hold one lock that keeps them apart: held
// by both, exclusively by a writer, and the same mutex for both (not one
// of a single instance's own).
func (fc *frameCheck) common(x, y *frameAcc, p, q *party, self bool) bool {
	for _, l := range x.locks {
		i := y.locks.find(l.place)
		if i < 0 || isRegion(l.place) {
			continue // a region (section 19.3) excludes only the same processor
		}
		m := y.locks[i]
		if x.write && !l.excl || y.write && !m.excl {
			continue
		}
		if self && p.own(l.place.root) || q.own(l.place.root) || p.own(l.place.root) {
			continue
		}
		return true
	}
	return false
}

// A conflictRec is a race found, for messages.
type conflictRec struct {
	x, y  *frameAcc
	p, q  *party
	self  bool
	alias bool
}

// check reports the races among the parties.
func (fc *frameCheck) check() {
	ps := fc.parties
	for i, p := range ps {
		for _, q := range ps[i:] {
			switch {
			case q.task == nil:
			case p.task == nil:
				fc.pair(p, q, false)
			case fc.concurrent(p, q):
				fc.pair(p, q, p == q)
			}
		}
	}
	for _, p := range ps[1:] {
		fc.checkTakenWrites(p)
	}
}

// pair checks the uses of parties p and q (the starter first, if one is).
func (fc *frameCheck) pair(p, q *party, self bool) {
	// Conflicts through views that may alias are reported only where the
	// parties have no other: the other is the clearer error.
	var aliases []conflictRec
	found := false
	for _, x := range p.accs {
		for _, y := range q.accs {
			if !x.write && !y.write || x.atomic && y.atomic {
				continue
			}
			if p.task == nil {
				// The starter: q must be running where x is.
				if !x.run.has(q.idx) {
					continue
				}
			}
			meets, alias := fc.meet(x, y, p, q, self)
			if !meets || fc.common(x, y, p, q, self) {
				continue
			}
			if alias && self && fc.sameForm(x, y, p) {
				continue
			}
			c := conflictRec{x: x, y: y, p: p, q: q, self: self, alias: alias}
			if alias {
				aliases = append(aliases, c)
				continue
			}
			found = true
			fc.report(c)
		}
	}
	if !found && len(aliases) > 0 {
		fc.report(aliases[0])
	}
}

// noteView records that index expression e indexes a slice variable, for
// sameForm.
func (w *walker) noteView(e *ast.IndexExpr) {
	id, ok := ast.Unparen(e.X).(*ast.Ident)
	if !ok {
		return
	}
	if v, ok := w.a.uses[id].(*types2.Var); ok {
		w.a.views[e.Pos()] = v
	}
}

// sameForm reports whether the uses x and y of two instances of task p, which
// reach the same type of memory through views that may alias, are accepted
// without a conflict: each indexes a slice variable the task's literal uses
// (and does not declare) in the same partition form of the loops that start
// the task, and nothing more of the memory behind it. Lowering then checks,
// before each task starts, that the two slices start at the same element or
// do not overlap (language spec section 19.4, Memory reached through
// views), and sameForm records that check.
func (fc *frameCheck) sameForm(x, y *frameAcc, p *party) bool {
	a, b := fc.a.views[x.pos], fc.a.views[y.pos]
	if a == nil || b == nil || a == b || p.task == nil {
		return false
	}
	ea, oka := underlying(a.Type()).(*types2.Slice)
	eb, okb := underlying(b.Type()).(*types2.Slice)
	if !oka || !okb || !types2.Identical(ea.Elem(), eb.Elem()) {
		return false
	}
	for _, v := range []*types2.Var{a, b} {
		if p.own(root{kind: rLocal, v: v}) {
			return false
		}
	}
	fa, fb := viewForm(x.place.path), viewForm(y.place.path)
	if fa == "" || fa != fb || !fc.selfDisjoint(p, x.place.path, y.place.path) {
		return false
	}
	// Check the pair in a fixed order, once.
	if a.Pos() > b.Pos() {
		a, b = b, a
	}
	call := p.task.Call
	for _, o := range fc.a.overlaps[call] {
		if o.A == a && o.B == b {
			return true
		}
	}
	fc.a.overlaps[call] = append(fc.a.overlaps[call], scoped.Overlap{A: a, B: b})
	return true
}

// viewForm returns the part that path selects of the elements of a slice, if
// path is no more than that: the elements of the slice (%), one of the
// partition forms of the loops that start tasks, and fields and array
// elements within an element. It returns "" for a path that leaves the
// elements' own storage.
func viewForm(path string) string {
	if !strings.HasPrefix(path, "%@") {
		return ""
	}
	form := ""
	for i, st := range steps(path) {
		switch {
		case i == 0:
		case i == 1:
			if st[1] != 't' && st[1] != 'k' && st[1] != 'w' {
				return ""
			}
			form = st
		case st[0] == '.' || st[0] == '[' || st[0] == '@':
		default:
			return ""
		}
	}
	return form
}

// fnSpan returns the syntax of a function, signature included.
func fnSpan(f *function) ast.Node {
	if f.lit != nil {
		return f.lit
	}
	return f.decl
}
