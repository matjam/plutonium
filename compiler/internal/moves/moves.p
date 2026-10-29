// Translated by go2pu from internal/moves/moves.go at the go-cutover tag; edited by hand since.

// Package moves checks that owners are used as owners: a value of an owner
// type (language spec section 18.5) moves when it is assigned, passed,
// returned, sent or stored, and the variable it left holds nothing until it
// is given a new value. Using such a variable is an error, as is moving an
// owner out of anything but a whole variable, copying owners with copy or
// a range loop, dropping one through a view, or taking one out of an
// interface that only lends it.
//
// The check follows each function's control flow. A variable's state at a
// point is live, moved, or moved on some paths only; where paths meet the
// states join, and a loop is walked until the states at its head stop
// changing. Every error is explained with the line of the move that caused
// it (spec section 21.1). Dropping what is left at the end of a scope is the
// business of lowering, which the checker tells what is known to hold
// nothing (empty.go).
package moves

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/load"
	"compiler/internal/scoped"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/sort"
)

// Info is what lowering needs to know to destroy owners (spec section 18.6):
// the uses of variables that move their value out, after which lowering
// clears the variable so that dropping it at the end of its scope does
// nothing, and the owner values an expression makes that nothing stores,
// which are dropped at the end of their statement.
type Info struct {
	Moves map[*ast.Ident]bool
	Temps map[ast.Expr]bool
	// DeferredDrops are the deferred calls x.Drop() of owner variables:
	// they drop x when the function returns.
	DeferredDrops map[*ast.CallExpr]bool
	// AssertMoves are the comma-ok assertions v, ok := x.(T) that move an
	// owner out of the owning interface variable x: x is cleared where the
	// assertion succeeds. SwitchMoves are the type switches with a binding
	// on an owning interface variable, which move it into the binding in
	// every clause (language spec section 7.9).
	AssertMoves map[*ast.TypeAssertExpr]bool
	SwitchMoves map[*ast.TypeSwitchStmt]bool

	// What is known to hold nothing (empty.go), so that lowering drops
	// neither a variable that was moved away on every path, nor one that
	// was never given a value, nor the parts of a variable or of a new
	// box's object that were never given one. Empty maps a place where
	// control leaves scopes (a block, a case or select clause, a statement
	// with an implicit block, a return or a branch statement) to what is
	// known there. AtStmt does the same for a call inside the statement
	// that leaves the function because it ended with rt.ExitTask.
	// EmptyResults are the named results that hold nothing before a return
	// statement replaces them, and Fresh the targets of assignments whose
	// old value is known to be nothing.
	Empty        map[ast.Node]Known
	AtStmt       map[ast.Stmt]Known
	EmptyResults map[*ast.ReturnStmt]VarSet
	Fresh        map[ast.Expr]bool
}

// A VarSet is a set of variables.
type VarSet map[*types2.Var]bool

// Known maps variables to the only parts of them that may hold values.
type Known map[*types2.Var]Held

// Held says which parts of a variable may hold values: only Parts of the
// variable, none if Parts is empty; or, if Box is set, the variable holds
// an owned box whose object may hold values only in Parts.
type Held struct {
	Box   bool
	Parts []Path
}

// A Path names a part of a value, one step at a time: ".i" is field i of a
// struct, "[k]" element k of an array, and "[*]" an element whose index is
// not known.
type Path []string

