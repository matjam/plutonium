// Translated by go2pu from internal/race/race.go at the go-cutover tag; edited by hand since.

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
// Interrupt handlers the program installs (x86.InterruptGate, x86.TrapGate) are parties of
// the first three rules like tasks that may run several at once; the
// allocators a handler may reach are followed too (region.go). The regions
// of section 19.3 (preemption or interrupts disabled, an rt.Lock held) are
// found as pseudo locks, and the uses of //pu:percpu variables are checked
// against them (E2022, E2023; section 20.9) instead of by rule 1.
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
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/load"
	"compiler/internal/moves"
	"compiler/internal/scoped"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/fmt"
	"pluto/os"
	"pluto/slices"
	"pluto/sort"
	"pluto/strings"
)

// maxErrors stops a run that has gone badly wrong from printing pages.
const maxErrors = 50

// debug prints the progress of the analysis (PLUTO_RACE_DEBUG=1).
var debug = compat.Getenv("PLUTO_RACE_DEBUG") != ""

// analyzing is held while a program is analysed (AnalyzeWith).
var analyzing compat.Mutex

// CheckWith analyses a loaded program that passed the escape analysis and
// returns the races it finds, as a diag.List, or nil. mv is the move
// checker's account of moves, which tells where the code that starts
// scoped tasks moves or drops a variable they may use (scope.go).
func CheckWith(prog *load.Program, mv *moves.Info) error {
	_, err := AnalyzeWith(prog, mv)
	return err
}

