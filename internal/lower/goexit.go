package lower

import (
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// rt.Goexit (language spec section 17.3).
//
// rt.Goexit ends the calling task after running the deferred calls of every
// frame on it and releasing what each frame holds, as Go's runtime.Goexit
// does. It is not an unwinder. Goexit sets a flag in the task record and
// returns; after every call that may end that way, the caller takes the
// flag (rt.takeGoexit) and, if it was set, leaves through its ordinary
// return path, which runs its deferred calls and releases its variables
// exactly as a return there would, and then sets the flag again for its own
// caller (rt.setGoexit). The flag reaches the bottom of the task, where
// rt.taskMain ends the task, or pluto.main, where the main task ends
// (rt.mainGoexit), or rt.CatchGoexit, which stops it (the test runner).
//
// While a frame runs its deferred calls on the way out, the flag is clear,
// so those calls run normally; a deferred call that itself ends with
// Goexit lets the remaining ones run, and the frame leaves for Goexit
// afterwards, as in Go.
//
// The check costs a call and a branch, so it is made only after calls that
// may end with Goexit. Which those are is worked out from the source of the
// whole program before lowering: a function may end with Goexit if it
// calls rt.Goexit, a function that may, or makes a call through a func
// value or an interface that may reach one. An indirect call may reach
// every function, method, function literal and range-over-func loop body
// that may end with Goexit and whose type it could hold: a func value of an
// identical signature, or for an interface (or type parameter) method a
// method of the same name and signature. Type parameters match any type, so
// generic code is covered whatever it is instantiated with. Code that cannot
// reach rt.Goexit pays nothing; a program that never calls it has no checks
// at all.

// gxBoundary says what a function does when a call it makes ends with
// Goexit, if it does not simply leave.
type gxBoundary uint8

const (
	gxLeave gxBoundary = iota // leave through the return path (the usual case)
	gxMain                    // pluto.main: the main task ends (rt.mainGoexit)
	gxCatch                   // rt.CatchGoexit, rt.taskMain, the panic handler: the exit stops here
	gxC                       // a //pu:export wrapper: C cannot be left this way
)

// A gxNode is a function, method, function literal or range-over-func loop
// body, for the analysis.
type gxNode struct {
	sig    *types2.Signature // its type, without the receiver
	method string            // its name, for a method
	edges  []gxEdge          // the calls it makes
	may    bool              // it may end with Goexit
	// value says a func value may hold it: a function literal, a loop
	// body, or a declared function or method whose value the program
	// takes; vsigs are then the types of those values.
	value bool
	vsigs []*types2.Signature
}

// A gxEdge is one call: of a declared function, through a func value of
// type fsig, or of method name (with signature msig) through an interface
// or a type parameter.
type gxEdge struct {
	fn   *types2.Func
	fsig *types2.Signature
	name string
	msig *types2.Signature
}

// goexitInfo is the result of the analysis.
type goexitInfo struct {
	active bool         // the program calls rt.Goexit somewhere
	catch  *types2.Func // rt.CatchGoexit
	// taskMain is the bottom of every task but main's: when the function
	// it runs leaves for Goexit, the task ends as if it had returned.
	taskMain *types2.Func
	funcs    map[*types2.Func]*gxNode
	members  []*gxNode // the nodes that may end with Goexit
}

// analyzeGoexit works out which functions of the program may end with
// rt.Goexit.
func (l *Lowerer) analyzeGoexit() {
	gx := &goexitInfo{funcs: map[*types2.Func]*gxNode{}}
	l.gx = gx
	goexit := l.rtFuncOrNil("Goexit")
	catch := l.rtFuncOrNil("CatchGoexit")
	gx.catch = catch
	gx.taskMain = l.rtFuncOrNil("taskMain")
	if goexit == nil {
		return
	}
	for _, p := range l.prog.Packages {
		for id, obj := range p.Info.Uses {
			if obj == goexit && id.Pos().IsValid() {
				gx.active = true
			}
		}
	}
	if !gx.active {
		return
	}
	var nodes []*gxNode
	newNode := func(sig *types2.Signature) *gxNode {
		n := &gxNode{sig: plainSigOf(sig)}
		nodes = append(nodes, n)
		return n
	}
	newValue := func(sig *types2.Signature) *gxNode {
		n := newNode(sig)
		n.value = true
		n.vsigs = []*types2.Signature{n.sig}
		return n
	}
	// called holds the identifiers that name the function a call or go
	// statement calls: every other use of a declared function takes its
	// value.
	called := map[*ast.Ident]bool{}
	markCalled := func(info *types2.Info, fun ast.Expr) {
		fun = ast.Unparen(fun)
		switch ix := fun.(type) {
		case *ast.IndexExpr:
			fun = ast.Unparen(ix.X)
		case *ast.IndexListExpr:
			fun = ast.Unparen(ix.X)
		}
		switch f := fun.(type) {
		case *ast.Ident:
			called[f] = true
		case *ast.SelectorExpr:
			if sel := info.Selections[f]; sel == nil || sel.Kind() == types2.MethodVal {
				called[f.Sel] = true
			}
		}
	}
	var walk func(info *types2.Info, root ast.Node, cur *gxNode)
	walk = func(info *types2.Info, root ast.Node, cur *gxNode) {
		ast.Inspect(root, func(x ast.Node) bool {
			switch x := x.(type) {
			case *ast.FuncLit:
				if sig, ok := info.Types[x].Type.(*types2.Signature); ok {
					walk(info, x.Body, newValue(sig))
				}
				return false
			case *ast.TaskStmt:
				// The call runs on another task; only its operands are
				// evaluated here.
				markCalled(info, x.Call.Fun)
				walk(info, x.Call.Fun, cur)
				for _, a := range x.Call.Args {
					walk(info, a, cur)
				}
				return false
			case *ast.RangeStmt:
				iter, yield, edges := gxRangeIterator(info, x.X)
				if iter == nil {
					return true
				}
				walk(info, x.X, cur)
				cur.edges = append(cur.edges, edges...)
				walk(info, x.Body, newValue(yield))
				return false
			case *ast.CallExpr:
				markCalled(info, x.Fun)
				if e, ok := gxClassify(info, x); ok {
					cur.edges = append(cur.edges, e)
				}
			}
			return true
		})
	}
	for _, p := range l.prog.Packages {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				fn, ok := p.Info.Defs[fd.Name].(*types2.Func)
				if !ok {
					continue
				}
				n := newNode(fn.Type().(*types2.Signature))
				if fn.Type().(*types2.Signature).Recv() != nil {
					n.method = fn.Name()
				}
				gx.funcs[fn] = n
				if fn == catch || fn == gx.taskMain {
					continue // never ends with Goexit itself
				}
				if fn == goexit {
					n.may = true
					gx.members = append(gx.members, n)
					continue
				}
				walk(p.Info, fd.Body, n)
			}
		}
	}
	// Bodyless compiler hooks execute their bound source implementation.
	// Include those edges before propagating exits, so callers run cleanup
	// instead of consuming a zero result returned by an exited hook.
	for fn, declaration := range l.decls {
		if declaration.decl.Body != nil {
			continue
		}
		if method, _, ok := schedHook(fn); ok {
			if target, _ := schedMethod(l.sched, method); target != nil {
				n := newNode(fn.Type().(*types2.Signature))
				n.edges = []gxEdge{{fn: target.Origin()}}
				gx.funcs[fn] = n
			}
		} else if direct, parked, ok := lateHookNames(fn); ok {
			n := newNode(fn.Type().(*types2.Signature))
			for _, name := range []string{direct, parked} {
				if target := l.rtFuncOrNil(name); target != nil {
					n.edges = append(n.edges, gxEdge{fn: target.Origin()})
				}
			}
			gx.funcs[fn] = n
		}
	}
	// Declared functions and methods whose values the program takes.
	for _, p := range l.prog.Packages {
		for id, obj := range p.Info.Uses {
			fn, ok := obj.(*types2.Func)
			if !ok || called[id] {
				continue
			}
			n := gx.funcs[fn.Origin()]
			if n == nil || n.value {
				continue
			}
			n.value = true
			// A method value has the method's type; a method
			// expression takes the receiver first.
			n.vsigs = append(n.vsigs, n.sig, plainSig(fn.Origin().Type().(*types2.Signature)))
		}
	}
	for changed := true; changed; {
		changed = false
		for _, n := range nodes {
			if n.may {
				continue
			}
			for _, e := range n.edges {
				if gx.mayEdge(e) {
					n.may = true
					gx.members = append(gx.members, n)
					changed = true
					break
				}
			}
		}
	}
}