// AnalyzeWith runs the move checker and returns what lowering needs, and
// the errors as a diag.List, or nil. calls is what the escape analysis
// found about calls (internal/escape2, CallFacts): which views of a local
// owner a call keeps past its return (views.go). With calls nil every call
// is taken at its worst.
func AnalyzeWith(prog *load.Program, calls *Calls) (*Info, error) {
	c := pu.New(checker{
		calls: calls,
		info: pu.New(Info{Moves: map[*ast.Ident]bool{}, Temps: map[ast.Expr]bool{}, DeferredDrops: map[*ast.CallExpr]bool{},
			AssertMoves: map[*ast.TypeAssertExpr]bool{}, SwitchMoves: map[*ast.TypeSwitchStmt]bool{},
			Empty: map[ast.Node]Known{}, AtStmt: map[ast.Stmt]Known{}, EmptyResults: map[*ast.ReturnStmt]VarSet{},
			Fresh: map[ast.Expr]bool{}}),
		fset:       prog.Fset,
		types:      map[ast.Expr]types2.TypeAndValue{},
		uses:       map[*ast.Ident]types2.Object{},
		defs:       map[*ast.Ident]types2.Object{},
		selections: map[*ast.SelectorExpr]*types2.Selection{},
		implicits:  map[ast.Node]types2.Object{},
		seen:       map[string]bool{},
		scoped:     scoped.Analyze(prog),
	})
	for _, p := range prog.Packages {
		for k, v := range p.Info.Types {
			c.types[k] = v
		}
		for k, v := range p.Info.Uses {
			c.uses[k] = v
		}
		for k, v := range p.Info.Defs {
			c.defs[k] = v
		}
		for k, v := range p.Info.Selections {
			c.selections[k] = v
		}
		for k, v := range p.Info.Implicits {
			c.implicits[k] = v
		}
	}
	c.findKeeps(prog)
	for _, p := range prog.Packages {
		c.pkg = p.Types
		for _, file := range p.Files {
			for _, d := range file.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					if d.Body != nil {
						var sig *types2.Signature
						if obj, ok := c.defs[d.Name].(*types2.Func); ok {
							sig, _ = obj.Type().(*types2.Signature)
						}
						c.checkFunc(d.Type, d.Recv, d.Body, sig)
					}
				case *ast.GenDecl:
					c.checkPackageVars(d)
				}
			}
		}
	}
	sort.SortStableFunc(c.errs, func(a, b *diag.Diagnostic) bool {
		pi, pj := a.Pos, b.Pos
		if pi.Filename != pj.Filename {
			return pi.Filename < pj.Filename
		}
		if pi.Line != pj.Line {
			return pi.Line < pj.Line
		}
		return pi.Column < pj.Column
	})
	return c.info, c.errs.Err()
}

type checker struct {
	info       *Info
	fset       *token.FileSet
	types      map[ast.Expr]types2.TypeAndValue
	uses       map[*ast.Ident]types2.Object
	defs       map[*ast.Ident]types2.Object
	selections map[*ast.SelectorExpr]*types2.Selection
	implicits  map[ast.Node]types2.Object
	pkg        *types2.Package // the package being checked, for type names

	errs diag.List
	seen map[string]bool

	scoped     *scoped.Info // the scoped tasks of sync.Scope (scope.go)
	taskBodies map[*ast.BlockStmt]*ast.FuncLit
	calls      *Calls                      // what calls keep of their arguments (views.go)
	owned      map[string][]types2.Type    // ownedLeaves, by owner type
	keeps      map[*types2.Func][]keepSite // what functions keep of the memory their inputs reach (keeps.go)
}

// report records an error once per position and code.
func (c *checker) report(d *diag.Diagnostic) {
	k := compat.Concat(d.Pos.String(), string(d.Code))
	if c.seen[k] {
		return
	}
	c.seen[k] = true
	c.errs = diag.List(compat.Append(c.errs, d))
}

func (c *checker) typeOf(e ast.Expr) types2.Type {
	if e == nil {
		return nil
	}
	if tv, ok := c.types[e]; ok && tv.Type != nil {
		return tv.Type
	}
	if id, ok := e.(*ast.Ident); ok {
		if obj := c.objOf(id); obj != nil {
			return obj.Type()
		}
	}
	return nil
}

func (c *checker) objOf(id *ast.Ident) types2.Object {
	if obj := c.uses[id]; obj != nil {
		return obj
	}
	return c.defs[id]
}

// isOwner reports whether values of T move.
func isOwner(T types2.Type) bool { return T != nil && types2.IsOwner(T) }

func isGlobal(v *types2.Var) bool {
	return v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
}

// checkPackageVars checks the initialisers of package variables: moving an
// owner out of another package variable is the only error possible there.
func (c *checker) checkPackageVars(d *ast.GenDecl) {
	if d.Tok != token.VAR {
		return
	}
	f := &fn{c: c, report: true, env: env{}, vars: map[*types2.Var]bool{}, heads: map[ast.Node]env{}, checkedAt: map[*ast.Ident]ast.Expr{}}
	for _, s := range d.Specs {
		vs := s.(*ast.ValueSpec)
		for i, val := range vs.Values {
			var T types2.Type
			if len(vs.Values) == len(vs.Names) {
				if v, ok := c.defs[vs.Names[i]].(*types2.Var); ok {
					T = v.Type()
				}
			}
			f.consume(val, T, move{kind: mvAssign, to: vs.Names[min(i, len(vs.Names)-1)].Name})
		}
	}
}

// ----------------------------------------------------------------------------
// States

// A state is what a variable holds at a point of the program.
type state uint8

