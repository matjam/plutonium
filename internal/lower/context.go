package lower

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Interrupt context (language spec, "Interrupts"). An interrupt handler
// runs on top of whatever the processor was doing, and the panic handler
// on top of whatever panicked, so neither may wait for anything: what it
// waits for may be held by the code it interrupted, which cannot run until
// it returns. checkContexts walks the whole program's call graph from
// every //pu:interrupt handler and from the panic handler and reports the
// first path to an operation that may wait or start a task:
//
//   - a call of a function marked //pu:blocking, which the walk does not
//     enter: the runtime's park, which every wait in the library ends in
//     (pluto/sync's Mutex.Lock and WaitGroup.Wait, channel operations,
//     sleeping, the poller), and any function the program marks, such as a
//     kernel's own lock or an assembly routine that waits;
//   - a channel send or receive outside a select with a default case, a
//     select without one, or a range over a channel;
//   - a task statement, or a call of the runtime's spawn, which every way of
//     starting a task ends in (rt.Go, sync.WaitGroup.Task).
//
// Whether a library function may wait is not marked on it: the walk goes
// through it to the runtime operation it reaches. Messages name the path
// only as far as the call that enters the standard library, which is where
// the program's own code waits.
//
// The walk runs over the lowered functions, so it follows the calls the
// compiler adds for the drops it inserts (section 18.6) like the calls the
// program writes: at the end of a scope, over an assigned owner, for
// temporaries, and when a counted value's count reaches zero, into Drop,
// drop glue and the Free of the allocators the program installs. A
// function without a body (assembly, C) is taken at its declaration: it
// does not wait unless it is marked //pu:blocking, and a function with a
// body that is marked is a restriction only (language spec section 20.10).
//
// Allocation is not special: new and make call the allocator through the
// rt.Allocator interface, and the walk follows that call into every
// allocator the program installs, so an allocator that may wait (one that
// takes a pluto/sync lock) is found like any other function that may.
//
// Calls through interfaces and func values cannot be resolved exactly. The
// walk follows an interface method call into that method of every type the
// program converts to the interface (its itabs, which also cover
// conversions from other interfaces and assertions), and a call through a
// func value into every function of an identical type whose value the
// program takes anywhere. Assembly and C are trusted unless their
// declarations are marked //pu:blocking.

// A callSite describes a call as written, for the walk and its messages.
type callSite struct {
	desc        string            // the callee as written ("cfg.load", "w.Write")
	recv        string            // the receiver as written, for a static method call
	iface       types2.Type       // for a call through an interface: its type
	method      int               // and the index of the method in it
	fsig        *types2.Signature // for a call through a func value: its type
	lit         *ssa.Sym          // and the function, when the value is a literal's (defer func() {...}())
	hook        *types2.Var       // or the package variable called, which may be a hook (hooks.go)
	viaHook     bool              // the walk resolved the call as a hook's
	drop        bool              // a dynamic interface's descriptor drop adapter
	dropType    types2.Type       // interface whose dynamic value is dropped
	countedDrop bool              // only shared/synced payloads are released
}

// A funcBody is the source of a lowered function, which the walk scans for
// channel operations and task statements.
type funcBody struct {
	body *ast.BlockStmt
	info *types2.Info
}

// An opKind says what a forbidden operation does.
type opKind int

const (
	opWait  opKind = iota // may wait: a lock, a channel, a select
	opSpawn               // starts a task
)

// A blockOp is a channel operation or task statement in a function's body.
type blockOp struct {
	pos  token.Pos
	what string // "sends on work"
	kind opKind
}