// AnalyzeWith is CheckWith, which also returns the checks that lowering must
// make before the scoped tasks it accepted start: slices that may overlap
// and that tasks use only in the same partition form (spec section 19.4,
// Memory reached through views).
func AnalyzeWith(prog *load.Program, mv *moves.Info) (scoped.Overlaps, error) {
	// loadLimit (path.go) is package state that run changes while it
	// widens a unit, so analyses in one process take turns.
	analyzing.Lock()
	defer analyzing.Unlock()
	a := newAnalyzer(prog)
	a.moves = mv
	if debug {
		compat.Fprintfb(os.Stderr, "race: %d units\n", compat.ArgI(int64(len(a.units))))
	}
	a.callGraph()
	starts := a.mayStartTasks()
	// A release of a lock that is not held is an error in any program
	// (section 19.3, Releasing a lock).
	a.checkReleases()
	a.checkCopies()
	if !starts {
		// One task alone cannot race.
		return nil, a.errs.Err()
	}
	a.run()
	if debug {
		fmt.Fprintf(os.Stderr, "race: summaries done; checking\n")
	}
	a.check()
	if debug {
		fmt.Fprintf(os.Stderr, "race: checking scopes\n")
	}
	a.checkScopes()
	sort.SortStableFunc(a.errs, func(a, b *diag.Diagnostic) bool {
		pi, pj := a.Pos, b.Pos
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
	// caps are the variables of enclosing functions a literal uses. In
	// its walk, the value of caps[k] is that of a parameter numbered
	// len(params)+k, which the caller supplies (capArgs).
	caps []*types2.Var
	// capVals holds what the literal assigns to its captures, in the
	// terms of its own walk (assignCap).
	capVals map[*types2.Var]origins
	// crossCalled is set when a function of another unit calls the
	// literal through a func value, which cannot supply its captures
	// (capArgs).
	crossCalled bool
	// crossBy holds the units whose walks made those calls (nil: a
	// walk outside any unit).
	crossBy map[*unit]bool
	pkg     *load.Package
	unit    *unit

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
	dynSigs []*types2.Signature // the types of the func values it calls
	dynAll  bool                // a call that may reach any function used as a value
	users   map[*unit]bool      // the units whose walks applied a summary of this one
	widened bool                // walked with places cut after one load (run)
	index   int                 // Tarjan
	low     int
	onstk   bool
}

type analyzer struct {
	subs  int // places substituted at call sites (debug)
	round int // the round of run (debug)
	// capSites holds the call arguments whose origins were cut: once cut,
	// always, so the summaries settle.
	capSites map[capSite]bool
	// applyMemo holds what each call instantiated at the last walk.
	applyMemo map[applyKey]*applyMemo
	prog      *load.Program
	fset      *token.FileSet

	// dirty holds the units run walks again (run).
	dirty map[*unit]bool

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
	valuesAt            map[*unit][]*function  // the values each unit makes (mayStartTasks)
	program             *function
	inits               []*function

	// Per-CPU variables and regions (region.go).
	cpuOn       bool                   // the program has a per-CPU variable outside the runtime
	cpuVars     map[*types2.Var]bool   // the per-CPU variables outside the runtime
	cpuUses     map[token.Pos]*cpuUse  // every use of one, by the position of the identifier
	regionSets  []lockset              // the sets of regions held where pointers to them were taken
	regionIDs   map[string]int         // their numbers, by content
	handlers    []*function            // the interrupt handlers the program installs
	allocState  map[string]*types2.Var // the shared object of each allocator type (region.go)
	unsafeTypes map[*types2.Named]bool // allocator types written in unsafe code (region.go)
	syncAllocs  map[*types2.Named]bool // allocator types that synchronize themselves (region.go)
	allocFns    []*function            // the Alloc and Free methods of installed allocators (when there are handlers)
	allocDone   bool

	// Stores that depend on a load of the same per-CPU variable (dep.go).
	cpuLoads     []*cpuLoad
	cpuLoadAt    map[token.Pos]int
	cpuLoadCall  map[loadKey]int
	cpuCallLoads map[*ast.CallExpr][]int
	cpuDeps      map[depKey]*cpuDepSite
	cpuDepCalls  map[*ast.CallExpr][]*cpuDepSite
	cpuFn        map[*function]*cpuFnInfo
	cpuNever     map[*function]int8               // the functions that never return (dep.go)
	rangeCalls   map[*ast.RangeStmt]*ast.CallExpr // the calls that ranges over funcs make (dep.go)
	cpuLeave     map[*function]bool               // the functions that can leave their caller without returning to it

	owned   ownedViews // view fields that own what they point to (ownedview.go)
	declPkg map[*types2.Package]*load.Package

	// Writes through views, by the type and field written (sendable.go),
	// and writes of package variables, which count when their address is
	// taken.
	viewWrites   map[string]*writeSite
	globalWrites map[*types2.Var]map[string]*writeSite
	addrGlobals  map[*types2.Var]bool
	gstores      map[*types2.Var][]gstore // the values stored in each package variable (loadCrossings)
	preWrites    []*access                // writes of package variables' places before any task starts (staticFields)
	initLits     map[ast.Node]bool        // composite literals whose address a package initializer stores: static object candidates
	statics      map[ast.Node]bool        // the static objects: initLits that no code moves, drops or overwrites once tasks run
	freedTypes   map[string]*writeSite    // the struct types of objects freed, dropped or moved once tasks run (the free condition)
	arenaTypes   []types2.Type            // the types allocated by alloc.New, Owned, Shared, Synced and the arenas' New and Slice (bulkFreed)
	plainWrites  plainSites               // the plain writes of package variables once tasks run (plainWritten)
	guardedW     map[string]bool          // the shared writes of package variables that a guard set guards (writeKey)
	converted    map[string]types2.Type   // types converted to interfaces
	convSites    map[string]*convSite     // one conversion of each type that can reach another task
	typeIDs      []types2.Type            // referent types of synced[T], and map types, numbered for paths
	crossMaps    map[int]bool             // map type numbers a crossing value reaches (checkMaps)
	// Maps kept in place (mapplace.go): the occurrence that takes the map
	// out of each package variable and field place that is not kept.
	mapVarOut     map[*types2.Var]*mapOut
	mapFieldOut   map[string]*mapOut
	mapFieldKept  map[string]bool
	mapFieldLabel map[string]string
	// arenaVars: for each function that starts scoped tasks, the variables
	// it sets to an object of an arena's New or Slice, and that arena
	// (allocmem.go).
	arenaVars   map[*function]map[*types2.Var][]vpath
	arenaTakes  map[*function]map[*types2.Var][]*types2.Var // arena variables set by take of others (arenaMoves)
	entryTypes  map[*types2.Var][]types2.Type               // types with drop entries in each named Fixed or Chunked arena, nil key for the rest (arenaEntryTypes)
	dropFns     map[types2.Type][]*function                 // the Drop methods a type's drop glue reaches (dropGlueFns)
	arenaRes    map[*function]bool                          // functions that may return an object of an arena's New or Slice (mayArenaResult)
	allocUse    map[*function]bool                          // functions whose bodies call alloc.Use (callsAllocUse)
	tpArgs      map[*types2.TypeParam][]types2.Type         // the type arguments each type parameter is instantiated with
	typeAcc     map[string][]*typeAccess                    // uses of each field of objects, for the type rule
	typeSeen    map[typeSeenKey]bool
	resultNodes map[resultKey]ast.Node // the memory a call's fresh results are

	errs    diag.List
	errSeen map[string]bool
}

func newAnalyzer(prog *load.Program) *analyzer {
	a := pu.New(analyzer{
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
		gstores:      map[*types2.Var][]gstore{},
		initLits:     map[ast.Node]bool{},
		globalWrites: map[*types2.Var]map[string]*writeSite{},
		converted:    map[string]types2.Type{},
		errSeen:      map[string]bool{},
		tpArgs:       map[*types2.TypeParam][]types2.Type{},
		typeAcc:      map[string][]*typeAccess{},
		crossMaps:    map[int]bool{},
		typeSeen:     map[typeSeenKey]bool{},
		resultNodes:  map[resultKey]ast.Node{},
		views:        map[token.Pos]*types2.Var{},
		overlaps:     scoped.Overlaps{},
	})
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
			if f, ok := p.Info.Uses[id].(*types2.Func); ok && arenaAllocFunc(f) {
				a.arenaTypes = compat.Append(a.arenaTypes, inst.TypeArgs.At(0))
			}
			for i := 0; i < tps.Len() && i < inst.TypeArgs.Len(); i++ {
				a.tpArgs[tps.At(i)] = compat.Append(a.tpArgs[tps.At(i)], inst.TypeArgs.At(i))
			}
		}
		a.declPkg[p.Types] = p
	}
	a.program = pu.New(function{name: "the program", program: true, sum: pu.New(summary{})})
	for _, p := range prog.Packages {
		init := pu.New(function{name: compat.Concat(p.Path, " initialization"), pkg: p, inits: true, sum: pu.New(summary{})})
		a.inits = compat.Append(a.inits, init)
		u := pu.New(unit{fns: compat.Clone([]*function{init}), vars: map[*types2.Var]origins{}})
		init.unit = u
		a.units = compat.Append(a.units, u)
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
				fn := pu.New(function{
					name:     funcName(obj),
					obj:      obj,
					decl:     fd,
					body:     fd.Body,
					pkg:      p,
					sig:      obj.Type().(*types2.Signature),
					bodyless: fd.Body == nil,
					sum:      pu.New(summary{}),
				})
				fn.params = sigParams(fn.sig)
				a.funcs[obj] = fn
				if fd.Recv != nil {
					a.byName[obj.Name()] = compat.Append(a.byName[obj.Name()], fn)
				}
				u := pu.New(unit{vars: map[*types2.Var]origins{}})
				if fd.Body != nil {
					a.collectLits(u, fn, fd.Body)
				}
				u.fns = compat.Append(u.fns, fn)
				fn.unit = u
				a.units = compat.Append(a.units, u)
			}
		}
	}
	a.scanValues()
	a.scanPerCPU()
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
		fn := pu.New(function{
			name: compat.Concat("the function literal in ", outer.name),
			lit:  lit,
			body: lit.Body,
			pkg:  outer.pkg,
			sig:  sig,
			unit: u,
			sum:  pu.New(summary{}),
		})
		fn.params = sigParams(sig)
		fn.caps = a.litCaps(lit)
		a.lits[lit] = fn
		a.collectLits(u, fn, lit.Body)
		u.fns = compat.Append(u.fns, fn)
		return false
	})
}