const (
	live  state = iota // a value (the zero value counts)
	moved              // nothing: the value moved away on every path here
	maybe              // nothing on some paths here
)

// A moveKind says how a value left a variable, for messages.
type moveKind uint8

const (
	mvAssign      moveKind = iota // n := m, n = m
	mvArg                         // passed by value
	mvRecv                        // the receiver of a method with a value receiver
	mvReturn                      // returned
	mvSend                        // sent on a channel
	mvGo                          // passed to a task statement
	mvStore                       // stored in a field, element or composite literal
	mvDrop                        // m.Drop()
	mvConv                        // converted to another owner type
	mvMethodValue                 // bound into a method value
	mvAssert                      // asserted out of an owning interface
	mvGoRecv                      // the owned box a task statement calls a method of
	mvTask                        // taken by a scoped task (s.Task)
	mvNewValue                    // given a new value (views.go)
	mvTake                        // emptied by take(&x) (views.go)
	mvScopeEnd                    // dropped at the end of its block (views.go)
	mvReclaim                     // a local arena's memory reclaimed by Reset or Release (views.go)
)

// A move records where and how a variable's value left it.
type move struct {
	pos  token.Pos
	kind moveKind
	to   string      // the receiving variable, place, channel or function
	typ  types2.Type // mvArg: the parameter type; mvRecv: the receiver type
	loop bool        // carried around a loop from a previous iteration
}

type vstate struct {
	st state
	mv *move
	// What is known to be zero, for drops (empty.go): z for the variable's
	// own storage, bz for the object an owned box variable holds. nil
	// means nothing is known.
	z, bz *zset
	// dd says that a deferred x.Drop() of the variable is registered on
	// every path here.
	dd bool
	// views are the views of the function's owners the variable may hold,
	// and pin, for an owner, a view of it kept where the walk cannot follow
	// (views.go).
	views []view
	pin   *pinMark
	// hpin, for an owner, is a view of what it holds on the heap that a
	// call may keep: the owner may not then move to another task, where
	// the kept view would let two tasks use that memory (section 8.8).
	hpin *pinMark
	// dl says that a deferred function literal using the variable is
	// registered on some path here: the literal runs when the function
	// returns, so the variable must hold a value then (section 18.5).
	dl *deferMark
}

// A deferMark records the use of an owner by a deferred function literal.
type deferMark struct {
	use  *ast.Ident // the use in the literal, or the view in the arguments
	stmt token.Pos  // the defer statement
	kind deferKind
	via  string // deferStored: the variable holding the literal
}

// deferKind says how a deferred call uses an owner.
type deferKind int

const (
	deferLiteral deferKind = iota // a function literal called by the defer statement
	deferView                     // a view of the owner in the receiver or arguments
	deferStored                   // a function literal held in a variable
)

// plain reports whether s says nothing beyond live.
func (s vstate) plain() bool {
	return s.st == live && s.z == nil && s.bz == nil && !s.dd && len(s.views) == 0 && s.pin == nil && s.hpin == nil && s.dl == nil
}

// An env maps the variables that do not hold a value, or of which more is
// known (empty.go), to their state; a variable not in it is live. A nil env
// is a point that cannot be reached.
type env map[*types2.Var]vstate

func (e env) clone() env {
	if e == nil {
		return nil
	}
	out := make(env, len(e))
	for k, v := range e {
		out[k] = v
	}
	return out
}

// join merges the states of two paths.
func join(a, b env) env {
	if a == nil {
		return b.clone()
	}
	if b == nil {
		return a.clone()
	}
	out := env{}
	for v, sa := range a {
		if s := joinState(sa, b[v]); !s.plain() {
			out[v] = s
		}
	}
	for v, sb := range b {
		if _, ok := a[v]; !ok {
			if s := joinState(vstate{}, sb); !s.plain() {
				out[v] = s
			}
		}
	}
	return out
}

// joinState merges a variable's states on two paths; the zero vstate is
// live with nothing more known.
func joinState(sa, sb vstate) vstate {
	var out vstate
	switch {
	case sa.st == live && sb.st == live:
	case sa.st == live:
		out = vstate{st: maybe, mv: sb.mv}
	case sb.st == live:
		out = vstate{st: maybe, mv: sa.mv}
	case sa.st == moved && sb.st == moved:
		out = vstate{st: moved, mv: sa.mv}
	default:
		mv := sa.mv
		if sa.st == maybe && sa.mv != nil && sa.mv.loop {
			mv = sa.mv
		} else if sb.st == maybe && sb.mv != nil && sb.mv.loop {
			mv = sb.mv
		}
		out = vstate{st: maybe, mv: mv}
	}
	out.z = sa.z.join(sb.z)
	out.bz = sa.bz.join(sb.bz)
	out.dd = sa.dd && sb.dd
	out.views = joinViews(sa.views, sb.views)
	out.pin = sa.pin
	if out.pin == nil {
		out.pin = sb.pin
	}
	out.hpin = sa.hpin
	if out.hpin == nil {
		out.hpin = sb.hpin
	}
	out.dl = sa.dl
	if out.dl == nil {
		out.dl = sb.dl
	}
	return out
}