// rtFuncOrNil returns the named function of package pluto/rt, or nil.
func (l *Lowerer) rtFuncOrNil(name string) *types2.Func {
	for _, p := range l.prog.Packages {
		if p.Path == "pluto/rt" {
			fn, _ := p.Types.Scope().Lookup(name).(*types2.Func)
			return fn
		}
	}
	return nil
}

// mayEdge reports whether the call e may end with Goexit.
func (gx *goexitInfo) mayEdge(e gxEdge) bool {
	switch {
	case e.fn != nil:
		return gx.mayFunc(e.fn)
	case e.fsig != nil:
		return gx.maySig(e.fsig)
	}
	return gx.mayMethod(e.name, e.msig)
}

// mayFunc reports whether a call of the declared function fn may end with
// Goexit.
func (gx *goexitInfo) mayFunc(fn *types2.Func) bool {
	if !gx.active {
		return false
	}
	n := gx.funcs[fn.Origin()]
	return n != nil && n.may
}

// maySig reports whether a call through a func value of type sig may end
// with Goexit.
func (gx *goexitInfo) maySig(sig *types2.Signature) bool {
	if !gx.active {
		return false
	}
	for _, m := range gx.members {
		for _, vs := range m.vsigs {
			if gxSigMatch(vs, sig) {
				return true
			}
		}
	}
	return false
}

