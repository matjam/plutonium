// Package race checks that safe code has no data races.
//
// Plutonium tasks run in parallel, and there is no garbage collector or
// run-time race detector to fall back on, so the compiler rejects every
// program in which two tasks could use the same memory at once, one of them
// writing, without synchronisation (language spec section 19.3, memory
// safety design section 7). It does so with four rules, all inferred from
// the program without annotations:
//
//   - The sendable rule (E3501): a view (a pointer or slice, also inside an
//     interface, struct or array) that crosses to another task, as an
//     argument of a task statement or a value sent on a channel, must point
//     to memory that is safe for both: memory of a synchronised type
//     (pluto/sync, pluto/sync/atomic, the runtime's records, channels), a
//     package variable that no task writes once tasks run, the object of
//     a synced[T], or an object of a struct type that the type rule
//     accepts: every field is never changed once an object is shared, or
//     is used only atomically or under one lock of the same object
//     (typerule.go). Owners cross by moving, so only one task has them.
//   - Package variables (E3502, E3503): a package variable that several
//     tasks use, one of them writing it once tasks run, must be used only
//     through atomic operations, or every write must hold one same lock
//     (the variable's guard, inferred from the locks held at the writes),
//     and every access must hold it too (a read lock suffices for reads).
//   - synced[T] contents (E3502, E3503): the object of a synced[T] may be
//     used by any task that holds a reference, so the same guard rule
//     applies to its fields, with the lock found in the same object.
//   - Map contents (E3502, E3503): a map is a counted reference, so the
//     contents of every map of a type that crosses to another task are one
//     piece of data under the guard rule, with the lock found in the struct
//     the map is a field of or at package level (checkMaps).
//
// The analysis is whole-program and works on the typed syntax tree after
// the escape analysis, and only when code reachable from the program's
// start can start a task. Each function gets a summary: what it does
// through its parameters, with the locks held relative to its entry;
// edges to the functions it calls, with the locks held at each call; the
// package variables and synced fields it uses itself; the locks it leaves
// held or released; the tasks it starts; and the values it hands to other
// tasks. A call instantiates what the callee does through its parameters,
// so a lock taken in a helper method, or held across a call, counts where
// the access happens; the rest is expanded once per task at the end
// (check.go). Locks are followed through each body by a must-hold flow
// analysis, as Clang's thread-safety analysis does with annotations.
package race

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/scoped"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// maxErrors stops a run that has gone badly wrong from printing pages.
const maxErrors = 50

// debug prints the progress of the analysis (PLUTO_RACE_DEBUG=1).
var debug = os.Getenv("PLUTO_RACE_DEBUG") != ""

// Check analyses a loaded program that passed the escape analysis and
// returns the races it finds, as a diag.List, or nil.
func Check(prog *load.Program) error {
	mv, _ := moves.Analyze(prog)
	return CheckWith(prog, mv)
}

// CheckWith is Check, given the move checker's account of moves, which
// tells where the code that starts scoped tasks moves or drops a variable
// they may use (scope.go).
func CheckWith(prog *load.Program, mv *moves.Info) error {
	_, err := AnalyzeWith(prog, mv)
	return err
}

// AnalyzeWith is CheckWith, which also returns the checks that lowering must
// make before the scoped tasks it accepted start: slices that may overlap
// and that tasks use only in the same partition form (spec section 19.4,
// Memory reached through views).
func AnalyzeWith(prog *load.Program, mv *moves.Info) (scoped.Overlaps, error) {
	a := newAnalyzer(prog)
	a.moves = mv
	if debug {
		fmt.Fprintf(os.Stderr, "race: %d units\n", len(a.units))
	}
	a.callGraph()
	if !a.mayStartTasks() {
		// One task alone cannot race.
		return nil, nil
	}
	a.run()
	a.check()
	a.checkScopes()
	sort.SliceStable(a.errs, func(i, j int) bool {
		pi, pj := a.errs[i].Pos, a.errs[j].Pos
		if pi.Filename != pj.Filename {
			return pi.Filename < pj.Filename
		}
		if pi.Line != pj.Line {
			return pi.Line < pj.Line
		}
		return pi.Column < pj.Column
	})
	if len(a.errs) > maxErrors {
		a.errs = a.errs[:maxErrors]
	}
	return a.overlaps, a.errs.Err()
}

