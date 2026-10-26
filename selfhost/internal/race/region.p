// Translated by go2pu from internal/race/region.go at the go-cutover tag; edited by hand since.

package race

import (
	"pluto/sort"
	"pluto/strings"
	"selfhost/compat"
	"selfhost/internal/diag"
	"selfhost/internal/directive"
	"selfhost/internal/load"
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
	typerr "selfhost/internal/types2/errors"
	"selfhost/pu"
)

// Regions and per-CPU variables (language spec sections 19.3 and 20.9).
//
// A region is a stretch of code where, on every path, the task cannot move
// to another processor (preemption disabled, maskable interrupts disabled,
// an rt.Lock held), or where interrupts are disabled (the last two, and the
// body of an interrupt handler up to a call that may switch tasks). Regions
// are found the way held locks are, as pseudo locks of the lock set: on every
// path, across calls, and with a deferred release counted at the end of the
// function. They never guard data (globalLock and the scoped check skip
// them); the checks of this file read them.
//
// A per-CPU variable (//pu:percpu) is exempt from rule 1 of section 19.3.
// Each use of one is classified when the program is scanned (perCPUUse):
// a lone load or store of a whole variable of at most a machine word is
// allowed anywhere; every other use needs a region (E2022), and a pointer
// taken with &v is tagged with the regions held where it was taken and may
// be dereferenced only while one of them is held (E2023), and not be stored
// anywhere but in local variables, sent, passed to a task or returned.

// The kinds of region.
const (
	regPreempt = iota // rt.DisablePreemption to rt.EnablePreemption
	regIRQ            // maskable interrupts disabled, or the body of a handler
	regLock           // an rt.Lock is held (in kernel mode that disables interrupts too)
)

// regAll, returned by regionOp, ends the regions in which the task cannot
// move to another processor and those with interrupts disabled: the task may
// be switched out here and resume on any processor.
const regAll = -1

// regionAt names the region of kind k opened at pos.
func regionAt(k int, pos token.Pos) vpath {
	return vpath{root: root{kind: rRegion, idx: k, pos: pos}}
}

// regionAny names every region of kind k, as the caller may hold one from
// any site: a release of the kind releases them all.
func regionAny(k int) vpath { return regionAt(k, token.NoPos) }

// isRegion reports whether a place is a region.
func isRegion(p vpath) bool { return p.root.kind == rRegion }

// withoutKind returns s without the regions of kind k.
func (s lockset) withoutKind(k int) lockset {
	out := lockset(compat.MakeCap[lock](0, len(s)))
	for _, l := range s {
		if l.place.root.kind != rRegion || l.place.root.idx != k {
			out = lockset(compat.Append(out, l))
		}
	}
	if len(out) == len(s) {
		return s
	}
	return out
}

// regionHeld reports whether s holds a region of one of the kinds.
func (s lockset) regionHeld(irq bool) bool {
	for _, l := range s {
		if l.place.root.kind != rRegion {
			continue
		}
		if !irq || l.place.root.idx != regPreempt {
			return true
		}
	}
	return false
}

// A cpuClass says what a use of a per-CPU variable needs.
type cpuClass uint8

const (
	cpuLone   cpuClass = iota // a lone load or store of a whole variable of at most a word: allowed anywhere
	cpuRegion                 // needs a non-migrating region, or an interrupt-disabled one when a handler uses the variable
	cpuDeref                  // through a pointer from &v: needs one of the regions the pointer was taken in
)

// A cpuUse is one use of a per-CPU variable, classified.
type cpuUse struct {
	v      *types2.Var
	class  cpuClass
	reason string // why it needs a region: "increment", "larger than a machine word"
	text   string // the access as written
	ptr    bool   // the use takes the variable's address
	tag    int    // cpuDeref: the regions the pointer was taken in (regionSet)
	pos    token.Pos

	// cpuDeref through a parameter of a callee: the pointer came from the
	// caller, so only a region the caller held when it called, and the callee
	// has not released, is the region it was taken in. A region the callee
	// opened itself is another instance of it, even at the same site
	// (recursion).
	inhOnly bool
	inh     lockset
	lone    bool // cpuDeref: a store of a whole variable of at most a machine word

	// A store that depends on a load of the same variable (dep.go): the load
	// and the store must be inside one region. own are the regions this
	// function opened that are held at the store, and spans, for each load,
	// those held at the load too. A load that was not seen on the way here
	// (unspan) is in no region the store can share.
	dep    *cpuDepSite
	own    lockset
	spans  []lockset
	unspan bool
	task   bool // the store is made by a task started here
}

func (u *cpuUse) key() string {
	k := compat.Sprintfb("%d|%d|%d|%s|%v", compat.ArgI(int64(u.pos)), compat.ArgU(uint64(u.class)), compat.ArgI(int64(u.tag)), compat.ArgS(u.reason), compat.ArgB(u.lone))
	if u.dep != nil {
		k = compat.Concat(k, compat.Concat("|dep|", u.own.String(), "|", compat.Sprintfb("%v %v", compat.ArgB(u.unspan), compat.ArgB(u.task))))
		for _, sp := range u.spans {
			k = compat.Concat(k, compat.Concat("|", sp.String()))
		}
	}
	if u.inhOnly {
		k = compat.Concat(k, compat.Concat("|", u.inh.String()))
	}
	return k
}

// regionSet interns the set of regions held where a pointer to a per-CPU
// variable was taken, and returns its number (never 0).
func (a *analyzer) regionSet(locks lockset) int {
	var held lockset
	for _, l := range locks {
		if isRegion(l.place) {
			held = held.with(l.place, true)
		}
	}
	k := held.String()
	if id, ok := a.regionIDs[k]; ok {
		return id
	}
	a.regionSets = compat.Append(a.regionSets, held)
	id := len(a.regionSets)
	a.regionIDs[k] = id
	return id
}