// mayMethod reports whether a call of method name, of signature sig,
// through an interface or a type parameter may end with Goexit.
func (gx *goexitInfo) mayMethod(name string, sig *types2.Signature) bool {
	if !gx.active {
		return false
	}
	for _, m := range gx.members {
		if m.method == name && gxSigMatch(m.sig, sig) {
			return true
		}
	}
	return false
}

// gxClassify describes the call e for the analysis; false for conversions
// and calls of builtins.
func gxClassify(info *types2.Info, e *ast.CallExpr) (gxEdge, bool) {
	tv, ok := info.Types[e.Fun]
	if !ok || tv.IsType() || tv.IsBuiltin() {
		return gxEdge{}, false
	}
	fun := ast.Unparen(e.Fun)
	switch ix := fun.(type) {
	case *ast.IndexExpr:
		if _, ok := info.Types[ix.X].Type.(*types2.Signature); ok {
			fun = ast.Unparen(ix.X)
		}
	case *ast.IndexListExpr:
		fun = ast.Unparen(ix.X)
	}
	if se, ok := fun.(*ast.SelectorExpr); ok {
		if _, ok := info.Uses[se.Sel].(*types2.Variant); ok {
			return gxEdge{}, false
		}
		if sel := info.Selections[se]; sel != nil {
			if fn, ok := sel.Obj().(*types2.Func); ok && sel.Kind() != types2.FieldVal {
				if isIfaceMethod(fn) {
					return gxEdge{name: fn.Name(), msig: fn.Type().(*types2.Signature)}, true
				}
				return gxEdge{fn: fn.Origin()}, true
			}
		}
	}
	if id := staticFuncIdent(fun, info); id != nil {
		if fn, ok := info.Uses[id].(*types2.Func); ok {
			return gxEdge{fn: fn.Origin()}, true
		}
	}
	if sig, ok := tv.Type.Underlying().(*types2.Signature); ok {
		return gxEdge{fsig: sig}, true
	}
	return gxEdge{}, false
}