// blockingOps lists the operations in a function body that may wait or
// start a task. Function literals are functions of their own.
func blockingOps(fb funcBody) []blockOp {
	var ops []blockOp
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.TaskStmt:
			ops = append(ops, blockOp{n.Pos(), "starts a task", opSpawn})
		case *ast.SendStmt:
			ops = append(ops, blockOp{n.Pos(), "sends on " + types2.ExprString(n.Chan), opWait})
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				ops = append(ops, blockOp{n.Pos(), "receives from " + types2.ExprString(n.X), opWait})
			}
		case *ast.RangeStmt:
			if tv, ok := fb.info.Types[n.X]; ok && tv.Type != nil {
				if _, ok := tv.Type.Underlying().(*types2.Chan); ok {
					ops = append(ops, blockOp{n.Pos(), "ranges over channel " + types2.ExprString(n.X), opWait})
				}
			}
		case *ast.SelectStmt:
			hasDefault := false
			for _, c := range n.Body.List {
				if cc, ok := c.(*ast.CommClause); ok && cc.Comm == nil {
					hasDefault = true
				}
			}
			if !hasDefault {
				ops = append(ops, blockOp{n.Pos(), "waits in a select with no default case", opWait})
			}
			// With a default case the communications do not wait; the
			// clause bodies are ordinary code.
			for _, c := range n.Body.List {
				if cc, ok := c.(*ast.CommClause); ok {
					for _, st := range cc.Body {
						ast.Inspect(st, visit)
					}
				}
			}
			return false
		}
		return true
	}
	ast.Inspect(fb.body, visit)
	return ops
}

// A hop is one call on a path through the call graph.
type hop struct {
	from, to *ssa.Sym
	pos      token.Pos
	site     *callSite // nil for calls the compiler adds (new, make, panics)
	alloc    bool      // a call of an rt.Allocator's Alloc method (initalloc.go)
}

// indirect reports whether the hop is a call through an interface or a
// func value whose function is not known.
func (h *hop) indirect() bool {
	return h.site != nil && h.site.lit == nil && (h.site.iface != nil || h.site.fsig != nil)
}

// A callGraph answers questions about the lowered program's calls.
type callGraph struct {
	l     *Lowerer
	funcs map[*ssa.Sym]*ssa.Func
	edges map[*ssa.Sym][]hop
	// candidates of indirect calls, by itab key or signature
	ifaceTargets map[string][]*ssa.Sym // by interface and method index
	valueFuncs   []*ssa.Sym
	hooks        *hookInfo
}

// checkContexts checks that no interrupt handler, and not the panic
// handler, can reach an operation that waits or starts a task.
func (l *Lowerer) checkContexts() {
	g := &callGraph{l: l, funcs: l.funcsBySym(), edges: map[*ssa.Sym][]hop{}, ifaceTargets: map[string][]*ssa.Sym{}, hooks: l.findHooks()}
	for sym := range l.funcValues {
		if !g.hooks.hidden[sym] {
			g.valueFuncs = append(g.valueFuncs, sym)
		}
	}
	sort.Slice(g.valueFuncs, func(i, j int) bool { return g.valueFuncs[i].Name < g.valueFuncs[j].Name })

	var panicSym *ssa.Sym
	if l.panicFn != nil {
		panicSym = l.funcs[funcSymName(l.panicFn, nil)]
	}
	var roots []*ssa.Sym
	for sym := range l.interruptSyms {
		if g.funcs[sym] != nil {
			roots = append(roots, sym)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Name < roots[j].Name })
	for _, root := range roots {
		// Panics in the handler reach the panic handler, which is
		// checked on its own.
		if path, last, op := g.search(root, panicSym); path != nil {
			l.reportContext(g, root, roleInterrupt, path, last, op)
		}
	}
	if panicSym != nil && g.funcs[panicSym] != nil {
		if path, last, op := g.search(panicSym, nil); path != nil {
			l.reportContext(g, panicSym, rolePanic, path, last, op)
		}
	}
	// The scheduler's Wake runs when rt.Ready does, which interrupt
	// handlers may call (scheduler contract, rule 6).
	if wake := l.schedWakeSym(); wake != nil && g.funcs[wake] != nil {
		if path, last, op := g.search(wake, panicSym); path != nil {
			l.reportContext(g, wake, roleWake, path, last, op)
		}
	}
	l.checkInitAlloc(g)
	l.checkInitTasks(g)
}