// ----------------------------------------------------------------------------
// Scanning

// scanPerCPU finds the per-CPU variables outside the runtime (the runtime's
// own are not checked, as its other variables are not: section 19.3) and
// classifies every use of them.
func (a *analyzer) scanPerCPU() {
	a.cpuVars = map[*types2.Var]bool{}
	for _, p := range a.prog.Packages {
		if p.Path == "pluto/rt" || strings.HasPrefix(p.Path, "pluto/rt/") {
			continue
		}
		for _, f := range p.Files {
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					continue
				}
				for _, spec := range gd.Specs {
					vs := spec.(*ast.ValueSpec)
					doc := vs.Doc
					if doc == nil {
						doc = gd.Doc
					}
					if !hasPerCPU(doc) {
						continue
					}
					for _, n := range vs.Names {
						if v, ok := p.Info.Defs[n].(*types2.Var); ok {
							a.cpuVars[v] = true
						}
					}
				}
			}
		}
	}
	if len(a.cpuVars) == 0 {
		return
	}
	a.cpuOn = true
	a.cpuUses = map[token.Pos]*cpuUse{}
	a.regionIDs = map[string]int{}
	sizes := types2.SizesFor("gc", "amd64")
	for _, p := range a.prog.Packages {
		for _, f := range p.Files {
			var stack []ast.Node
			ast.Inspect(f, func(n ast.Node) bool {
				if n == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				if id, ok := n.(*ast.Ident); ok {
					if v, ok := p.Info.Uses[id].(*types2.Var); ok && a.cpuVars[v] {
						a.cpuUses[id.Pos()] = a.classify(p, sizes, stack, id, v)
					}
				}
				stack = compat.Append(stack, n)
				return true
			})
		}
	}
	a.cpuCallLoads = map[*ast.CallExpr][]int{}
	a.scanDeps()
}

func hasPerCPU(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		if d, ok := strings.CutPrefix(c.Text, "//pu:"); ok && directive.Normalize(d) == "percpu" {
			return true
		}
	}
	return false
}

// classify decides what the use of v at id needs. stack holds the nodes
// around id, outermost first.
func (a *analyzer) classify(p *load.Package, sizes types2.Sizes, stack []ast.Node, id *ast.Ident, v *types2.Var) *cpuUse {
	u := pu.New(cpuUse{v: v, class: cpuLone, pos: id.Pos(), text: id.Name})
	var cur ast.Expr = id
	i := len(stack) - 1
	// A qualified name pkg.v is the variable.
	if i >= 0 {
		if se, ok := stack[i].(*ast.SelectorExpr); ok && se.Sel == id && p.Info.Selections[se] == nil {
			cur = se
			u.text = types2.ExprString(se)
			i--
		}
	}
	typeOf := func(e ast.Expr) types2.Type {
		if tv, ok := p.Info.Types[e]; ok {
			return tv.Type
		}
		return nil
	}
	part := false
	ptr := false
	// Climb the selections, indexes and slices that make a part of the
	// variable, or take its address.
climb:
	for ; i >= 0; i-- {
		switch s := stack[i].(type) {
		case *ast.ParenExpr:
			cur = s
		case *ast.SelectorExpr:
			if s.X != cur {
				break climb
			}
			sel := p.Info.Selections[s]
			if sel == nil {
				break climb
			}
			switch sel.Kind() {
			case types2.FieldVal:
				if pointerElem(typeOf(cur)) != nil {
					break climb // the field of what a pointer variable points to
				}
				part = true
				cur = s
			case types2.MethodVal:
				if fn, ok := sel.Obj().(*types2.Func); ok {
					if r := fn.Type().(*types2.Signature).Recv(); r != nil && pointerElem(r.Type()) != nil && pointerElem(typeOf(cur)) == nil {
						ptr = true
						u.reason = "a call of a method with a pointer receiver"
					}
				}
				cur = s
				i--
				break climb
			default:
				break climb
			}
		case *ast.IndexExpr:
			if s.X != cur {
				break climb
			}
			if _, ok := underlying(typeOf(cur)).(*types2.Array); !ok {
				break climb // an element of what a slice, map or pointer reaches
			}
			part = true
			cur = s
		case *ast.SliceExpr:
			if s.X != cur {
				break climb
			}
			if _, ok := underlying(typeOf(cur)).(*types2.Array); ok {
				ptr = true
				u.reason = "slicing the variable, which takes its address"
				cur = s
				i--
			}
			break climb
		case *ast.UnaryExpr:
			if s.Op == token.AND && s.X == cur {
				ptr = true
				u.reason = "taking its address"
				cur = s
				i--
			}
			break climb
		default:
			break climb
		}
	}
	u.ptr = ptr
	// What the enclosing statement does with it.
	var parent ast.Node
	if i >= 0 {
		parent = stack[i]
	}
	write, rmw, sameStmt, custom := false, false, false, false
	switch s := parent.(type) {
	case *ast.AssignStmt:
		for _, l := range s.Lhs {
			if l == cur {
				write = true
				if s.Tok != token.ASSIGN && s.Tok != token.DEFINE {
					rmw, custom = true, true
					u.text = compat.Concat(types2.ExprString(l), " ", s.Tok.String(), " ...")
				}
			}
		}
		if write && !rmw {
			sameStmt = a.mentions(p, s, id, v)
		}
	case *ast.IncDecStmt:
		write, rmw, custom = true, true, true
		u.text = compat.Concat(types2.ExprString(s.X), s.Tok.String())
	case *ast.RangeStmt:
		if s.Tok == token.ASSIGN && (s.Key == cur || s.Value == cur) {
			write = true
		}
	}
	switch {
	case ptr:
		u.class = cpuRegion
	case part:
		u.class, u.reason = cpuRegion, "an access to part of the variable"
	case sizes.Sizeof(v.Type()) > 8:
		u.class, u.reason = cpuRegion, "an access to a variable larger than a machine word"
	case rmw:
		u.class, u.reason = cpuRegion, "a compound assignment or increment, which reads and writes it"
	case write && sameStmt:
		u.class, u.reason = cpuRegion, "a read and a write in one statement"
	}
	if !custom {
		u.text = types2.ExprString(cur)
		if as, ok := parent.(*ast.AssignStmt); ok && write {
			u.text = compat.Concat(u.text, compat.Concat(" ", as.Tok.String(), " ..."))
		}
	}
	return u
}