// gxRangeIterator returns, for a range over a function or a container, the
// iterator's signature, the yield function's, and the calls the loop
// statement makes to get and call the iterator; nil for other ranges.
func gxRangeIterator(info *types2.Info, x ast.Expr) (*types2.Signature, *types2.Signature, []gxEdge) {
	tv, ok := info.Types[x]
	if !ok || tv.Type == nil {
		return nil, nil, nil
	}
	yieldOf := func(iter *types2.Signature) *types2.Signature {
		if iter.Params().Len() != 1 {
			return nil
		}
		y, _ := iter.Params().At(0).Type().Underlying().(*types2.Signature)
		return y
	}
	if iter, ok := tv.Type.Underlying().(*types2.Signature); ok {
		y := yieldOf(iter)
		if y == nil {
			return nil, nil, nil
		}
		if id := staticFuncIdent(x, info); id != nil {
			if fn, ok := info.Uses[id].(*types2.Func); ok {
				return iter, y, []gxEdge{{fn: fn.Origin()}}
			}
		}
		return iter, y, []gxEdge{{fsig: iter}}
	}
	switch tv.Type.Underlying().(type) {
	case *types2.Slice, *types2.Array, *types2.Pointer, *types2.Chan, *types2.Map, *types2.Basic:
		return nil, nil, nil
	}
	obj, _, _ := types2.LookupFieldOrMethod(tv.Type, true, nil, types2.RangeMethodName)
	all, ok := obj.(*types2.Func)
	if !ok {
		return nil, nil, nil
	}
	res := all.Type().(*types2.Signature).Results()
	if res.Len() != 1 {
		return nil, nil, nil
	}
	iter, ok := res.At(0).Type().Underlying().(*types2.Signature)
	if !ok {
		return nil, nil, nil
	}
	y := yieldOf(iter)
	if y == nil {
		return nil, nil, nil
	}
	first := gxEdge{fn: all.Origin()}
	if isIfaceMethod(all) {
		first = gxEdge{name: all.Name(), msig: all.Type().(*types2.Signature)}
	}
	return iter, y, []gxEdge{first, {fsig: iter}}
}

// plainSigOf returns sig without its receiver.
func plainSigOf(sig *types2.Signature) *types2.Signature {
	if sig.Recv() == nil && sig.TypeParams() == nil {
		return sig
	}
	return types2.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
}

// gxSigMatch reports whether a function of signature a could be called
// through a func value or method of signature b. A type parameter on
// either side matches any type, since generic code may be instantiated
// with it; the rest must be identical.
func gxSigMatch(a, b *types2.Signature) bool {
	a, b = plainSigOf(a), plainSigOf(b)
	return a.Variadic() == b.Variadic() && gxTupleMatch(a.Params(), b.Params()) && gxTupleMatch(a.Results(), b.Results())
}

func gxTupleMatch(a, b *types2.Tuple) bool {
	if a.Len() != b.Len() {
		return false
	}
	for i := 0; i < a.Len(); i++ {
		if !gxTypeMatch(a.At(i).Type(), b.At(i).Type(), 0) {
			return false
		}
	}
	return true
}

// gxTypeMatch reports whether types a and b are identical but for type
// parameters, which match anything.
func gxTypeMatch(a, b types2.Type, depth int) bool {
	a, b = types2.Unalias(a), types2.Unalias(b)
	if _, ok := a.(*types2.TypeParam); ok {
		return true
	}
	if _, ok := b.(*types2.TypeParam); ok {
		return true
	}
	if depth > 8 {
		return true // deep enough: assume they may match
	}
	switch a := a.(type) {
	case *types2.Pointer:
		b, ok := b.(*types2.Pointer)
		return ok && gxTypeMatch(a.Elem(), b.Elem(), depth+1)
	case *types2.Slice:
		b, ok := b.(*types2.Slice)
		return ok && gxTypeMatch(a.Elem(), b.Elem(), depth+1)
	case *types2.Array:
		b, ok := b.(*types2.Array)
		return ok && a.Len() == b.Len() && gxTypeMatch(a.Elem(), b.Elem(), depth+1)
	case *types2.Chan:
		b, ok := b.(*types2.Chan)
		return ok && a.Dir() == b.Dir() && gxTypeMatch(a.Elem(), b.Elem(), depth+1)
	case *types2.Signature:
		b, ok := b.(*types2.Signature)
		return ok && a.Variadic() == b.Variadic() && gxTupleMatch(a.Params(), b.Params()) && gxTupleMatch(a.Results(), b.Results())
	case *types2.Named:
		b, ok := b.(*types2.Named)
		if !ok || a.Origin().Obj() != b.Origin().Obj() {
			return false
		}
		aa, ba := a.TypeArgs(), b.TypeArgs()
		if aa.Len() != ba.Len() {
			return false
		}
		for i := 0; i < aa.Len(); i++ {
			if !gxTypeMatch(aa.At(i), ba.At(i), depth+1) {
				return false
			}
		}
		return true
	}
	return types2.Identical(a, b)
}