func envEqual(a, b env) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for k, v := range a {
		u, ok := b[k]
		if !ok || u.st != v.st || u.dd != v.dd || !u.z.equal(v.z) || !u.bz.equal(v.bz) ||
			!viewsEqual(u.views, v.views) || (u.pin == nil) != (v.pin == nil) || (u.hpin == nil) != (v.hpin == nil) || (u.dl == nil) != (v.dl == nil) {
			return false
		}
	}
	return true
}

// ----------------------------------------------------------------------------
// Functions

// A fn is the walk of one function body, or of a function literal's body.
type fn struct {
	c      *checker
	sig    *types2.Signature
	report bool // the final walk: report errors
	env    env
	// consumed is the expression consume is about to walk, whose owner
	// value moves where it goes rather than being a temporary.
	consumed ast.Expr

	// vars are the variables of this body (parameters, results, locals),
	// as opposed to variables of an enclosing function that a literal
	// captures.
	vars    map[*types2.Var]bool
	results []*types2.Var // named results

	// heads holds, for each loop and label, the states that reach it from
	// later in the body (back edges, gotos), from the previous walk.
	heads   map[ast.Node]env
	changed bool

	targets []*target
	// blocks are the blocks being walked, innermost last, for deferred
	// calls that use owners of inner blocks.
	blocks  []*ast.BlockStmt
	declIn  map[*types2.Var]*ast.BlockStmt
	topBody *ast.BlockStmt
	// noFacts are the variables for which nothing is known to be empty
	// (empty.go).
	noFacts map[*types2.Var]bool
	// kept are the variables whose own storage something other than the
	// walk may use: an address kept, a function literal or a deferred call
	// that uses them (empty.go).
	kept map[*types2.Var]bool
	// holders: the owners used by the function literals that local
	// variables may hold (litHolders), computed when a deferred call needs
	// them.
	holders map[*types2.Var][]ownerUse

	// For views (views.go): argKey maps call arguments without their
	// parentheses to the expressions the escape analysis keys them by;
	// checkedAt is, for each identifier, the largest expression naming a
	// part of it, where its views are checked; writing is the target of
	// the assignment being walked, which is written rather than read.
	argKey    map[ast.Expr]ast.Expr
	checkedAt map[*ast.Ident]ast.Expr
	writing   ast.Expr
}

// A target is a statement that break or continue may leave.
type target struct {
	label  string
	isLoop bool
	breaks env
	conts  env
}

// checkFunc checks a function body with signature sig.
func (c *checker) checkFunc(ft *ast.FuncType, recv *ast.FieldList, body *ast.BlockStmt, sig *types2.Signature) {
	f := &fn{c: c, sig: sig, vars: map[*types2.Var]bool{}, heads: map[ast.Node]env{}, declIn: map[*types2.Var]*ast.BlockStmt{}, topBody: body,
		argKey: keysOf(c, body), checkedAt: map[*ast.Ident]ast.Expr{}}
	// The function's own variables: parameters, results, and everything
	// declared in its body outside nested literals.
	for _, l := range []*ast.FieldList{recv, ft.Params, ft.Results} {
		if l == nil {
			continue
		}
		for _, field := range l.List {
			for _, name := range field.Names {
				if v, ok := c.defs[name].(*types2.Var); ok {
					f.vars[v] = true
					if l == ft.Results {
						f.results = compat.Append(f.results, v)
					}
				}
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.Ident:
			if v, ok := c.defs[n].(*types2.Var); ok {
				f.vars[v] = true
			}
		case *ast.CaseClause:
			if v, ok := c.implicits[n].(*types2.Var); ok {
				f.vars[v] = true
			}
		}
		return true
	})
	f.findNoFacts(body)
	if lit := c.litOf(body); lit != nil {
		// A scoped task's literal owns the variables it takes.
		if t := c.scoped.Lits[lit]; t != nil {
			for v := range t.Taken {
				f.vars[v] = true
				f.noFacts[v], f.kept[v] = true, true
			}
		}
	}
	for i := 0; i < 50; i++ {
		f.changed = false
		f.walk(body)
		if !f.changed {
			break
		}
	}
	f.report = true
	f.walk(body)
}