// mentions reports whether a statement uses the variable v anywhere other
// than at id: a read and a write of it in one statement.
func (a *analyzer) mentions(p *load.Package, s ast.Node, id *ast.Ident, v *types2.Var) bool {
	found := false
	ast.Inspect(s, func(n ast.Node) bool {
		if o, ok := n.(*ast.Ident); ok && o != id && p.Info.Uses[o] == v {
			found = true
		}
		return !found
	})
	return found
}

// ----------------------------------------------------------------------------
// The flow analysis

// regionOp reports whether a call opens or closes a region: the runtime's
// preemption control and the interrupt flag instructions (spec section
// 19.3, which says these calls are not unsafe code and that the analysis
// follows them). A call that may switch tasks ends the interrupt-disabled
// body of a handler.
func (w *walker) regionOp(e *ast.CallExpr) (kind int, acquire, ok bool) {
	if !w.a.cpuOn {
		return 0, false, false
	}
	fn := w.calledFunc(e)
	if fn == nil || fn.Pkg() == nil || fn.Type().(*types2.Signature).Recv() != nil {
		return 0, false, false
	}
	switch fn.Pkg().Path() {
	case "pluto/rt":
		switch fn.Name() {
		case "DisablePreemption":
			return regPreempt, true, true
		case "EnablePreemption":
			return regPreempt, false, true
		case "Yield", "Preempt":
			return regAll, false, true
		}
	case "pluto/arch/x86":
		switch fn.Name() {
		case "Cli", "SaveFlagsCli":
			return regIRQ, true, true
		case "Sti", "StiHlt", "RestoreFlags":
			return regIRQ, false, true
		}
	}
	return 0, false, false
}

// parkPoint ends the regions in which the task cannot move to another
// processor, and those with interrupts disabled, at an operation that may
// switch to another task: a yield, a wait on a channel, a call of the
// runtime's park, which every wait in the library ends in. The task may
// resume on another processor (section 19.3).
func (w *walker) parkPoint() {
	if !w.a.cpuOn || w.noPark > 0 {
		return
	}
	w.region(regPreempt, false, token.NoPos)
	w.region(regIRQ, false, token.NoPos)
}

// isBlocking reports whether a function is marked //pu:blocking: the
// runtime's park.
func (a *analyzer) isBlocking(fn *types2.Func) bool {
	f := a.funcs[fn.Origin()]
	if f == nil || f.decl == nil || f.decl.Doc == nil {
		return false
	}
	for _, c := range f.decl.Doc.List {
		if n, _, ok := directive.Split(c.Text); ok && n == "blocking" {
			return true
		}
	}
	return false
}

// region opens a region of kind k here, or closes the regions of the kind.
func (w *walker) region(k int, acquire bool, pos token.Pos) {
	any := regionAny(k)
	if acquire {
		w.st.locks = w.st.locks.with(regionAt(k, pos), true)
		w.noteLocked(any)
		return
	}
	w.st.locks = w.st.locks.withoutKind(k)
	if !w.lockedHere[any] {
		w.st.rel = w.st.rel.with(any)
	}
}

// isRTLock reports whether the method selected is of the runtime's lock,
// used outside the runtime.
func (w *walker) isRTLock(se *ast.SelectorExpr) bool {
	if !w.a.cpuOn || w.inRuntime() {
		return false
	}
	sel := w.a.selections[se]
	if sel == nil {
		return false
	}
	fn, ok := sel.Obj().(*types2.Func)
	if !ok {
		return false
	}
	n := recvNamed(fn)
	return n != nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "pluto/rt" && n.Obj().Name() == "Lock"
}

// cpuSite records the use of a per-CPU variable at id.
func (w *walker) cpuSite(id *ast.Ident, v *types2.Var) {
	if w.frame != nil || !w.a.cpuVars[v] {
		return
	}
	if n, ok := w.a.cpuLoadAt[id.Pos()]; ok && w.a.cpuLoads[n].feeds {
		// A later store depends on this load: note the regions it is in.
		w.st.setSnap(n, w.st.ownRegions())
	}
	if w.quiet > 0 {
		return
	}
	use := w.a.cpuUses[id.Pos()]
	if use == nil {
		use = pu.New(cpuUse{v: v, class: cpuRegion, reason: "an access the compiler cannot classify", text: id.Name, pos: id.Pos()})
	}
	if ds := w.a.cpuDeps[depKey{id.Pos(), v}]; ds != nil {
		use = w.depUse(use, ds)
	}
	w.sum.addAccess(pu.New(access{place: vpath{root: root{kind: rGlobal, v: v}}, locks: w.st.locks, rel: w.st.rel, pos: id.Pos(), pre: !w.st.spawned, cpu: use}))
}

// depUse returns the use of a per-CPU variable that stores a value, or runs
// under a condition, that depends on loads of the variable: a read-modify-write
// (section 20.9), which needs one region around the loads and the store.
func (w *walker) depUse(use *cpuUse, ds *cpuDepSite) *cpuUse {
	u := pu.New(*use)
	u.class = cpuRegion
	if use.class == cpuLone || use.reason == "" {
		u.reason = w.a.depReason(ds)
	}
	w.depAttach(u, ds)
	return u
}