// A ctxRole says why a root of the walk must not wait.
type ctxRole int

const (
	roleInterrupt ctxRole = iota // a //pu:interrupt handler
	rolePanic                    // the //pu:panic handler
	roleWake                     // the bound scheduler's Wake
)

// schedWakeSym returns the lowered Wake method of the bound scheduler, or
// nil.
func (l *Lowerer) schedWakeSym() *ssa.Sym {
	if l.sched == nil {
		return nil
	}
	obj, _, _ := types2.LookupFieldOrMethod(l.sched.v.Type(), true, l.sched.v.Pkg(), "Wake")
	fn, ok := obj.(*types2.Func)
	if !ok {
		return nil
	}
	return l.funcs[funcSymName(fn, nil)]
}

// A ctxNode is a function reached by the walk, and how.
type ctxNode struct {
	sym    *ssa.Sym
	via    *hop
	parent *ctxNode
}

// search walks the calls from root breadth first, not entering skip, and
// returns the first function found that may wait or start a task,
// with either the call of the //pu:blocking function it makes (last) or
// the operation in its body (op).
func (g *callGraph) search(root, skip *ssa.Sym) (*ctxNode, *hop, *blockOp) {
	seen := map[*ssa.Sym]bool{root: true}
	queue := []*ctxNode{{sym: root}}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if fb, ok := g.l.bodies[n.sym]; ok {
			if ops := blockingOps(fb); len(ops) > 0 {
				return n, nil, &ops[0]
			}
		}
		for i := range g.callsOf(n.sym) {
			h := &g.edges[n.sym][i]
			if g.l.isBlocking(h.to) || isSpawn(h.to) {
				return n, h, nil
			}
			if h.to == skip || seen[h.to] {
				continue
			}
			seen[h.to] = true
			queue = append(queue, &ctxNode{sym: h.to, via: h, parent: n})
		}
	}
	return nil, nil, nil
}

// callsOf returns the calls the function sym makes, with every candidate
// of each indirect call.
func (g *callGraph) callsOf(sym *ssa.Sym) []hop {
	if hops, ok := g.edges[sym]; ok {
		return hops
	}
	var hops []hop
	if f := g.funcs[sym]; f != nil {
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				switch v.Op {
				case ssa.OpCall:
					if callee, ok := v.Aux.(*ssa.Sym); ok {
						if exported := g.l.cnames[callee.Name]; callee.Kind == ssa.SymExtern && exported != nil {
							callee = exported
						}
						hops = append(hops, hop{from: sym, to: callee, pos: g.callPos(v), site: g.l.sites[v]})
					}
				case ssa.OpCallInd:
					site := g.l.sites[v]
					if v.Args[0].Op == ssa.OpFuncAddr {
						target := v.Args[0].Aux.(*ssa.Sym)
						hops = append(hops, hop{from: sym, to: target, pos: g.callPos(v), site: site})
						continue
					}
					if site == nil {
						panic("lower: indirect call without call-graph metadata")
					}
					for _, t := range g.targets(site) {
						hops = append(hops, hop{from: sym, to: t, pos: g.callPos(v), site: site})
					}
				}
			}
		}
	}
	g.edges[sym] = hops
	return hops
}