// ----------------------------------------------------------------------------
// Lowering

// deferMayGoexit reports whether the call of a defer statement may end
// with Goexit.
func (l *Lowerer) deferMayGoexit(s *fnState, call *ast.CallExpr) bool {
	if !l.gx.active {
		return false
	}
	e, ok := gxClassify(s.info, call)
	return ok && l.gx.mayEdge(e)
}

// callMayGoexit reports whether the call c, of signature sig, may end with
// Goexit, so that the caller must check for it.
func (s *fnState) callMayGoexit(c callee, sig *types2.Signature) bool {
	gx := s.l.gx
	if !gx.active || c.deferred || c.noReturn || s.gxBoundary == gxCatch {
		return false
	}
	if s.gxBoundary == gxMain {
		return true
	}
	if c.sym != nil {
		if fn := s.l.symFuncs[c.sym]; fn != nil {
			return gx.mayFunc(fn)
		}
		if fn := s.l.gxWrapped[c.sym]; fn != nil {
			return gx.mayFunc(fn)
		}
		return false
	}
	if c.site != nil && c.site.iface != nil {
		if it, ok := c.site.iface.Underlying().(*types2.Interface); ok && c.site.method < it.NumMethods() {
			m := it.Method(c.site.method)
			return gx.mayMethod(m.Name(), m.Type().(*types2.Signature))
		}
		return true
	}
	if c.site != nil && c.site.fsig != nil {
		sig = c.site.fsig
	}
	return gx.maySig(sig)
}

// checkGoexit follows a call that may end with Goexit: if it did, the
// function leaves (leaveForGoexit).
func (s *fnState) checkGoexit(pos token.Pos) {
	if s.b == nil {
		return
	}
	if s.cleanupGoexit != nil {
		// Required cleanup keeps running when a helper raises another exit.
		// Re-entering this frame's return cleanup would release it twice.
		s.noteImplicitGoexit()
		return
	}
	taken := s.callRuntime("takeGoexit", []*ssa.Type{ssa.TBool})[0]
	exit := s.newBlock(ssa.BlockInvalid)
	cont := s.newBlock(ssa.BlockInvalid)
	s.b.Likely = ssa.Unlikely
	s.endIf(taken, exit, cont)
	s.seal(exit)
	s.startBlock(exit)
	s.leaveForGoexit(pos)
	s.seal(cont)
	s.startBlock(cont)
}

// leaveForGoexit leaves the function after a call that ended with Goexit.
func (s *fnState) leaveForGoexit(pos token.Pos) {
	switch {
	case s.gxBoundary == gxMain:
		s.callRuntimeNoReturn("mainGoexit")
	case s.gxBoundary == gxC:
		s.callRuntimeNoReturn("goexitFromC")
	case s.rf != nil:
		// A range-over-func loop body: stop the loop. The iterator, and
		// the function the loop is in, check for Goexit in turn.
		s.store(ssa.TI64, s.capture(s.rf.state), s.constInt(ssa.TI64, rfDone))
		s.store(ssa.TBool, s.results[0], s.constBool(false))
		s.emitReturnFor(token.NoPos, true)
	case len(s.results) < len(s.abi.results):
		// Generated code that returns through returnValues: its results
		// are not used by a caller that is leaving too.
		saved := s.results
		s.results = nil
		for _, p := range s.abi.results {
			addr := s.temp(p.typ, "result")
			s.zero(addr, s.l.sizeof(p.typ))
			s.results = append(s.results, addr)
		}
		s.emitReturnFor(token.NoPos, true)
		s.results = saved
	default:
		// What the move checker knows holds nothing when the statement
		// started, and the statement does not mention, still does.
		var empty moves.Known
		if s.l.moves != nil && s.curStmt != nil {
			empty = s.l.moves.AtStmt[s.curStmt]
		}
		if s.goexitCleared != nil {
			known := make(moves.Known, len(empty)+1)
			for variable, held := range empty {
				known[variable] = held
			}
			known[s.goexitCleared] = moves.Held{}
			empty = known
		}
		s.leavingThrough(empty, func() { s.emitReturnFor(pos, true) })
	}
	s.b = nil
}