// depAttach makes the use depend on the loads of a site: the regions this
// function holds at the store, and, for each load, those held at the load too.
func (w *walker) depAttach(u *cpuUse, ds *cpuDepSite) {
	u.dep = ds
	u.own = w.st.ownRegions()
	u.spans, u.unspan = nil, false
	for _, n := range ds.loads {
		held, ok := w.st.snaps[n]
		if !ok {
			u.unspan = true
			continue
		}
		var both lockset
		for _, l := range held {
			if u.own.find(l.place) >= 0 {
				both = both.with(l.place, true)
			}
		}
		u.spans = compat.Append(u.spans, both)
	}
}

// cpuCall notes the regions held at a call of a function that loads a per-CPU
// variable a later store depends on.
func (w *walker) cpuCall(e *ast.CallExpr) {
	if !w.a.cpuOn || w.frame != nil {
		return
	}
	for _, n := range w.a.cpuCallLoads[e] {
		if w.a.cpuLoads[n].feeds {
			w.st.setSnap(n, w.st.ownRegions())
		}
	}
}

// cpuCallStore records a call of a function that stores a per-CPU variable
// from a value, or under a condition, that depends on a load of it. It runs
// once the arguments are evaluated, so the loads among them are noted.
func (w *walker) cpuCallStore(e *ast.CallExpr) {
	if !w.a.cpuOn || w.frame != nil || w.quiet > 0 {
		return
	}
	for _, ds := range w.a.cpuDepCalls[e] {
		use := w.depUse(&cpuUse{v: ds.v, class: cpuRegion, text: compat.Concat("the call of ", ds.callee), pos: e.Pos()}, ds)
		w.sum.addAccess(pu.New(access{place: vpath{root: root{kind: rGlobal, v: ds.v}}, locks: w.st.locks, rel: w.st.rel, pos: e.Pos(), pre: !w.st.spawned, write: true, cpu: use}))
	}
}

// cpuTaskStore records a task statement that starts a function which stores
// a per-CPU variable from a value, or under a condition, that depends on a
// load of it. The task runs later, on any processor: no region of the task
// that starts it covers the store, however many it holds here.
func (w *walker) cpuTaskStore(c *ast.CallExpr) {
	if !w.a.cpuOn || w.frame != nil || w.quiet > 0 {
		return
	}
	for _, ds := range w.a.cpuDepCalls[c] {
		if !ds.task {
			continue
		}
		use := pu.New(cpuUse{v: ds.v, class: cpuRegion, text: compat.Concat("the task started with ", ds.callee), pos: c.Pos(), dep: ds, unspan: true, task: true})
		use.reason = w.a.depReason(ds)
		w.sum.addAccess(pu.New(access{place: vpath{root: root{kind: rGlobal, v: ds.v}}, pos: c.Pos(), write: true, cpu: use}))
	}
}