func (f *fn) walk(body *ast.BlockStmt) {
	f.env = env{}
	f.targets = nil
	f.blocks = nil
	for _, v := range f.results {
		f.declaredEmpty(v) // results start with nothing
	}
	f.block(body)
	f.deferExit() // falling off the end of the body
}

// note merges a state reaching a loop head or label from later in the body.
func (f *fn) note(n ast.Node, e env) {
	if e == nil {
		return
	}
	j := join(f.heads[n], e)
	if f.heads[n] == nil {
		j = e.clone()
	}
	if !envEqual(j, f.heads[n]) {
		f.heads[n] = j
		f.changed = true
	}
}

// ----------------------------------------------------------------------------
// Variables

// varOf returns the local variable an identifier names, or nil.
func (f *fn) varOf(id *ast.Ident) *types2.Var {
	v, ok := f.c.objOf(id).(*types2.Var)
	if !ok || v.IsField() || isGlobal(v) {
		return nil
	}
	return v
}

// captured reports whether v belongs to a function around this literal.
func (f *fn) captured(v *types2.Var) bool { return !f.vars[v] }

// use checks that v holds a value at id.
func (f *fn) use(id *ast.Ident, v *types2.Var) { f.useKeeping(id, v, false) }

// useKeeping is use; keep says the use only reads a part of v that is not
// an owner, which cannot give v a value, so what is known to be zero in it
// stays known (empty.go).
func (f *fn) useKeeping(id *ast.Ident, v *types2.Var, keep bool) {
	if f.env == nil || f.captured(v) || !isOwner(v.Type()) {
		return
	}
	s, ok := f.env[v]
	if !ok {
		return
	}
	if !keep && (s.z != nil || s.bz != nil) {
		// Any other use may give it a value: nothing is known to be zero
		// now.
		s.z, s.bz = nil, nil
		f.setState(v, s)
	}
	if s.st == live || !f.report {
		return
	}
	f.c.report(f.c.useDiag(id, v, s))
}

// setState records v's state.
func (f *fn) setState(v *types2.Var, s vstate) {
	if f.env == nil {
		return
	}
	if s.plain() {
		delete(f.env, v)
		return
	}
	f.env[v] = s
}

// moveVar moves v's value out at id.
func (f *fn) moveVar(id *ast.Ident, v *types2.Var, gomv move) {
	mv := pu.New(gomv)

	if mv.kind != mvDrop && f.c.scoped.Scopes[v] != nil && f.report {
		f.c.report(f.c.scopeMovedDiag(id, v, f.c.scoped.Scopes[v]))
	}
	if f.captured(v) {
		if f.report {
			f.c.report(f.c.capturedDiag(id, v))
		}
		return
	}
	f.use(id, v)
	f.checkView(id)
	if f.env != nil {
		mv.pos = id.Pos()
		// The value left: the variable holds nothing (empty.go), and the
		// views of it are stale (views.go).
		f.env[v] = vstate{st: moved, mv: mv, z: emptyZ, dd: f.env[v].dd, pin: f.env[v].pin, hpin: f.env[v].hpin, dl: f.env[v].dl}
		f.invalidate(v, (*mv))
	}
	if f.report {
		f.c.info.Moves[id] = true
	}
}

// moveOut records that v, read at id, no longer holds a value, without
// making lowering clear it where id is read: the caller arranges that.
func (f *fn) moveOut(id *ast.Ident, v *types2.Var, gomv move) {
	mv := pu.New(gomv)

	if f.captured(v) {
		if f.report {
			f.c.report(f.c.capturedDiag(id, v))
		}
		return
	}
	if f.env != nil {
		mv.pos = id.Pos()
		// Where the assertion fails the variable still holds its value, so
		// nothing is known to be empty.
		f.env[v] = vstate{st: moved, mv: mv, dd: f.env[v].dd, pin: f.env[v].pin, hpin: f.env[v].hpin, dl: f.env[v].dl}
		f.invalidate(v, (*mv))
	}
}

// assignVar gives v a value. It holds no views until the assignment says
// which (views.go).
func (f *fn) assignVar(v *types2.Var) {
	if f.env != nil {
		f.setState(v, vstate{dd: f.env[v].dd, pin: f.env[v].pin, hpin: f.env[v].hpin, dl: f.env[v].dl})
	}
}