// noteGoexit takes the Goexit flag after a deferred call and records it in
// the bool slot gx (runDeferList).
func (s *fnState) noteGoexit(gx *ssa.Value) {
	taken := s.callRuntime("takeGoexit", []*ssa.Type{ssa.TBool})[0]
	s.ifThen(taken, func() { s.store(ssa.TBool, gx, s.constBool(true)) })
}

// beginCleanupGoexit delays an exit until generated destruction has completed.
// Taking the flag between callbacks lets each remaining cleanup run normally.
func (s *fnState) beginCleanupGoexit() {
	if s.l.gx.active {
		s.cleanupGoexit = s.temp(types2.Typ[types2.Bool], "drop.goexit")
		s.store(ssa.TBool, s.cleanupGoexit, s.constBool(false))
	}
}

func (s *fnState) noteImplicitGoexit() {
	if !s.l.gx.active {
		return
	}
	s.implicitGoexits++
	if s.cleanupGoexit != nil {
		s.noteGoexit(s.cleanupGoexit)
	} else {
		s.noteGoexit(s.implicitGoexitSlot())
	}
}

func (s *fnState) implicitGoexitSlot() *ssa.Value {
	if s.implicitGoexit == nil {
		s.implicitGoexit = s.temp(types2.Typ[types2.Bool], "implicit.goexit")
		s.zeroAtEntry(countedSlot{base: s.implicitGoexit, T: types2.Typ[types2.Bool]})
	}
	return s.implicitGoexit
}

// collectCleanupGoexit shares the destruction accumulator while lowering
// mandatory frame, scope, or deferred cleanup. Ordinary call sites still
// propagate an exit immediately; cleanup restores it after all releases.
func (s *fnState) collectCleanupGoexit() func() {
	saved := s.cleanupGoexit
	if s.l.gx.active && saved == nil {
		s.cleanupGoexit = s.implicitGoexitSlot()
	}
	return func() { s.cleanupGoexit = saved }
}

func (s *fnState) checkImplicitGoexit(pos token.Pos) {
	if s.b == nil || s.cleanupGoexit != nil || s.implicitGoexit == nil {
		return
	}
	// A release in an untaken branch cannot consume its caller's flag.
	// Only an exit collected by this frame's implicit cleanup propagates here.
	s.ifThen(s.load(ssa.TBool, s.implicitGoexit), func() {
		s.store(ssa.TBool, s.implicitGoexit, s.constBool(false))
		s.callRuntime("setGoexit", nil)
		s.checkGoexit(pos)
	})
}

func (s *fnState) checkClearedGoexit(pos token.Pos, place ast.Expr) {
	saved := s.goexitCleared
	if id, ok := ast.Unparen(place).(*ast.Ident); ok {
		s.goexitCleared, _ = s.info.Uses[id].(*types2.Var)
	}
	s.checkImplicitGoexit(pos)
	s.goexitCleared = saved
}

func (s *fnState) finishCleanupGoexit() {
	if s.cleanupGoexit != nil {
		s.noteGoexit(s.cleanupGoexit)
		s.passGoexit(s.cleanupGoexit)
	}
}

// passGoexit sets the Goexit flag again for the caller if gx says the
// function is leaving for Goexit.
func (s *fnState) passGoexit(gx *ssa.Value) {
	s.ifThen(s.load(ssa.TBool, gx), func() { s.callRuntime("setGoexit", nil) })
}