// targets returns the functions an indirect call may reach.
func (g *callGraph) targets(site *callSite) []*ssa.Sym {
	if site.drop {
		var out []*ssa.Sym
		for _, d := range g.l.types.descs {
			if d.drop == nil || !types2.Implements(d.T, site.dropType.Underlying().(*types2.Interface)) {
				continue
			}
			if site.countedDrop {
				w, ok := d.T.Underlying().(*types2.Wrapper)
				if !ok || w.Kind() != types2.WrapShared && w.Kind() != types2.WrapSynced {
					continue
				}
			}
			out = append(out, d.drop)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out
	}
	if site.iface != nil {
		key := fmt.Sprintf("%s.%d", typeString(site.iface), site.method)
		if t, ok := g.ifaceTargets[key]; ok {
			return t
		}
		var keys []string
		for k, it := range g.l.types.itabs {
			// Identical rather than the same name: an itab may name
			// the interface by an alias (alloc.Allocator).
			if types2.Identical(it.I, site.iface) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		var out []*ssa.Sym
		for _, k := range keys {
			if it := g.l.types.itabs[k]; site.method < len(it.methods) {
				out = append(out, it.methods[site.method])
			}
		}
		g.ifaceTargets[key] = out
		return out
	}
	if site.lit != nil {
		return []*ssa.Sym{site.lit}
	}
	if g.hooks != nil && site.hook != nil {
		if t, ok := g.hooks.targets[site.hook]; ok {
			site.viaHook = true
			return t
		}
	}
	var out []*ssa.Sym
	want := plainSig(site.fsig)
	for _, sym := range g.valueFuncs {
		if types2.Identical(plainSig(g.l.funcValues[sym]), want) {
			out = append(out, sym)
		}
	}
	return out
}

func (g *callGraph) callPos(v *ssa.Value) token.Pos {
	if v.Pos.IsValid() {
		return v.Pos
	}
	return g.l.callOrigins[v]
}

// plainSig returns sig with its receiver, if any, as the first parameter:
// the type of the func value that calls it.
func plainSig(sig *types2.Signature) *types2.Signature {
	if sig.Recv() == nil {
		return sig
	}
	params := []*types2.Var{sig.Recv()}
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i))
	}
	return types2.NewSignatureType(nil, nil, nil, types2.NewTuple(params...), sig.Results(), sig.Variadic())
}

// isSpawn reports whether sym is the runtime's spawn, which starts every
// task.
func isSpawn(sym *ssa.Sym) bool {
	switch sym.Name {
	case "pluto/rt.spawn", "pluto/rt.spawnScoped", "pluto/rt.spawnFor", "pluto/rt.TaskJoined":
		return true
	}
	return false
}

// isBlocking reports whether sym is a function marked //pu:blocking.
func (l *Lowerer) isBlocking(sym *ssa.Sym) bool {
	fn := l.symFuncs[sym]
	if fn == nil {
		return false
	}
	d := l.decls[fn.Origin()]
	return d != nil && hasDirective(d.decl.Doc, "blocking")
}

// synthesized reports whether sym is code the compiler made (a method
// wrapper or an interface adapter), which messages pass over.
func (g *callGraph) synthesized(sym *ssa.Sym) bool {
	_, ok := g.l.bodies[sym]
	return g.funcs[sym] != nil && !ok
}