// A function is a declared function or method, a function literal, or a
// package's initialisation.
type function struct {
	name   string
	obj    *types2.Func  // declared functions
	lit    *ast.FuncLit  // literals
	decl   *ast.FuncDecl // declared functions
	body   *ast.BlockStmt
	sig    *types2.Signature
	params []*types2.Var // receiver first
	pkg    *load.Package
	unit   *unit

	inits   bool // a package's variable initialisers and init functions
	program bool // the whole program: every package's initialisation, then main

	sum      *summary
	bodyless bool
}

// A convSite is a conversion of a value to an interface, named in the error
// where an interface that it made not sendable crosses tasks.
type convSite struct {
	pos token.Pos
	pkg string
}

// A unit is a declared function together with the literals inside it,
// which share its variables. Units are analysed in the order of the call
// graph, callees first.
type unit struct {
	fns     []*function // literals, innermost first, then the declaration
	vars    map[*types2.Var]origins
	grew    bool
	callee  map[*unit]bool
	dynamic bool                // calls a func value the call graph cannot name
	dynSigs []*types2.Signature // the types of those func values
	dynAll  bool                // a call that may reach any function used as a value
	index   int                 // Tarjan
	low     int
	onstk   bool
}

type analyzer struct {
	prog *load.Program
	fset *token.FileSet

	scoped *scoped.Info // the scoped tasks of sync.Scope (scope.go)
	moves  *moves.Info  // what moves, for the uses of scoped tasks
	// views are the slice variables that index expressions index, by the
	// position of the expression, and overlaps the checks that scoped
	// tasks need among them (scope.go).
	views    map[token.Pos]*types2.Var
	overlaps scoped.Overlaps
	// concCalls are the func values that callees call from several
	// scoped tasks at once, with where they are passed (scope.go).
	concCalls []concCall
	concSeen  map[string]bool

	types      map[ast.Expr]types2.TypeAndValue
	uses       map[*ast.Ident]types2.Object
	defs       map[*ast.Ident]types2.Object
	selections map[*ast.SelectorExpr]*types2.Selection
	implicits  map[ast.Node]types2.Object

	reach map[*unit]bool // units reachable from the program's start

	sharedMemo, envMemo map[*function]bool // expander pruning (check.go)
	envUsed             map[*function]bits
	funcs               map[*types2.Func]*function
	lits                map[*ast.FuncLit]*function
	units               []*unit
	byName              map[string][]*function // methods by name
	values              []*function            // functions and literals used as values
	program             *function
	inits               []*function

	owned   ownedViews // view fields that own what they point to (ownedview.go)
	declPkg map[*types2.Package]*load.Package

	// Writes through views, by the type and field written (sendable.go),
	// and writes of package variables, which count when their address is
	// taken.
	viewWrites   map[string]*writeSite
	globalWrites map[*types2.Var]map[string]*writeSite
	addrGlobals  map[*types2.Var]bool
	converted    map[string]types2.Type              // types converted to interfaces
	convSites    map[string]*convSite                // one conversion of each type that can reach another task
	typeIDs      []types2.Type                       // referent types of synced[T], and map types, numbered for paths
	crossMaps    map[int]bool                        // map type numbers a crossing value reaches (checkMaps)
	tpArgs       map[*types2.TypeParam][]types2.Type // the type arguments each type parameter is instantiated with
	typeAcc      map[string][]*typeAccess            // uses of each field of objects, for the type rule
	typeSeen     map[string]bool
	resultNodes  map[resultKey]ast.Node // the memory a call's fresh results are

	errs    diag.List
	errSeen map[string]bool
}