// litCaps returns the variables declared outside lit, other than package
// variables, that its body uses, in the order of their first use.
func (a *analyzer) litCaps(lit *ast.FuncLit) []*types2.Var {
	var out []*types2.Var
	seen := map[*types2.Var]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := a.objOf(id).(*types2.Var)
		if !ok || isGlobal(v) || v.IsField() || seen[v] || (v.Pos() >= lit.Pos() && v.Pos() < lit.End()) {
			return true
		}
		seen[v] = true
		out = compat.Append(out, v)
		return true
	})
	return out
}

// capIdx returns the parameter number of captured variable v in f's walk,
// or -1.
func (f *function) capIdx(v *types2.Var) int {
	for k, c := range f.caps {
		if c == v {
			return len(f.params) + k
		}
	}
	return -1
}

// scanValues finds the functions and literals used as values, the
// candidates of calls through func values whose origin is unknown.
func (a *analyzer) scanValues() {
	seen := map[*function]bool{}
	a.valuesAt = map[*unit][]*function{}
	for pi, p := range a.prog.Packages {
		var site *unit // the unit whose code is scanned
		note := func(f *function) {
			if f == nil {
				return
			}
			if !seen[f] {
				seen[f] = true
				a.values = compat.Append(a.values, f)
			}
			if site != nil && !slices.Contains(a.valuesAt[site], f) {
				a.valuesAt[site] = compat.Append(a.valuesAt[site], f)
			}
		}
		init := a.inits[pi].unit
		for _, file := range p.Files {
			called := map[ast.Expr]bool{}
			for _, d := range file.Decls {
				site = init
				if fd, ok := d.(*ast.FuncDecl); ok {
					if obj, ok := p.Info.Defs[fd.Name].(*types2.Func); ok && a.funcs[obj] != nil {
						site = a.funcs[obj].unit
					}
				}
				ast.Inspect(d, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.CallExpr:
						called[ast.Unparen(stripIndex(n.Fun))] = true
						if len(n.Args) == 1 && onceDoCall(p.Info.Selections, n) {
							// Do calls its argument there and keeps it
							// nowhere (onceDo): it is not a value that
							// other calls may reach.
							switch arg := ast.Unparen(n.Args[0]).(type) {
							case *ast.FuncLit, *ast.SelectorExpr:
								called[arg] = true
							}
						}
					case *ast.Ident:
						if called[n] {
							return true
						}
						if fn, ok := p.Info.Uses[n].(*types2.Func); ok {
							note(a.funcs[fn.Origin()])
						}
					case *ast.SelectorExpr:
						if called[n] {
							// The selected name is part of the call, not a
							// value of its own.
							called[n.Sel] = true
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
}

// onceDoCall reports whether call e is o.Do(f) for a sync.Once o.
func onceDoCall(sels map[*ast.SelectorExpr]*types2.Selection, e *ast.CallExpr) bool {
	se, ok := ast.Unparen(e.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	sel := sels[se]
	if sel == nil || sel.Kind() != types2.MethodVal {
		return false
	}
	fn, ok := sel.Obj().(*types2.Func)
	if !ok || fn.Name() != "Do" {
		return false
	}
	n := recvNamed(fn)
	return n != nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "pluto/sync" && n.Obj().Name() == "Once"
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
		out = compat.Append(out, r)
	}
	for i := 0; i < sig.Params().Len(); i++ {
		out = compat.Append(out, sig.Params().At(i))
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
				return compat.Concat("(*", n.Obj().Name(), ").", obj.Name())
			}
			return compat.Concat(n.Obj().Name(), ".", obj.Name())
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
	// A function used as a value can be called through a func value only
	// once code that makes the value is reachable.
	live := map[*function]bool{}
	var dyn []*unit
	add := func(u *unit) {
		if u != nil && !reach[u] {
			reach[u] = true
			work = compat.Append(work, u)
			for _, f := range a.valuesAt[u] {
				live[f] = true
			}
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
	// C code may call an exported function at any time: it is an entry
	// of a party (check), so what it reaches is analysed too.
	for _, f := range a.exportedFuncs() {
		add(f.unit)
	}
	spawns := false
	seenHandler := map[*function]bool{}
	drain := func() {
		for len(work) > 0 {
			u := work[len(work)-1]
			work = work[:len(work)-1]
			unitSpawns, installs := a.scanUnit(u)
			if !spawns && unitSpawns {
				if debug {
					compat.Fprintfb(os.Stderr, "race: %s may start a task\n", compat.ArgS(u.fns[len(u.fns)-1].name))
				}
				spawns = true
			}
			// An interrupt handler the program installs runs beside every
			// task, so its code is part of the program (spec section 19.3).
			for _, h := range installs {
				if !seenHandler[h] {
					seenHandler[h] = true
					a.handlers = compat.Append(a.handlers, h)
					add(h.unit)
				}
			}
			for c := range u.callee {
				add(c)
			}
			if u.dynAll || len(u.dynSigs) > 0 {
				dyn = compat.Append(dyn, u)
			}
		}
	}
	// settle drains the work list, then lets the values that are live reach
	// the calls through func values found so far, until nothing is added.
	settle := func() {
		for {
			drain()
			for _, u := range dyn {
				for _, f := range a.values {
					if reach[f.unit] || !live[f] {
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
			if len(work) == 0 {
				return
			}
		}
	}
	settle()
	if len(a.handlers) > 0 {
		// A handler that allocates calls the Alloc and Free of the
		// allocators the program installs (section 20.10): they are part
		// of the program.
		for _, f := range a.allocatorMethods() {
			add(f.unit)
		}
		settle()
	}
	a.reach = reach
	// One task alone cannot race, but a handler is a party beside it, and
	// the uses of per-CPU variables are checked in every program.
	return spawns || len(a.handlers) > 0 || a.cpuOn
}

// scanUnit reports whether a unit's code starts a task itself, and which
// interrupt handlers it installs (x86.InterruptGate, x86.TrapGate).
func (a *analyzer) scanUnit(u *unit) (spawns bool, handlers []*function) {
	for _, f := range u.fns {
		if f.body == nil {
			continue
		}
		ast.Inspect(f.body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.TaskStmt:
				spawns = true
			case *ast.CallExpr:
				if len(n.Args) == 3 {
					if h, _ := a.handlerOf(n); h != nil {
						handlers = compat.Append(handlers, h)
					}
				}
			case *ast.Ident:
				if fn, ok := a.uses[n].(*types2.Func); ok && scoped.StartsTaskWithFunc(fn) {
					spawns = true
				}
			}
			return true
		})
	}
	return spawns, handlers
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
		stack = compat.Append(stack, u)
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
				scc = compat.Append(scc, top)
				if top == u {
					break
				}
			}
			sccs = compat.Append(sccs, scc)
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
	// a callee's summary, or a summary its last walk applied through a
	// func value (calls through func values are not edges of the graph),
	// which apply records in the callee's users.
	callers := map[*unit][]*unit{}
	for _, u := range a.units {
		for c := range u.callee {
			callers[c] = compat.Append(callers[c], u)
		}
	}
	dirty := map[*unit]bool{}
	a.dirty = dirty
	for u := range a.reach {
		dirty[u] = true
	}
	for round := 0; round < 10 && len(dirty) > 0; round++ {
		a.round = round
		for _, scc := range sccs {
			for pass := 0; pass <= 30; pass++ {
				if pass == widenPass && len(scc) > 1 {
					// The group is slow to settle: its places are cut
					// shorter from here on, in every later walk too,
					// which only makes them stand for more memory.
					for _, u := range scc {
						u.widened = true
					}
				}
				var todo []*unit
				for _, u := range scc {
					if dirty[u] {
						todo = compat.Append(todo, u)
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
						compat.Fprintfb(os.Stderr, "race: round %d pass %d changed %s (%d accesses, %d typed, %d edges, %d spawns, %d crossings, %d calls; %d type accesses)\n", compat.ArgI(int64(round)), compat.ArgI(int64(pass)), compat.ArgS(f.name), compat.ArgI(int64(len(f.sum.accesses))), compat.ArgI(int64(len(f.sum.typed))), compat.ArgI(int64(len(f.sum.edges))), compat.ArgI(int64(len(f.sum.spawns))), compat.ArgI(int64(len(f.sum.cross))), compat.ArgI(int64(len(f.sum.calls))), compat.ArgI(int64(len(a.typeSeen))))
					}
					for _, c := range callers[u] {
						dirty[c] = true
					}
					// The units that applied u's summary through func
					// values or interfaces: no other unit read it.
					for c := range u.users {
						if a.reach[c] {
							dirty[c] = true
						}
					}
				}
			}
		}
	}
	// The program: initialisation of every package in order, then main.
	if debug {
		fmt.Fprintf(os.Stderr, "race: walking the program\n")
	}
	w := newWalker(a, a.program)
	w.runProgram()
	a.program.sum = w.sum
}

func sortedUnits(m map[*unit]bool) []*unit {
	var out []*unit
	for u := range m {
		out = compat.Append(out, u)
	}
	sort.SortFunc(out, func(a, b *unit) bool { return unitPos(a) < unitPos(b) })
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
	if u.widened {
		loadLimit = 1
		defer func() { loadLimit = maxLoads }()
	}
	changed := false
	for i := 0; i < 20; i++ {
		u.grew = false
		for _, f := range u.fns {
			if f.bodyless {
				continue
			}
			w := newWalker(a, f)
			subs0 := a.subs
			w.run()
			if debug && a.subs-subs0 > 1000000 {
				compat.Fprintfb(os.Stderr, "race: heavy walk %s iter %d: %d substituted places, %d accesses\n", compat.ArgS(f.name), compat.ArgI(int64(i)), compat.ArgI(int64(a.subs-subs0)), compat.ArgI(int64(len(w.sum.accesses))))
			}
			if a.round >= 2 {
				// Past the first rounds a summary only grows, so that
				// the rounds end (keep).
				w.sum.keep(f.sum)
			}
			if !w.sum.equal(f.sum) {
				if debug && a.round >= 3 {
					compat.Fprintfb(os.Stderr, "race: flip %s: %s\n", compat.ArgS(f.name), compat.ArgS(w.sum.diff(f.sum)))
				}
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
					nodes = compat.Append[ast.Node](nodes, init.Rhs)
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
			if f.obj != nil && entryRun(f.obj) {
				// However it is called, a Reset, Release or Drop of an
				// arena runs the Drop methods of its drop entries.
				for _, d := range a.entryFns(nil, nil) {
					u.callee[d.unit] = true
				}
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
				u.dynSigs = compat.Append(u.dynSigs, sig)
			}
		case *ast.RangeStmt:
			// A range over a function or a container's iterator calls the
			// iterator, which may be any function value of its type.
			if T := a.typeOf(n.X); T != nil {
				if sig, ok := T.Underlying().(*types2.Signature); ok {
					u.dynSigs = compat.Append(u.dynSigs, sig)
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
					if entryRun(fn) {
						// The Drop methods the arena's drop entries run.
						for _, d := range a.entryFns(nil, nil) {
							u.callee[d.unit] = true
						}
					}
					if sig := fn.Type().(*types2.Signature); sig.Recv() != nil && isInterface(sig.Recv().Type()) || isTypeParamRecv(sel) {
						// The methods the call may reach, as the walker
						// finds them (methodCandidates).
						for _, c := range a.methodCandidates(fn.Name(), sig) {
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
		name = strings.SliceFrom(name, i+1)
	}
	return compat.Concat(name, ":", itoa(ps.Line))
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
	return compat.String(b[i:])
}

// add records an error once per key.
func (a *analyzer) add(key string, d *diag.Diagnostic) {
	if a.errSeen[key] {
		return
	}
	a.errSeen[key] = true
	a.errs = diag.List(compat.Append(a.errs, d))
}