// displayName names a function in messages: the symbol without the import
// path's directories, and without "main." for the main package.
func displayName(sym *ssa.Sym) string {
	name := sym.Name
	if rest, ok := strings.CutPrefix(name, "main."); ok {
		return rest
	}
	if i := strings.LastIndex(name[:max(strings.Index(name, "."), 0)], "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// reportContext reports the path from root, an interrupt handler, the
// panic handler or the scheduler's Wake (role), to the operation it must
// not reach: the call last of a //pu:blocking function, or op, in the
// function n.
func (l *Lowerer) reportContext(g *callGraph, root *ssa.Sym, rl ctxRole, n *ctxNode, last *hop, op *blockOp) {
	isPanic := rl == rolePanic
	var hops []hop
	for ; n.via != nil; n = n.parent {
		hops = append(hops, *n.via)
	}
	for i, j := 0, len(hops)-1; i < j; i, j = i+1, j-1 {
		hops[i], hops[j] = hops[j], hops[i]
	}
	if last != nil {
		hops = append(hops, *last)
	}
	// Pass over wrappers and adapters: the call into one continues as
	// the call out of it.
	var vis []hop
	for _, h := range hops {
		if len(vis) > 0 && g.synthesized(h.from) {
			vis[len(vis)-1].to = h.to
			continue
		}
		vis = append(vis, h)
	}

	kind := opWait
	if op != nil {
		kind = op.kind
	} else if last != nil && isSpawn(last.to) {
		kind = opSpawn
	}
	// The rest of the path inside the standard library is how the library
	// waits, not where the program does: end at the call that enters it.
	if k := libraryEntry(vis); k >= 0 && (k < len(vis)-1 || op != nil) {
		vis = vis[:k+1]
		op = nil
		last = &vis[k]
	}
	var phrases []string
	var indirect *hop
	for i := range vis {
		h := &vis[i]
		if indirect == nil && h.indirect() {
			indirect = h
		}
		final := last != nil && i == len(vis)-1
		phrases = append(phrases, l.hopPhrase(h, final, kind))
	}
	pos := token.NoPos
	if op != nil {
		phrases = append(phrases, fmt.Sprintf("%s (%s)", op.what, l.shortPos(op.pos)))
		pos = op.pos
	}
	if len(vis) > 0 {
		pos = vis[0].pos
	}
	name := displayName(root)
	path := name + " " + strings.Join(phrases, ", which ") + "."

	role := "is an interrupt handler"
	switch rl {
	case rolePanic:
		role = "is the panic handler"
	case roleWake:
		role = "is the scheduler's Wake, which interrupt handlers reach through rt.Ready"
	}
	code, verb := diag.ContextWaits, "wait"
	if kind == opSpawn {
		code, verb = diag.ContextSpawns, "start a task"
	}
	var why string
	switch {
	case rl == roleWake:
		why = "rt.Ready calls the scheduler's Wake, and interrupt handlers may call rt.Ready " +
			"(or make a channel operation that does), so Wake runs under the same rule as " +
			"a handler: it runs on top of whatever code the CPU was executing, and must not " +
			"wait for anything that code may hold."
	case !isPanic && kind == opWait:
		why = "Interrupt handlers run on top of whatever code the CPU was executing. " +
			"If a handler waits, for a lock or for room in a channel, the interrupted code " +
			"can never resume to release it, and the CPU stops."
	case !isPanic:
		why = "Interrupt handlers run on top of whatever code the CPU was executing. " +
			"Starting a task allocates its stack and takes the scheduler's locks, " +
			"which the interrupted code may hold, and then the CPU stops."
	case kind == opWait:
		why = "A panic can happen anywhere: in an interrupt handler, or while the code " +
			"that panicked holds a lock. The panic handler runs on top of that code, so " +
			"if it waits for a lock or a channel, it may wait for code that can never run " +
			"again, and the program hangs instead of reporting the panic."
	default:
		why = "A panic can happen anywhere: in an interrupt handler, or while the code " +
			"that panicked holds a lock. Starting a task allocates and takes the " +
			"scheduler's locks, which the code that panicked may hold."
	}
	text := why + "\n" + path
	if indirect != nil {
		text += "\n" + indirectNote(indirect)
	}
	d := &diag.Diagnostic{Code: code, Msg: fmt.Sprintf("%s %s, so it must not %s", name, role, verb)}
	switch {
	case kind == opWait && throughFile(hops):
		text += "\nThe path goes through an os.File. Writing to a file waits for the poller " +
			"when the file is a pipe or a socket that is full, and the check cannot tell which " +
			"file a call writes to. rt.Console writes to standard error with a system call " +
			"that never waits."
		example := "rt.Console{}.Write(msg)"
		if isPanic {
			example = "p.Print(rt.Console{})"
		}
		d.Fix = &diag.Fix{Intro: "Write to rt.Console{} instead of os.Stderr in a panic or interrupt handler; os.File writes may wait for the poller", Code: example}
	case isPanic:
		text += "\nReport the panic with code that does not wait: write to the console or a device directly, then halt."
	default:
		d.Fix = &diag.Fix{Intro: "To hand work to normal code, use a non-blocking send", Code: "select { case work <- item: default: }"}
	}
	d.Text = text
	l.report(pos, d)
}

// libraryEntry returns the index of the call in path that enters the
// standard library from the program's own code and after which the path
// stays in the library, or -1.
func libraryEntry(path []hop) int {
	k := -1
	for i, h := range path {
		switch {
		case !isLibrary(h.to):
			k = -1
		case k < 0 && !isLibrary(h.from):
			k = i
		}
	}
	return k
}

// isLibrary reports whether sym is a function of the standard library.
func isLibrary(sym *ssa.Sym) bool { return strings.HasPrefix(sym.Name, "pluto/") }

// throughFile reports whether a path calls a method of pluto/os's File,
// whose writes may wait for the poller.
func throughFile(hops []hop) bool {
	for _, h := range hops {
		if strings.HasPrefix(h.to.Name, "pluto/os.(*File).") {
			return true
		}
	}
	return false
}

// allocCalls are the runtime functions the compiler calls to allocate: for
// new and make, and for the operations that make strings.
var allocCalls = map[string]bool{
	"pluto/rt.alloc": true, "pluto/rt.allocWith": true, "pluto/rt.boxNew": true, "pluto/rt.boxNewWith": true,
	"pluto/rt.concatStrings": true, "pluto/rt.stringFromRunes": true,
	"pluto/rt.stringFromRune": true,
}

// hopPhrase describes one call of a path, following "which": "calls
// readConfig (irq.p:5)". final marks the call that waits or starts a
// task (kind).
func (l *Lowerer) hopPhrase(h *hop, final bool, kind opKind) string {
	at := l.shortPos(h.pos)
	var s string
	switch {
	case final && kind == opWait && h.site != nil && h.site.recv != "" && !h.indirect():
		return fmt.Sprintf("waits on %s (%s)", h.site.recv, at)
	case h.site == nil && allocCalls[h.to.Name]:
		s = fmt.Sprintf("allocates (%s)", at)
	case h.site == nil && strings.HasSuffix(h.to.Name, ").Drop"):
		// The compiler adds the call where a value is dropped.
		s = fmt.Sprintf("drops a value with %s (%s)", displayName(h.to), at)
	case h.site == nil || h.site.desc == "":
		s = fmt.Sprintf("calls %s (%s)", displayName(h.to), at)
	case h.indirect():
		s = fmt.Sprintf("calls %s (%s), which may be %s", h.site.desc, at, displayName(h.to))
	default:
		s = fmt.Sprintf("calls %s (%s)", h.site.desc, at)
	}
	switch {
	case final && kind == opSpawn:
		s += ", which starts a task"
	case final:
		s += ", which may wait"
	}
	return s
}

// indirectNote explains how the walk resolved an indirect call.
func indirectNote(h *hop) string {
	qual := func(p *types2.Package) string { return p.Name() }
	if I := h.site.iface; I != nil {
		method := ""
		if it, ok := I.Underlying().(*types2.Interface); ok && h.site.method < it.NumMethods() {
			method = it.Method(h.site.method).Name()
		}
		return fmt.Sprintf("%s calls through an interface, so it counts as calling the %s method "+
			"of every type the program converts to %s.", h.site.desc, method, types2.TypeString(I, qual))
	}
	if h.site.viaHook {
		return fmt.Sprintf("%s is a hook, a variable its package only sets to functions and calls, "+
			"so it counts as calling the functions assigned to it.", h.site.desc)
	}
	return fmt.Sprintf("%s calls through a func value, so it counts as calling every function "+
		"of type %s whose value the program takes.", h.site.desc, types2.TypeString(h.site.fsig, qual))
}