func newAnalyzer(prog *load.Program) *analyzer {
	a := &analyzer{
		scoped:       scoped.Analyze(prog),
		sharedMemo:   map[*function]bool{},
		envMemo:      map[*function]bool{},
		envUsed:      map[*function]bits{},
		prog:         prog,
		fset:         prog.Fset,
		types:        map[ast.Expr]types2.TypeAndValue{},
		uses:         map[*ast.Ident]types2.Object{},
		defs:         map[*ast.Ident]types2.Object{},
		selections:   map[*ast.SelectorExpr]*types2.Selection{},
		implicits:    map[ast.Node]types2.Object{},
		funcs:        map[*types2.Func]*function{},
		lits:         map[*ast.FuncLit]*function{},
		byName:       map[string][]*function{},
		declPkg:      map[*types2.Package]*load.Package{},
		viewWrites:   map[string]*writeSite{},
		globalWrites: map[*types2.Var]map[string]*writeSite{},
		converted:    map[string]types2.Type{},
		errSeen:      map[string]bool{},
		tpArgs:       map[*types2.TypeParam][]types2.Type{},
		typeAcc:      map[string][]*typeAccess{},
		crossMaps:    map[int]bool{},
		typeSeen:     map[string]bool{},
		resultNodes:  map[resultKey]ast.Node{},
		views:        map[token.Pos]*types2.Var{},
		overlaps:     scoped.Overlaps{},
	}
	for _, p := range prog.Packages {
		for k, v := range p.Info.Types {
			a.types[k] = v
		}
		for k, v := range p.Info.Uses {
			a.uses[k] = v
		}
		for k, v := range p.Info.Defs {
			a.defs[k] = v
		}
		for k, v := range p.Info.Selections {
			a.selections[k] = v
		}
		for k, v := range p.Info.Implicits {
			a.implicits[k] = v
		}
		for id, inst := range p.Info.Instances {
			var tps *types2.TypeParamList
			switch obj := p.Info.Uses[id].(type) {
			case *types2.Func:
				tps = obj.Type().(*types2.Signature).TypeParams()
			case *types2.TypeName:
				if n, ok := types2.Unalias(obj.Type()).(*types2.Named); ok {
					tps = n.TypeParams()
				}
			}
			if tps == nil || inst.TypeArgs == nil {
				continue
			}
			for i := 0; i < tps.Len() && i < inst.TypeArgs.Len(); i++ {
				a.tpArgs[tps.At(i)] = append(a.tpArgs[tps.At(i)], inst.TypeArgs.At(i))
			}
		}
		a.declPkg[p.Types] = p
	}
	a.program = &function{name: "the program", program: true, sum: &summary{}}
	for _, p := range prog.Packages {
		init := &function{name: p.Path + " initialisation", pkg: p, inits: true, sum: &summary{}}
		a.inits = append(a.inits, init)
		u := &unit{fns: []*function{init}, vars: map[*types2.Var]origins{}}
		init.unit = u
		a.units = append(a.units, u)
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok {
					continue
				}
				obj, ok := p.Info.Defs[fd.Name].(*types2.Func)
				if !ok {
					continue
				}
				fn := &function{
					name:     funcName(obj),
					obj:      obj,
					decl:     fd,
					body:     fd.Body,
					pkg:      p,
					sig:      obj.Type().(*types2.Signature),
					bodyless: fd.Body == nil,
					sum:      &summary{},
				}
				fn.params = sigParams(fn.sig)
				a.funcs[obj] = fn
				if fd.Recv != nil {
					a.byName[obj.Name()] = append(a.byName[obj.Name()], fn)
				}
				u := &unit{vars: map[*types2.Var]origins{}}
				if fd.Body != nil {
					a.collectLits(u, fn, fd.Body)
				}
				u.fns = append(u.fns, fn)
				fn.unit = u
				a.units = append(a.units, u)
			}
		}
	}
	a.scanValues()
	return a
}

// collectLits adds the literals inside body to u, innermost first.
func (a *analyzer) collectLits(u *unit, outer *function, body ast.Node) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok {
			return true
		}
		sig, _ := a.typeOf(lit).(*types2.Signature)
		if sig == nil {
			return false
		}
		fn := &function{
			name: "the function literal in " + outer.name,
			lit:  lit,
			body: lit.Body,
			pkg:  outer.pkg,
			sig:  sig,
			unit: u,
			sum:  &summary{},
		}
		fn.params = sigParams(sig)
		a.lits[lit] = fn
		a.collectLits(u, fn, lit.Body)
		u.fns = append(u.fns, fn)
		return false
	})
}