// spanned reports whether, for each load a dependent store depends on, a
// region that suits the store is held at the store and was held at the load
// too: one that this function opened and both are inside, or one its caller
// holds, which the function has not released between them.
func (u *cpuUse) spanned(locks lockset, irq bool) bool {
	if u.dep == nil {
		return true
	}
	if u.unspan {
		return false
	}
	for _, both := range u.spans {
		ok := false
		for _, l := range locks {
			if l.place.root.kind != rRegion || irq && l.place.root.idx == regPreempt {
				continue
			}
			if both.find(l.place) >= 0 || u.own.find(l.place) < 0 {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// hasDeref reports whether a path follows a pointer, slice or reference.
func hasDeref(p string) bool { return strings.ContainsAny(p, "*%$#") }

// cpuPlace handles a place whose root is a per-CPU variable. It returns the
// place to use for the rules of sharing, ok false when the access is the
// variable's own memory, which is a use checked here instead: by name (the
// walker recorded it at the identifier), or through a pointer from &v
// (recorded as a dereference).
//
// write says whether the access stores. inh, when not nil, is the set of
// regions the pointer may have been taken in, for an access made by a callee
// through a pointer its caller passed.
func (w *walker) cpuPlace(p vpath, pos token.Pos, locks lockset, rel released, pre, strict, write bool, inh *lockset, add func(*access)) (vpath, bool) {
	if p.root.kind != rGlobal || !w.a.cpuVars[p.root.v] {
		return p, true
	}
	if hasDeref(p.path) {
		// The memory a pointer in the variable points to is ordinary
		// shared memory.
		p.root.idx = 0
		return p, true
	}
	if p.root.idx != 0 || strict {
		u := pu.New(cpuUse{v: p.root.v, class: cpuDeref, tag: p.root.idx, pos: pos,
			text:   compat.Concat("the access through a pointer to ", p.root.v.Name()),
			reason: "an access through a pointer, which is not a lone store of a whole variable of at most a machine word",
			lone:   write && p.path == "" && types2.SizesFor("gc", "amd64").Sizeof(p.root.v.Type()) <= 8})
		if inh != nil {
			u.inhOnly, u.inh = true, *inh
		} else if ds := w.a.cpuDeps[depKey{pos, p.root.v}]; ds != nil && write {
			w.depAttach(u, ds)
		}
		add(pu.New(access{place: vpath{root: root{kind: rGlobal, v: p.root.v}}, locks: locks, rel: rel, pos: pos, pre: pre, write: write, cpu: u}))
	}
	return p, false
}

// addrsOfPlaces returns pointers to places ps; one to a per-CPU variable is
// tagged with the regions held now.
func (w *walker) addrsOfPlaces(ps []vpath) origins {
	var out origins
	for _, p := range ps {
		if p.root.kind == rGlobal && w.a.cpuVars[p.root.v] && !hasDeref(p.path) {
			p.root.idx = w.a.regionSet(w.st.locks)
		}
		out = origins(compat.Append(out, origin{vp: p.addr()}))
	}
	return out.uniq()
}

// publishAt records that val is stored where other code can reach it, and
// reports a pointer to a per-CPU variable that leaves its region this way.
func (w *walker) publishAt(val origins, pos token.Pos, how string) {
	w.cpuSink(val, pos, how)
	w.publish(val)
}

// cpuSink reports a pointer to a per-CPU variable kept where it can outlive
// its region (E2023).
func (w *walker) cpuSink(val origins, pos token.Pos, how string) {
	if !w.a.cpuOn || w.quiet > 0 || w.frame != nil || !pos.IsValid() {
		return
	}
	for _, o := range val {
		r := o.vp.root
		if r.kind != rGlobal || r.idx == 0 || !w.a.cpuVars[r.v] || hasDeref(o.vp.path) || !o.vp.isAddr() {
			continue
		}
		w.a.pointerEscapes(pos, r.v, how)
		return
	}
}

// pointerEscapes reports a pointer to v that is kept, sent or passed where
// the region it was taken in does not bound it.
func (a *analyzer) pointerEscapes(pos token.Pos, v *types2.Var, how string) {
	name := v.Name()
	d := diag.New(a.pos(pos), diag.CheckerCode(typerr.PerCPUPointer), "a pointer to the per-CPU variable %s is %s, outside the region it was taken in", pu.Box(name), pu.Box(how))
	d.Text = compat.Sprintfb("%s has one copy for each processor, and &%s points to the copy of the processor that runs this code now. The pointer is good only while the task cannot move to another processor, so it may be kept only in local variables and used only inside the region it was taken in (preemption or interrupts disabled, or an rt.Lock held). Here it is %s, where it can be used after the region ends or by another task.", compat.ArgS(name), compat.ArgS(name), compat.ArgS(how))
	d.Fix = pu.New(diag.Fix{Intro: "Use the variable by name inside the region, or copy the value out and keep the copy", Code: compat.Concat("rt.DisablePreemption()\ntotal := ", name, "\nrt.EnablePreemption()")})
	a.add(compat.Sprintfb("cpuptr|%d", compat.ArgI(int64(pos))), d)
}

// ----------------------------------------------------------------------------
// The check

// checkPerCPU applies section 20.9 to the uses of per-CPU variables that
// each task, handler and the main task make. Each is checked where the
// lock set at the use holds the region it needs: the one the task or
// handler is in at that point, however its callers set it up.
func (a *analyzer) checkPerCPU(uses []*cpuEntry, groups []*group) {
	// The handlers that use a variable make interrupts matter.
	handlers := map[*types2.Var][]string{}
	for _, g := range groups {
		if !g.handler {
			continue
		}
		for _, e := range g.cpu {
			v := e.x.cpu.v
			name := strings.TrimPrefix(strings.TrimPrefix(g.desc, "handler "), "the interrupt handler ")
			found := false
			for _, h := range handlers[v] {
				found = found || h == name
			}
			if !found {
				handlers[v] = compat.Append(handlers[v], name)
			}
		}
	}
	for _, e := range uses {
		x, u := e.x, e.x.cpu
		switch u.class {
		case cpuLone:
		case cpuRegion:
			hs := handlers[u.v]
			if x.locks.regionHeld(len(hs) > 0) && u.spanned(x.locks, len(hs) > 0) {
				continue
			}
			a.add(compat.Sprintfb("cpuacc|%d", compat.ArgI(int64(x.pos))), a.cpuAccessError(x, hs))
		case cpuDeref:
			if !a.pointerRegionHeld(x) {
				a.add(compat.Sprintfb("cpuptr|%d", compat.ArgI(int64(x.pos))), a.cpuDerefError(x, handlers[u.v]))
				continue
			}
			// The region bounds the pointer; a handler that uses the
			// variable also needs interrupts disabled, as for a use by name.
			if hs := handlers[u.v]; len(hs) > 0 && !u.lone && !x.locks.regionHeld(true) {
				a.add(compat.Sprintfb("cpuacc|%d", compat.ArgI(int64(x.pos))), a.cpuAccessError(x, hs))
				continue
			}
			// A store whose value or condition depends on a load of the
			// variable needs the load in the same region.
			irq := len(handlers[u.v]) > 0
			if u.dep != nil && (!x.locks.regionHeld(irq) || !u.spanned(x.locks, irq)) {
				a.add(compat.Sprintfb("cpuacc|%d", compat.ArgI(int64(x.pos))), a.cpuAccessError(x, handlers[u.v]))
			}
		}
	}
}

// pointerRegionHeld reports whether a region the pointer used by x was
// taken in is still held at the use.
func (a *analyzer) pointerRegionHeld(x *access) bool {
	if x.cpu.tag < 1 || x.cpu.tag > len(a.regionSets) {
		return false
	}
	set := a.regionSets[x.cpu.tag-1]
	if len(set) == 0 {
		return true // taken outside any region: reported there (E2022)
	}
	for _, r := range set {
		if x.locks.find(r.place) >= 0 && (!x.cpu.inhOnly || x.cpu.inh.find(r.place) >= 0) {
			return true
		}
	}
	return false
}

// A cpuEntry is a use of a per-CPU variable by one group.
type cpuEntry struct {
	x *access
	g *group
}

func (a *analyzer) cpuAccessError(x *access, handlers []string) *diag.Diagnostic {
	u := x.cpu
	name := a.cpuName(u.v)
	// A dependent store in a region that does not also hold the load.
	apart := u.dep != nil && x.locks.regionHeld(len(handlers) > 0)
	var d *diag.Diagnostic
	switch {
	case u.task:
		d = diag.New(a.pos(x.pos), diag.CheckerCode(typerr.PerCPUAccess), "%s stores the per-CPU variable %s in a task that runs later, from a value that depends on a load made here", pu.Box(u.text), pu.Box(name))
	case apart:
		d = diag.New(a.pos(x.pos), diag.CheckerCode(typerr.PerCPUAccess), "%s uses the per-CPU variable %s in a region that does not also hold the load it depends on", pu.Box(u.text), pu.Box(name))
	case len(handlers) > 0:
		d = diag.New(a.pos(x.pos), diag.CheckerCode(typerr.PerCPUAccess), "%s uses the per-CPU variable %s outside a region with interrupts disabled, which it needs because the interrupt handler %s also uses it", pu.Box(u.text), pu.Box(name), pu.Box(compat.Join(handlers, " and ")))
	default:
		d = diag.New(a.pos(x.pos), diag.CheckerCode(typerr.PerCPUAccess), "%s uses the per-CPU variable %s outside a region where the task cannot move to another processor", pu.Box(u.text), pu.Box(name))
	}
	reason := u.reason
	if reason == "" {
		reason = "an access other than a lone load or store of the whole variable"
	}
	b := pu.Alloc[compat.Builder]()
	compat.Fprintfb(b, "%s has one copy for each processor. This is %s, so the task could move to another processor in the middle of it and read one processor's copy and write another's.", compat.ArgS(name), compat.ArgS(reason))
	if u.task {
		b.WriteString(compat.Concat(" The task runs later and on any processor, so no region held here covers its store: the task could not have read the value it stores from this processor's copy at the moment it stores it. Pass the task what it needs without loading ", name, " first, or load and store inside the task, in one region."))
	} else if apart {
		b.WriteString(" The store is inside a region, but the load it depends on was made outside that region (or in an earlier one), so the task could have moved between the two. The load and the store must be inside the same region.")
	} else if len(handlers) > 0 {
		compat.Fprintfb(b, " The interrupt handler %s also uses %s, so an interrupt on this processor could run in the middle of it as well: it must be inside a region with maskable interrupts disabled (x86.Cli to x86.Sti, x86.SaveFlagsCli to x86.RestoreFlags, or an rt.Lock held), not just with preemption disabled.", compat.ArgS(compat.Join(handlers, " and ")), compat.ArgS(name))
	} else {
		b.WriteString(" It must be inside a region where the task cannot move: preemption disabled (rt.DisablePreemption to rt.EnablePreemption), maskable interrupts disabled, or an rt.Lock held.")
	}
	if u.dep != nil && u.dep.opaque != "" {
		compat.Fprintfb(b, " The dependence passes through a call of %s, whose body the analysis cannot see; it assumes that the result of such a call depends on all of its arguments, and rejects the store when it cannot tell.", compat.ArgS(u.dep.opaque))
	}
	if u.dep != nil {
		b.WriteString(" A lone load or store of a whole variable of at most a machine word is allowed anywhere, as long as the store does not depend on a load of the same variable.")
	} else {
		b.WriteString(" A lone load or store of a whole variable of at most a machine word is allowed anywhere.")
	}
	d.Text = b.String()
	stmt := u.text
	if !strings.HasSuffix(stmt, "++") && !strings.HasSuffix(stmt, "--") {
		stmt = compat.Concat("// the access to ", name)
	}
	code := compat.Concat("rt.DisablePreemption()\ndefer rt.EnablePreemption()\n", stmt)
	intro := "Put the access inside a region where the task cannot move"
	if u.dep != nil {
		switch {
		case u.dep.callee != "":
			stmt = compat.Concat("x := ", name, "\n", u.dep.callee, "(x)")
		case !u.dep.data:
			stmt = compat.Concat("if ", name, " == old {\n\t", name, " = new\n}")
		default:
			stmt = compat.Concat("x := ", name, "\n", name, " = x + 1")
		}
		code = compat.Concat("rt.DisablePreemption()\ndefer rt.EnablePreemption()\n", stmt)
		intro = "Put the load and the store inside one region where the task cannot move"
	}
	if len(handlers) > 0 {
		code = compat.Concat("f := x86.SaveFlagsCli()\n", stmt, "\nx86.RestoreFlags(f)")
		intro = "Put the access inside a region with interrupts disabled"
		if u.dep != nil {
			intro = "Put the load and the store inside one region with interrupts disabled"
		}
	}
	d.Fix = pu.New(diag.Fix{Intro: intro, Code: code})
	return d
}

// depReason says why a store depends on a load of its variable.
func (a *analyzer) depReason(ds *cpuDepSite) string {
	l := a.cpuLoads[ds.loads[0]]
	name := a.cpuName(ds.v)
	var from string
	if l.call != nil {
		from = compat.Sprintfb("the call of %s at %s, which loads %s", compat.ArgS(l.callee), compat.ArgS(a.short(l.pos)), compat.ArgS(name))
	} else {
		from = compat.Sprintfb("the load of %s at %s", compat.ArgS(name), compat.ArgS(a.short(l.pos)))
	}
	switch {
	case ds.callee != "":
		return compat.Sprintfb("a call of %s, which stores %s, from a value or under a condition that depends on %s", compat.ArgS(ds.callee), compat.ArgS(name), compat.ArgS(from))
	case ds.data:
		return compat.Sprintfb("a store of a value that depends on %s", compat.ArgS(from))
	}
	return compat.Sprintfb("a store controlled by a condition that depends on %s", compat.ArgS(from))
}

func (a *analyzer) cpuDerefError(x *access, handlers []string) *diag.Diagnostic {
	name := a.cpuName(x.cpu.v)
	d := diag.New(a.pos(x.pos), diag.CheckerCode(typerr.PerCPUPointer), "a pointer to the per-CPU variable %s is used outside the region it was taken in", pu.Box(name))
	d.Text = compat.Sprintfb("%s has one copy for each processor, and &%s points to the copy of the processor that ran the code that took it. Here the pointer is used where the region it was taken in (preemption or interrupts disabled, or an rt.Lock held) is no longer held, so the task may have moved to another processor and the pointer reaches that processor's copy while another task or an interrupt handler uses it.", compat.ArgS(name), compat.ArgS(name))
	d.Fix = pu.New(diag.Fix{Intro: "Use the pointer only inside the region it was taken in, or take the address again where it is needed"})
	return d
}

// cpuName writes a per-CPU variable's name as the program would.
func (a *analyzer) cpuName(v *types2.Var) string {
	name := v.Name()
	if pkg := v.Pkg(); pkg != nil && a.prog.Main != nil && pkg != a.prog.Main.Types {
		name = compat.Concat(pkg.Name(), ".", name)
	}
	return name
}

// ----------------------------------------------------------------------------
// Interrupt handlers

// handlerRegions are the regions an interrupt handler's body is in when it
// starts: maskable interrupts are disabled, so nothing preempts it.
func handlerRegions(pos token.Pos) lockset {
	var ls lockset
	ls = ls.with(regionAt(regIRQ, pos), true)
	return ls.with(regionAt(regPreempt, pos), true)
}

// handlerOf returns the handler a call of x86.InterruptGate or x86.TrapGate
// installs, and whether it is a trap handler (//pu:interrupt trap), entered
// through a trap gate, whose body is not an interrupt-disabled region.
func (a *analyzer) handlerOf(c *ast.CallExpr) (h *function, trap bool) {
	var id *ast.Ident
	switch f := ast.Unparen(c.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	}
	if id == nil || len(c.Args) != 3 {
		return nil, false
	}
	fn, ok := a.uses[id].(*types2.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "pluto/arch/x86" || fn.Name() != "InterruptGate" && fn.Name() != "TrapGate" {
		return nil, false
	}
	var arg *ast.Ident
	switch x := ast.Unparen(c.Args[0]).(type) {
	case *ast.Ident:
		arg = x
	case *ast.SelectorExpr:
		arg = x.Sel
	}
	if arg == nil {
		return nil, false
	}
	hf, _ := a.uses[arg].(*types2.Func)
	if hf == nil {
		return nil, false
	}
	f := a.funcs[hf.Origin()]
	if f == nil || f.decl == nil {
		return nil, false
	}
	return f, isTrapDecl(f.decl)
}

// isTrapDecl reports whether a function is declared //pu:interrupt trap.
func isTrapDecl(d *ast.FuncDecl) bool {
	if d.Doc == nil {
		return false
	}
	for _, c := range d.Doc.List {
		if name, ok := strings.CutPrefix(c.Text, "//pu:"); ok && directive.Normalize(name) == "interrupt trap" {
			return true
		}
	}
	return false
}

// sortedCPU orders entries by position for stable reports.
func sortedCPU(es []*cpuEntry) {
	sort.SortStableFunc(es, func(a, b *cpuEntry) bool { return a.x.pos < b.x.pos })
}

// ----------------------------------------------------------------------------
// Allocators

// allocatorMethods returns the Alloc and Free methods of the types that
// implement the runtime's Allocator interface: what a call of new, make or
// a string operation may reach through the interface (section 20.10). Only a
// program with an interrupt handler needs them: the state they share with the
// handler meets the rules of section 19.3 like any other data.
func (a *analyzer) allocatorMethods() []*function {
	if a.allocFns != nil || a.allocDone {
		return a.allocFns
	}
	a.allocDone = true
	var iface *types2.Interface
	for _, p := range a.prog.Packages {
		if p.Path != "pluto/rt" {
			continue
		}
		if tn, ok := p.Types.Scope().Lookup("Allocator").(*types2.TypeName); ok {
			iface, _ = tn.Type().Underlying().(*types2.Interface)
		}
	}
	if iface == nil {
		return nil
	}
	for _, name := range []string{"Alloc", "Free"} {
		for _, f := range a.byName[name] {
			r := f.sig.Recv()
			if r == nil || f.bodyless {
				continue
			}
			T := r.Type()
			if p, ok := T.(*types2.Pointer); ok {
				T = p.Elem()
			}
			if types2.Implements(T, iface) || types2.Implements(types2.NewPointer(T), iface) {
				a.allocFns = compat.Append(a.allocFns, f)
			}
		}
	}
	return a.allocFns
}

// selfSynchronized reports whether E is an allocator type that synchronizes
// itself: it implements the Allocator interface, is written in unsafe code
// (section 18.12) and holds a lock of its own (an rt.Lock or a sync.Mutex),
// as the program-wide default allocator, pluto/alloc.SyncSlab, does. Its
// state is the responsibility of that code (section 19.3, what is not
// checked), so a view of one may cross between tasks, as a view of a
// pluto/sync type may. An allocator without a lock of its own, as an arena
// is, is no exception: no arena is safe for simultaneous use by several
// tasks (appendix F.2).
func (a *analyzer) selfSynchronized(E types2.Type) bool {
	n, ok := types2.Unalias(E).(*types2.Named)
	if !ok {
		return false
	}
	st, ok := n.Underlying().(*types2.Struct)
	if !ok {
		return false
	}
	if v, ok := a.syncAllocs[n.Origin()]; ok {
		return v
	}
	if a.syncAllocs == nil {
		a.syncAllocs = map[*types2.Named]bool{}
	}
	res := false
	if implementsAllocator(a, n) && a.unsafeNamed(n) {
		for i := 0; i < st.NumFields() && !res; i++ {
			res = isSyncType(st.Field(i).Type())
		}
	}
	a.syncAllocs[n.Origin()] = res
	return res
}

// addAllocators adds to the summary of a task, handler or the main task, if
// it may allocate, a call of every installed allocator's Alloc and Free.
func (a *analyzer) addAllocators(s *summary, base *state) {
	if len(a.handlers) == 0 || !s.mayAlloc {
		return
	}
	tw := newWalker(a, a.program)
	tw.sum = s
	for _, f := range a.allocatorMethods() {
		if a.unsafeAllocator(f) {
			continue
		}
		tw.apply(f, compat.Clone([]origins{a.allocatorState(f)}), f.decl.Pos(), f.name, base.clone(), nil)
	}
}

// unsafeAllocator reports whether f is a method of an allocator type written
// in unsafe code (section 18.12): some method of the type reinterprets memory
// through unsafe.Pointer, uses the functions of package unsafe, the unchecked
// allocation layer or the processor instructions of pluto/arch/x86. Its state
// is the programmer's responsibility, as the state of any unsafe code is
// (section 19.3, what is not checked).
func (a *analyzer) unsafeAllocator(f *function) bool {
	r := f.sig.Recv()
	if r == nil {
		return false
	}
	T := r.Type()
	if p := pointerElem(T); p != nil {
		T = p
	}
	n, ok := T.(*types2.Named)
	if !ok {
		return false
	}
	return a.unsafeNamed(n)
}

// unsafeNamed reports whether some method of n is unsafe code.
func (a *analyzer) unsafeNamed(n *types2.Named) bool {
	if v, ok := a.unsafeTypes[n.Origin()]; ok {
		return v
	}
	if a.unsafeTypes == nil {
		a.unsafeTypes = map[*types2.Named]bool{}
	}
	res := false
	for i := 0; i < n.NumMethods() && !res; i++ {
		if m := a.funcs[n.Method(i).Origin()]; m != nil && m.body != nil {
			res = a.usesUnsafe(m.body)
		}
	}
	a.unsafeTypes[n.Origin()] = res
	return res
}

// usesUnsafe reports whether a body is unsafe code by the list of section
// 18.12 that a body can show: a conversion to or from unsafe.Pointer, a name
// of package unsafe, a call into the unchecked allocation layer, or one of the
// processor instructions of pluto/arch/x86 other than the interrupt flag.
func (a *analyzer) usesUnsafe(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.Ident:
			switch o := a.uses[n].(type) {
			case *types2.PkgName:
				found = o.Imported().Path() == "unsafe"
			case *types2.Func:
				if o.Pkg() == nil || o.Type().(*types2.Signature).Recv() != nil {
					break
				}
				switch o.Pkg().Path() {
				case "pluto/arch/x86":
					switch o.Name() {
					case "Cli", "Sti", "SaveFlagsCli", "RestoreFlags":
					default:
						found = true
					}
				case "pluto/alloc":
					found = o.Name() == "Raw" || o.Name() == "New" || o.Name() == "Free"
				}
			}
		case *ast.CallExpr:
			if tv, ok := a.types[n.Fun]; ok && tv.IsType() && len(n.Args) == 1 {
				found = isUnsafePointer(tv.Type)
				if tv, ok := a.types[n.Args[0]]; ok && isUnsafePointer(tv.Type) {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// allocatorState returns the origin of the receiver of an allocator method.
// The analysis does not follow which object the program installs as its
// allocator, so every pointer receiver of one type is one object, shared by
// every party that allocates: the fields Alloc and Free reach through it are
// shared data like a package variable, and meet the rules of section 19.3
// (section 20.10). A value receiver is a copy.
func (a *analyzer) allocatorState(f *function) origins {
	r := f.sig.Recv()
	if r == nil || pointerElem(r.Type()) == nil {
		return nil
	}
	T := pointerElem(r.Type())
	if a.allocState == nil {
		a.allocState = map[string]*types2.Var{}
	}
	key := types2.TypeString(T, nil)
	v := a.allocState[key]
	if v == nil {
		name := r.Name()
		if name == "" || name == "_" {
			name = "the allocator"
		}
		v = types2.NewVar(token.NoPos, nil, name, T)
		a.allocState[key] = v
	}
	return compat.Clone(origins{{vp: vpath{root: root{kind: rGlobal, v: v}}.addr()}})
}

// cpuCarry reports a pointer to a per-CPU variable kept in a variable that
// outlives the loop iteration it is assigned in: the next iteration would
// use it in a region that is not the one it was taken in, which the sites of
// regions cannot tell apart (E2023).
func (w *walker) cpuCarry(v *types2.Var, val origins, pos token.Pos) {
	if !w.a.cpuOn || w.quiet > 0 || w.frame != nil || !pos.IsValid() || !holdsCPUPointer(val) {
		return
	}
	switch {
	case len(w.loopPos) > 0 && v.Pos() < w.loopPos[len(w.loopPos)-1]:
		w.cpuSink(val, pos, "kept in a variable that outlives the loop iteration")
	case w.gotoCarries(v, pos):
		w.cpuSink(val, pos, "kept in a variable that outlives the pass of a loop made with goto")
	case w.captured(v):
		w.cpuSink(val, pos, "kept in a variable of the enclosing function, which outlives the call")
	}
}

// holdsCPUPointer reports whether val has a pointer to a per-CPU variable.
func holdsCPUPointer(val origins) bool {
	for _, o := range val {
		if r := o.vp.root; r.kind == rGlobal && r.idx != 0 && !hasDeref(o.vp.path) && o.vp.isAddr() {
			return true
		}
	}
	return false
}

// gotoCarries reports whether the code at pos may run again after a goto to
// a label that follows the declaration of v: v then lives across the passes
// of that loop, which the sites of regions cannot tell apart.
func (w *walker) gotoCarries(v *types2.Var, pos token.Pos) bool {
	if len(w.gotos) == 0 || w.fn == nil || w.fn.body == nil {
		return false
	}
	found := false
	ast.Inspect(w.fn.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.LabeledStmt:
			if w.gotos[n.Label.Name] && v.Pos() < n.Pos() && n.Pos() <= pos {
				found = true
			}
		}
		return !found
	})
	return found
}

// captured reports whether v is declared outside the function literal being
// walked: it outlives a call of the literal.
func (w *walker) captured(v *types2.Var) bool {
	lit := w.fn.lit
	return lit != nil && (v.Pos() < lit.Pos() || v.Pos() >= lit.End())
}