// scanValues finds the functions and literals used as values, the
// candidates of calls through func values whose origin is unknown.
func (a *analyzer) scanValues() {
	seen := map[*function]bool{}
	note := func(f *function) {
		if f != nil && !seen[f] {
			seen[f] = true
			a.values = append(a.values, f)
		}
	}
	for _, p := range a.prog.Packages {
		for _, file := range p.Files {
			called := map[ast.Expr]bool{}
			ast.Inspect(file, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					called[ast.Unparen(stripIndex(n.Fun))] = true
				case *ast.Ident:
					if called[n] {
						return true
					}
					if fn, ok := p.Info.Uses[n].(*types2.Func); ok {
						note(a.funcs[fn.Origin()])
					}
				case *ast.SelectorExpr:
					if called[n] {
						return true
					}
					if sel := p.Info.Selections[n]; sel != nil && (sel.Kind() == types2.MethodVal || sel.Kind() == types2.MethodExpr) {
						if fn, ok := sel.Obj().(*types2.Func); ok {
							note(a.funcs[fn.Origin()])
						}
					}
				case *ast.FuncLit:
					if !called[n] {
						note(a.lits[n])
					}
				}
				return true
			})
		}
	}
}

func stripIndex(e ast.Expr) ast.Expr {
	switch ix := e.(type) {
	case *ast.IndexExpr:
		return ix.X
	case *ast.IndexListExpr:
		return ix.X
	}
	return e
}

// sigParams lists a signature's receiver and parameters.
func sigParams(sig *types2.Signature) []*types2.Var {
	var out []*types2.Var
	if r := sig.Recv(); r != nil {
		out = append(out, r)
	}
	for i := 0; i < sig.Params().Len(); i++ {
		out = append(out, sig.Params().At(i))
	}
	return out
}

func funcName(obj *types2.Func) string {
	sig := obj.Type().(*types2.Signature)
	if r := sig.Recv(); r != nil {
		t := r.Type()
		ptr := ""
		if p, ok := t.(*types2.Pointer); ok {
			t = p.Elem()
			ptr = "*"
		}
		if n, ok := types2.Unalias(t).(*types2.Named); ok {
			if ptr != "" {
				return "(*" + n.Obj().Name() + ")." + obj.Name()
			}
			return n.Obj().Name() + "." + obj.Name()
		}
	}
	return obj.Name()
}

func (a *analyzer) typeOf(e ast.Expr) types2.Type {
	if e == nil {
		return nil
	}
	if tv, ok := a.types[e]; ok && tv.Type != nil {
		return tv.Type
	}
	if id, ok := e.(*ast.Ident); ok {
		if obj := a.objOf(id); obj != nil {
			return obj.Type()
		}
	}
	return nil
}

func (a *analyzer) objOf(id *ast.Ident) types2.Object {
	if obj := a.uses[id]; obj != nil {
		return obj
	}
	return a.defs[id]
}

// mayStartTasks reports whether code reachable from the program's start
// may start a task: a task statement, or the runtime's check that a func
// value may start one (rt.Go, time.AfterFunc). Calls through func values
// may reach any function of their type used as a value. It records the
// units reachable, the only ones analysed.
func (a *analyzer) mayStartTasks() bool {
	reach := map[*unit]bool{}
	var work []*unit
	add := func(u *unit) {
		if u != nil && !reach[u] {
			reach[u] = true
			work = append(work, u)
		}
	}
	for _, f := range a.inits {
		add(f.unit)
	}
	if m := a.prog.Main; m != nil {
		if obj, ok := m.Types.Scope().Lookup("main").(*types2.Func); ok {
			if f := a.funcs[obj]; f != nil {
				add(f.unit)
			}
		}
	}
	spawns := false
	for len(work) > 0 {
		u := work[len(work)-1]
		work = work[:len(work)-1]
		if !spawns && a.unitSpawns(u) {
			if debug {
				fmt.Fprintf(os.Stderr, "race: %s may start a task\n", u.fns[len(u.fns)-1].name)
			}
			spawns = true
		}
		for c := range u.callee {
			add(c)
		}
		for _, f := range a.values {
			if reach[f.unit] {
				continue
			}
			if u.dynAll {
				add(f.unit)
				continue
			}
			for _, sig := range u.dynSigs {
				if sameShape(f.sig, sig) {
					add(f.unit)
					break
				}
			}
		}
	}
	a.reach = reach
	return spawns
}

// unitSpawns reports whether a unit's code starts a task itself.
func (a *analyzer) unitSpawns(u *unit) bool {
	found := false
	for _, f := range u.fns {
		if f.body == nil || found {
			continue
		}
		ast.Inspect(f.body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.TaskStmt:
				found = true
			case *ast.Ident:
				if fn, ok := a.uses[n].(*types2.Func); ok && scoped.StartsTaskWithFunc(fn) {
					found = true
				}
			}
			return !found
		})
	}
	return found
}

// run computes every summary: units in the order of the call graph, each
// strongly connected group of units until its summaries stop changing.
func (a *analyzer) run() {
	// a.callGraph has run.
	idx := 0
	var stack []*unit
	var sccs [][]*unit
	var visit func(u *unit)
	visit = func(u *unit) {
		u.index, u.low = idx+1, idx+1
		idx++
		stack = append(stack, u)
		u.onstk = true
		for _, c := range sortedUnits(u.callee) {
			if c.index == 0 {
				visit(c)
				u.low = min(u.low, c.low)
			} else if c.onstk {
				u.low = min(u.low, c.index)
			}
		}
		if u.low == u.index {
			var scc []*unit
			for {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				top.onstk = false
				scc = append(scc, top)
				if top == u {
					break
				}
			}
			sccs = append(sccs, scc)
		}
	}
	for _, u := range a.units {
		if u.index == 0 && a.reach[u] {
			visit(u)
		}
	}
	// Calls through func values are not edges of the graph (they could
	// reach any function used as a value, which would make most of the
	// program one group), so the whole order is repeated until nothing
	// changes.
	// A unit is analysed again only when something it depends on changed:
	// a callee's summary, or, for units that call func values (which are
	// not edges of the graph), any function used as a value.
	callers := map[*unit][]*unit{}
	for _, u := range a.units {
		for c := range u.callee {
			callers[c] = append(callers[c], u)
		}
	}
	valued := map[*unit]bool{}
	for _, f := range a.values {
		valued[f.unit] = true
	}
	dirty := map[*unit]bool{}
	for u := range a.reach {
		dirty[u] = true
	}
	for round := 0; round < 10 && len(dirty) > 0; round++ {
		for _, scc := range sccs {
			for pass := 0; pass <= 30; pass++ {
				var todo []*unit
				for _, u := range scc {
					if dirty[u] {
						todo = append(todo, u)
					}
				}
				if len(todo) == 0 {
					break
				}
				for _, u := range todo {
					delete(dirty, u)
					if !a.analyzeUnit(u) {
						continue
					}
					if debug {
						f := u.fns[len(u.fns)-1]
						fmt.Fprintf(os.Stderr, "race: round %d pass %d changed %s (%d accesses, %d edges, %d spawns, %d crossings, %d calls; %d type accesses)\n", round, pass, f.name, len(f.sum.accesses), len(f.sum.edges), len(f.sum.spawns), len(f.sum.cross), len(f.sum.calls), len(a.typeSeen))
					}
					for _, c := range callers[u] {
						dirty[c] = true
					}
					if valued[u] {
						for d := range a.reach {
							if d.dynamic {
								dirty[d] = true
							}
						}
					}
				}
			}
		}
	}
	// The program: initialisation of every package in order, then main.
	w := newWalker(a, a.program)
	w.runProgram()
	a.program.sum = w.sum
}

func sortedUnits(m map[*unit]bool) []*unit {
	var out []*unit
	for u := range m {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return unitPos(out[i]) < unitPos(out[j]) })
	return out
}

func unitPos(u *unit) token.Pos {
	f := u.fns[len(u.fns)-1]
	switch {
	case f.decl != nil:
		return f.decl.Pos()
	case f.lit != nil:
		return f.lit.Pos()
	}
	return 0
}

// analyzeUnit walks every function of u until its variables stop growing,
// and reports whether any summary changed.
func (a *analyzer) analyzeUnit(u *unit) bool {
	changed := false
	for i := 0; i < 20; i++ {
		u.grew = false
		for _, f := range u.fns {
			if f.bodyless {
				continue
			}
			w := newWalker(a, f)
			w.run()
			if !w.sum.equal(f.sum) {
				f.sum = w.sum
				changed = true
			}
		}
		if !u.grew {
			break
		}
		changed = true
	}
	return changed
}

// callGraph records, for each unit, the units whose summaries it uses.
func (a *analyzer) callGraph() {
	for _, u := range a.units {
		u.callee = map[*unit]bool{}
		for _, f := range u.fns {
			body := f.body
			if f.inits {
				var nodes []ast.Node
				for _, init := range f.pkg.Info.InitOrder {
					nodes = append(nodes, init.Rhs)
				}
				for _, file := range f.pkg.Files {
					for _, d := range file.Decls {
						if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "init" && fd.Recv == nil {
							if fn := a.funcs[a.defs[fd.Name].(*types2.Func)]; fn != nil {
								u.callee[fn.unit] = true
							}
						}
					}
				}
				for _, n := range nodes {
					a.calleesOf(u, n)
				}
				continue
			}
			if f.body != nil {
				a.calleesOf(u, body)
			}
		}
		delete(u.callee, u)
	}
}

func (a *analyzer) calleesOf(u *unit, n ast.Node) {
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if fn, ok := a.uses[n].(*types2.Func); ok {
				if f := a.funcs[fn.Origin()]; f != nil && f.unit != nil {
					u.callee[f.unit] = true
				}
			}
		case *ast.CallExpr:
			if sig, ok := a.typeOf(n.Fun).Underlying().(*types2.Signature); ok && !a.isStaticCall(n) {
				u.dynamic = true
				u.dynSigs = append(u.dynSigs, sig)
			}
		case *ast.RangeStmt:
			// A range over a function or a container's iterator calls the
			// iterator, which may be any function value of its type.
			u.dynamic = true
			if T := a.typeOf(n.X); T != nil {
				if sig, ok := T.Underlying().(*types2.Signature); ok {
					u.dynSigs = append(u.dynSigs, sig)
				} else if types2.RangesByMethod(T) {
					u.dynAll = true
				}
			}
		case *ast.SelectorExpr:
			if sel := a.selections[n]; sel != nil {
				if fn, ok := sel.Obj().(*types2.Func); ok {
					if f := a.funcs[fn.Origin()]; f != nil && f.unit != nil {
						u.callee[f.unit] = true
					}
					if r := fn.Type().(*types2.Signature).Recv(); r != nil && isInterface(r.Type()) || isTypeParamRecv(sel) {
						for _, c := range a.byName[fn.Name()] {
							u.callee[c.unit] = true
						}
					}
				}
			}
		}
		return true
	})
}

func isTypeParamRecv(sel *types2.Selection) bool {
	T := sel.Recv()
	if p, ok := T.(*types2.Pointer); ok {
		T = p.Elem()
	}
	_, ok := types2.Unalias(T).(*types2.TypeParam)
	return ok
}

// isStaticCall reports whether a call names a declared function or method
// (or is a conversion or builtin).
func (a *analyzer) isStaticCall(c *ast.CallExpr) bool {
	tv := a.types[c.Fun]
	if tv.IsType() || tv.IsBuiltin() {
		return true
	}
	switch f := ast.Unparen(stripIndex(c.Fun)).(type) {
	case *ast.Ident:
		_, ok := a.uses[f].(*types2.Func)
		return ok
	case *ast.SelectorExpr:
		if sel := a.selections[f]; sel != nil {
			return sel.Kind() != types2.FieldVal
		}
		_, ok := a.uses[f.Sel].(*types2.Func)
		return ok
	case *ast.FuncLit:
		return true
	}
	return false
}

func isInterface(T types2.Type) bool {
	if T == nil {
		return false
	}
	if _, ok := types2.Unalias(T).(*types2.TypeParam); ok {
		return false
	}
	_, ok := T.Underlying().(*types2.Interface)
	return ok
}

func isGlobal(v *types2.Var) bool {
	return v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
}

// pos converts a position for messages.
func (a *analyzer) pos(p token.Pos) token.Position { return a.fset.Position(p) }

// short returns "main.p:12" for a position.
func (a *analyzer) short(p token.Pos) string {
	ps := a.fset.Position(p)
	name := ps.Filename
	if rest, ok := strings.CutPrefix(name, load.StdPrefix); ok {
		name = rest
	} else if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return name + ":" + itoa(ps.Line)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// add records an error once per key.
func (a *analyzer) add(key string, d *diag.Diagnostic) {
	if a.errSeen[key] {
		return
	}
	a.errSeen[key] = true
	a.errs = append(a.errs, d)
}
