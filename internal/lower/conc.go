package lower

// Lowering of task statements, channel operations and select (language spec
// sections 4.7 and 4.8). Channels are pointers to the runtime's hchan;
// every operation is a call into pluto/rt (chan.p), and select is one call
// over an array of cases laid out in the selecting frame. Operations that
// may block call runtime functions marked //pu:blocking, so that a checker
// of interrupt context sees them in the call graph.

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
	typerr "github.com/matjam/plutonium/internal/types2/errors"
)

// rtType returns the named type of package pluto/rt.
func (l *Lowerer) rtType(name string) types2.Type {
	for _, p := range l.prog.Packages {
		if p.Path == "pluto/rt" {
			if tn, ok := p.Types.Scope().Lookup(name).(*types2.TypeName); ok {
				return tn.Type()
			}
		}
	}
	panic(fmt.Sprintf("lower: pluto/rt.%s is missing", name))
}

// chanElem returns the element type of channel type T.
func chanElem(T types2.Type) types2.Type {
	return unwrap(T).Underlying().(*types2.Chan).Elem()
}

// checkSendable reports a type that crosses tasks in an instance of a
// generic function but is not sendable; the checker reports the others.
func (s *fnState) checkSendable(T types2.Type, pos token.Pos, what string) {
	if s.tmap == nil {
		return
	}
	if bad := types2.Unsendable(T); bad != nil {
		s.l.errorf(pos, diag.CheckerCode(typerr.NotSendable), "%s %s is not sendable: it contains %s, whose count is not atomic, so it cannot cross tasks; use synced[T] (spec section 19.2)", what, T, bad)
	}
}

// ----------------------------------------------------------------------------
// task statements

// goFrameClosures reports the func values of a task statement that are known
// to be closure records in this frame: the function started (fun, unless
// it is a function literal, which taskStmt checks, or a method call) and the
// func-typed arguments. It reports whether there were any. Those it cannot
// tell about are checked when the task statement runs.
func (s *fnState) goFrameClosures(fun ast.Expr, args []ast.Expr) bool {
	found := false
	if id, ok := fun.(*ast.Ident); ok {
		if what := s.frameClosure(id, 0); what != "" {
			s.l.errorf(id.Pos(), diag.GoFrameClosure, "cannot start %s with a task statement: it holds %s, whose closure record lives in the starting frame (spec section 12.12)", id.Name, what)
			found = true
		}
	}
	if s.frameClosureArgs(args, "a task") {
		found = true
	}
	return found
}

// frameClosureArgs reports the func-typed arguments of a call that are
// known to be closure records in this frame, for a call that hands them to
// dest, which starts a task with them. It reports whether there were
// any.
func (s *fnState) frameClosureArgs(args []ast.Expr, dest string) bool {
	found := false
	for _, a := range args {
		if _, ok := s.typeOf(a).Underlying().(*types2.Signature); !ok {
			continue
		}
		what := s.frameClosure(a, 0)
		if what == "" {
			continue
		}
		found = true
		if id, ok := ast.Unparen(a).(*ast.Ident); ok {
			s.l.errorf(a.Pos(), diag.GoFrameClosure, "cannot pass %s to %s: it holds %s, whose closure record lives in the starting frame (spec section 12.12)", id.Name, dest, what)
		} else {
			s.l.errorf(a.Pos(), diag.GoFrameClosure, "cannot pass %s to %s: its closure record lives in the starting frame (spec section 12.12)", what, dest)
		}
	}
	return found
}

// goFuncCall checks a call of a function that starts tasks with
// some of its func arguments (sync.WaitGroup.Task, time.AfterFunc, rt.Go,
// and any function that passes a func parameter on to one of them or to a
// task statement; the closure flow summaries find them): the arguments that
// are known to be closure records in this frame are compile errors, as in
// a task statement. The functions still check the rest when they run.
func (s *fnState) goFuncCall(e *ast.CallExpr, fun ast.Expr) {
	var cands []int
	for i, a := range e.Args {
		if _, ok := s.typeOf(a).Underlying().(*types2.Signature); ok && s.frameClosure(a, 0) != "" {
			cands = append(cands, i)
		}
	}
	if len(cands) == 0 {
		return
	}
	g := calledFunc(s.info, e)
	if g == nil {
		return
	}
	var targs []types2.Type
	nrecv := 0
	if se, ok := fun.(*ast.SelectorExpr); ok && s.info.Selections[se] != nil {
		targs = s.methodTypeArgs(g, se, s.info.TypeOf(se.X))
		nrecv = 1
	} else if id := identOf(fun); id != nil {
		targs = s.instanceArgs(id)
	}
	sum := s.l.closureSummary(g, targs)
	sig := g.Type().(*types2.Signature)
	gp := sig.Params().Len()
	if sig.Recv() != nil {
		gp++
	}
	var args []ast.Expr
	for _, i := range cands {
		if sum.starts.has(min(i+nrecv, gp-1)) {
			args = append(args, e.Args[i])
		}
	}
	s.frameClosureArgs(args, types2.ExprString(e.Fun)+", which starts a task with it")
}

// frameClosure describes e if it is known to be a func value whose closure
// record is in this frame, and returns "" otherwise: a function literal
// that captures variables, a method value, or a local variable whose only
// assignment is its declaration, from one of those. depth bounds the chain
// of variables followed.
func (s *fnState) frameClosure(e ast.Expr, depth int) string {
	if s.staticLits || depth > 8 {
		return ""
	}
	switch e := ast.Unparen(e).(type) {
	case *ast.FuncLit:
		if caps := s.capturedVars(e); len(caps) > 0 {
			return fmt.Sprintf("a function literal that captures %s (%s)", caps[0].Name(), s.l.shortPos(e.Pos()))
		}
	case *ast.SelectorExpr:
		if sel := s.info.Selections[e]; sel != nil && sel.Kind() == types2.MethodVal {
			return "the method value " + types2.ExprString(e)
		}
	case *ast.Ident:
		if v, ok := s.info.Uses[e].(*types2.Var); ok {
			if init := s.onlyValue(v); init != nil {
				return s.frameClosure(init, depth+1)
			}
		}
	case *ast.CallExpr:
		// A function that builds the closures it returns in the region
		// this frame provides (regions.go).
		if g := calledFunc(s.info, e); g != nil && regionSig(g.Type().(*types2.Signature)) {
			f := &closureFlow{l: s.l, info: s.info, tmap: s.tmap, ts: s}
			if _, targs := f.callee(e); s.l.closureSummary(g, targs).builds {
				return fmt.Sprintf("the closure %s returns (%s), built in this frame", shortExpr(e), s.l.shortPos(e.Pos()))
			}
		}
	}
	return ""
}

// onlyValue returns the initializer of v, a variable declared in the body
// of the function being lowered, if that is the only value v ever holds:
// nothing else assigns to v, and nothing takes its address or selects
// through it (a method with a pointer receiver could change it). It
// returns nil otherwise.
func (s *fnState) onlyValue(v *types2.Var) ast.Expr {
	var body *ast.BlockStmt
	switch {
	case s.job == nil:
	case s.job.decl != nil:
		body = s.job.decl.Body
	case s.job.lit != nil:
		body = s.job.lit.Body
	}
	if body == nil || v.Pos() < body.Pos() || v.Pos() >= body.End() {
		return nil
	}
	var init ast.Expr
	ok := true
	uses := func(x ast.Expr) bool {
		id, isID := ast.Unparen(x).(*ast.Ident)
		return isID && s.info.Uses[id] == v
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if id, isID := lhs.(*ast.Ident); isID && s.info.Defs[id] == v {
					if len(n.Rhs) == len(n.Lhs) {
						init = n.Rhs[i]
					} else {
						ok = false
					}
				} else if uses(lhs) {
					ok = false
				}
			}
		case *ast.OrStmt:
			// The value comes from a call that may fail (language spec
			// section 11.27): it is not a plain initializer.
			for _, lhs := range n.Lhs {
				if id, isID := lhs.(*ast.Ident); isID && s.info.Defs[id] == v || uses(lhs) {
					ok = false
				}
			}
		case *ast.ValueSpec:
			for i, name := range n.Names {
				if s.info.Defs[name] == v {
					if len(n.Values) == len(n.Names) {
						init = n.Values[i]
					} else {
						ok = false
					}
				}
			}
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN && (uses(n.Key) || uses(n.Value)) {
				ok = false
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND && uses(n.X) {
				ok = false
			}
		case *ast.SelectorExpr:
			if uses(n.X) {
				ok = false
			}
		}
		return ok
	})
	if !ok {
		return nil
	}
	return init
}

// syncPackages are the packages whose struct types are not copyable for the
// purposes of advice: copying a Mutex, a WaitGroup or an atomic value makes
// a different one, and nothing marks them (they have no noCopy field).
var syncPackages = map[string]bool{"pluto/sync": true, "pluto/sync/atomic": true}

// noCopyType returns the synchronization type that T is or holds by value
// (in a struct field or an array), or nil if copying a T is harmless.
func noCopyType(T types2.Type) types2.Type {
	return noCopyIn(T, map[types2.Type]bool{})
}

func noCopyIn(T types2.Type, seen map[types2.Type]bool) types2.Type {
	T = unwrap(T)
	if seen[T] {
		return nil
	}
	seen[T] = true
	if n, ok := T.(*types2.Named); ok {
		if _, isStruct := n.Underlying().(*types2.Struct); isStruct && n.Obj().Pkg() != nil && syncPackages[n.Obj().Pkg().Path()] {
			return T
		}
	}
	switch u := T.Underlying().(type) {
	case *types2.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if bad := noCopyIn(u.Field(i).Type(), seen); bad != nil {
				return bad
			}
		}
	case *types2.Array:
		return noCopyIn(u.Elem(), seen)
	}
	return nil
}

// sourceQualifier prints the types of the file that holds pos as the file
// refers to them: a package by the name the file's import gives it (the
// package's own name, or the alias), not by import path, and a type of the
// current package without one.
func (s *fnState) sourceQualifier(pos token.Pos) types2.Qualifier {
	names := map[*types2.Package]string{}
	file := s.l.fset.Position(pos).Filename
	for _, f := range s.pkg.Files {
		if s.l.fset.Position(f.Pos()).Filename != file {
			continue
		}
		for _, imp := range f.Imports {
			if pn := s.info.PkgNameOf(imp); pn != nil {
				names[pn.Imported()] = pn.Name()
			}
		}
	}
	return func(p *types2.Package) string {
		if p == s.pkg.Types {
			return ""
		}
		if n, ok := names[p]; ok {
			return n
		}
		return p.Name()
	}
}

// goCapturingLiteral reports a task statement that starts a function literal
// capturing the variables caps. The usual fix passes them as arguments; a
// variable that holds a Mutex, a WaitGroup or another synchronization type
// cannot be passed by value, since the copy is a different one, so for
// those the fix allocates the variable and passes the pointer.
func (s *fnState) goCapturingLiteral(lit *ast.FuncLit, caps []*types2.Var) {
	q := s.sourceQualifier(lit.Pos())
	str := func(T types2.Type) string { return types2.TypeString(T, q) }
	d := &diag.Diagnostic{Code: diag.GoCapturingLiteral}
	prefix := fmt.Sprintf("a function literal that captures variables cannot be started with a task statement: %s lives in the starting frame, which may be gone before the task runs (spec section 12.12); ", caps[0].Name())

	var params, args, news []string
	var shared []*types2.Var
	var sharedT []types2.Type
	for _, v := range caps {
		if bad := noCopyType(v.Type()); bad != nil {
			shared = append(shared, v)
			sharedT = append(sharedT, bad)
			params = append(params, v.Name()+" *"+str(v.Type()))
			news = append(news, fmt.Sprintf("var %s %s // at package level", v.Name(), str(v.Type())))
			args = append(args, "&"+v.Name())
			continue
		}
		params = append(params, v.Name()+" "+str(v.Type()))
		args = append(args, v.Name())
	}
	if len(shared) == 0 {
		it, them := "it as an argument", "them as arguments"
		if len(caps) == 1 {
			them = it
		}
		d.Msg = prefix + fmt.Sprintf("pass %s instead: task func(%s) {...}(%s)", them, strings.Join(params, ", "), strings.Join(args, ", "))
		s.l.report(lit.Pos(), d)
		return
	}

	v, bad := shared[0], sharedT[0]
	what := fmt.Sprintf("%s has type %s, which", v.Name(), str(v.Type()))
	if unwrap(v.Type()) != bad {
		what = fmt.Sprintf("%s has type %s, which holds a value of type %s and", v.Name(), str(v.Type()), str(bad))
	}
	sname := "s"
	for _, c := range caps {
		if c.Name() == "s" {
			sname = "sc"
		}
	}
	// The Scope's type as the file would write it: sync.Scope, or under
	// the name the file imports pluto/sync by.
	scope := "sync.Scope"
	if n, ok := unwrap(bad).(*types2.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "pluto/sync" {
		scope = q(n.Obj().Pkg()) + ".Scope"
	}
	d.Msg = prefix + fmt.Sprintf("%s must not be copied, so start the literal with a %s, which waits for it and lets it use %s: var %s %s; %s.Task(func() {...})", what, scope, v.Name(), sname, scope, sname)
	d.Text = diag.Lookup(diag.GoCapturingLiteral).Text + fmt.Sprintf("\n%s holds a value of type %s. Passing it as an argument would copy it, and a copy is a separate value: calling Done or Unlock on the copy never reaches the original, so a Wait or Lock on the original may never return. A sync.Scope declared in this function waits for the tasks it starts before its block ends, so they may use %s where it is.", v.Name(), str(bad), v.Name())
	d.Fix = &diag.Fix{
		Intro: "Start the literal with a " + scope,
		Code:  fmt.Sprintf("var %s %s\n%s.Task(func() {\n\t...\n})", sname, scope, sname),
	}
	d.More = []*diag.Fix{{
		Intro: "Or declare it at package level and pass a pointer",
		Code:  strings.Join(news, "\n") + fmt.Sprintf("\ntask func(%s) {\n\t...\n}(%s)", strings.Join(params, ", "), strings.Join(args, ", ")),
	}}
	s.l.report(lit.Pos(), d)
}

// A goArg is a value a task statement evaluates in the starting frame and
// copies into the new task's argument block.
type goArg struct {
	v   value
	off int64
}

// taskStmt lowers task f(args). The function value (or receiver) and the
// arguments are evaluated here and copied into a block that the runtime
// copies onto the new task's stack; a generated function, the thunk,
// runs first on the new task, loads them from there and makes the
// call. Variadic arguments are copied with their array, which would
// otherwise stay in this frame.
func (s *fnState) taskStmt(st *ast.TaskStmt) {
	call := st.Call
	fun := ast.Unparen(call.Fun)
	if lit, ok := fun.(*ast.FuncLit); ok {
		if caps := s.capturedVars(lit); len(caps) > 0 {
			s.goCapturingLiteral(lit, caps)
			return
		}
	}
	funTV := s.info.Types[call.Fun]
	if funTV.IsBuiltin() {
		s.l.errorf(st.Pos(), diag.GoBuiltin, "task with the built-in %s is not supported; start a function that calls it", identOf(fun).Name)
		return
	}
	if s.goFrameClosures(fun, call.Args) {
		return
	}
	// The function value and the arguments are evaluated now, calls first
	// (order.go).
	s.callsFirst(call)

	var vals []value                                      // receiver or func value first, then the arguments
	var emit func(ts *fnState, in []value, pos token.Pos) // makes the call in the thunk
	var sig *types2.Signature
	var checkFunc *ssa.Value // a func value to check at run time
	nlead := 0               // values before the arguments
	boxRecv := false         // the receiver is the owned or synced box itself (task p.Serve())

	if se, ok := fun.(*ast.SelectorExpr); ok {
		if sel := s.info.Selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
			fn := sel.Obj().(*types2.Func)
			recvT := s.typeOf(se.X)
			sig = s.subst(funTV.Type).(*types2.Signature)
			switch {
			case isInterface(recvT) && len(sel.Index()) == 1:
				iv := s.materialize(s.expr(se.X))
				vals = append(vals, iv)
				emit = func(ts *fnState, in []value, pos token.Pos) {
					ts.ifaceCall(in[0], recvT, fn, sig, in[1:], pos, fn.Name())
				}
				vals = append(vals, s.callArgs(call, sig, nil)...)
			default:
				m, path, ok := s.selectedMethod(se, sel)
				if !ok {
					return
				}
				var recv value
				var boxElem types2.Type
				if types2.GoBoxReceiver(sel) != nil && !isIfaceMethod(m) {
					// The task takes the box itself, which the type
					// checker has made sendable: an owned box moves into
					// it, a synced one is counted for it (language spec
					// section 4.7). The thunk calls the method on the
					// object, which the box's word points to, and drops
					// the box when the method returns.
					boxRecv = true
					recv = s.materialize(s.hold(s.expr(se.X)))
					boxElem = pointerElem(unwrap(recv.T))
				} else {
					recv = s.materialize(s.methodReceiver(se.X, m, path))
				}
				vals = append(vals, recv)
				if isIfaceMethod(m) {
					rT := recv.T
					emit = func(ts *fnState, in []value, pos token.Pos) {
						ts.ifaceCall(in[0], rT, m, sig, in[1:], pos, m.Name())
					}
					vals = append(vals, s.callArgs(call, sig, nil)...)
				} else {
					var targs []types2.Type
					var def *defaultsOf
					w := s.ringOf(m)
					if w == nil {
						targs = s.methodTypeArgs(m, se, recvT)
						def = &defaultsOf{m, targs}
					}
					sym, full := s.l.methodFunc(m, targs, w, se.Sel.Pos(), "started by a task statement")
					noReturn := s.l.isNoReturn(m)
					emit = func(ts *fnState, in []value, pos token.Pos) {
						r := in[0]
						if boxRecv {
							r = scalarValue(r.v, types2.NewPointer(boxElem))
						}
						ts.emitCall(callee{sym: sym, noReturn: noReturn}, full, &r, in[1:], pos)
					}
					vals = append(vals, s.callArgs(call, sig, def)...)
				}
			}
			nlead = 1
		}
	}
	if emit == nil {
		if id := staticFuncIdent(fun, s.info); id != nil {
			if fn, ok := s.info.Uses[id].(*types2.Func); ok {
				if s.l.errorsNewValue(fn, call.Pos(), "started by a task statement") {
					return
				}
				targs := s.instanceArgs(id)
				sig = s.subst(funTV.Type).(*types2.Signature)
				sym := s.l.funcSym(fn, targs)
				s.checkNotHandler(sym, call.Pos(), "started by a task statement")
				noReturn := s.l.isNoReturn(fn)
				emit = func(ts *fnState, in []value, pos token.Pos) {
					ts.emitCall(callee{sym: sym, noReturn: noReturn}, sig, nil, in, pos)
				}
				vals = s.callArgs(call, sig, &defaultsOf{fn, targs})
			}
		}
	}
	if emit == nil {
		// A func value: copy it, and check at run time that its closure
		// record is not in this frame.
		sig = s.typeOf(call.Fun).Underlying().(*types2.Signature)
		fT := s.typeOf(call.Fun)
		fv := s.scalar(call.Fun)
		if _, lit := fun.(*ast.FuncLit); !lit {
			checkFunc = fv
		}
		vals = append(vals, scalarValue(fv, fT))
		emit = func(ts *fnState, in []value, pos token.Pos) {
			f := in[0].v
			ts.emitCall(callee{code: ts.load(ssa.TPtr, f), ctx: f}, sig, nil, in[1:], pos)
		}
		vals = append(vals, s.callArgs(call, sig, nil)...)
		nlead = 1
	}
	var checkArgs []*ssa.Value // func value arguments, checked like the callee
	for i, v := range vals {
		vals[i] = s.holdProducedOwner(v)
	}
	for _, v := range vals[nlead:] {
		s.checkSendable(v.T, call.Pos(), "task statement argument of type")
		if _, isFunc := unwrap(v.T).Underlying().(*types2.Signature); isFunc && !v.addr {
			checkArgs = append(checkArgs, v.v)
		}
	}

	// Lay out the argument block.
	args := make([]goArg, len(vals))
	var off, align int64 = 0, 1
	for i, v := range vals {
		a := s.l.alignof(v.T)
		align = max(align, a)
		off = (off + a - 1) &^ (a - 1)
		args[i] = goArg{v: v, off: off}
		off += s.l.sizeof(v.T)
	}
	// The array behind variadic arguments packed by callArgs.
	var varArr *types2.Array
	var varOff int64
	if sig.Variadic() && !call.Ellipsis.IsValid() {
		if extra := len(s.argSources(call.Args)) - (sig.Params().Len() - 1); extra > 0 {
			et := s.subst(sig.Params().At(sig.Params().Len() - 1).Type()).Underlying().(*types2.Slice).Elem()
			varArr = types2.NewArray(et, int64(extra))
			a := s.l.alignof(et)
			align = max(align, a)
			varOff = (off + a - 1) &^ (a - 1)
			off = varOff + s.l.sizeof(varArr)
		}
	}
	size := max(off, 1)
	block := s.tempSize((size+7)&^7, max(align, 8), "goargs")
	// The new task owns a reference to every string in the block
	// (language spec section 18.8), and its thunk drops them when the call
	// returns.
	var provisional []value
	for _, a := range args {
		dst := s.offPtr(block, a.off)
		s.storeTo(dst, a.v, a.v.T)
		if s.l.counted(a.v.T) {
			s.takeRefAt(a.v, dst, a.v.T)
			slot := s.temp(a.v.T, "goargs.reference")
			s.storeTo(slot, s.loadFrom(dst, a.v.T), a.v.T)
			provisional = append(provisional, s.ownTemp(slot, a.v.T))
		}
	}
	if varArr != nil {
		last := args[len(args)-1].v
		p, _, _ := s.sliceParts(last)
		s.move(s.offPtr(block, varOff), p, s.l.sizeof(varArr))
		if s.l.counted(varArr.Elem()) {
			s.retainRange(s.offPtr(block, varOff), s.constInt(ssa.TI64, varArr.Len()), varArr.Elem())
			slot := s.temp(varArr, "goargs.variadic")
			s.move(slot, s.offPtr(block, varOff), s.l.sizeof(varArr))
			provisional = append(provisional, s.ownTemp(slot, varArr))
		}
	}

	// The thunk: func(p unsafe.Pointer), which loads the values and calls.
	s.taskStmts++
	name := fmt.Sprintf("%s.task%d", s.f.Name, s.taskStmts)
	thunk := s.l.out.Sym(name, ssa.SymFunc)
	unsafePtr := types2.Typ[types2.UnsafePointer]
	tsig := types2.NewSignatureType(nil, nil, nil, types2.NewTuple(types2.NewParam(token.NoPos, nil, "p", unsafePtr)), nil, false)
	pos := call.Pos()
	s.l.queue = append(s.l.queue, &funcJob{sym: thunk, pkg: s.pkg, sig: tsig, parent: s, build: func(ts *fnState) {
		in := ts.incomingArgs(tsig)
		p := in[0].v
		loaded := make([]value, len(args))
		for i, a := range args {
			loaded[i] = ts.materialize(ts.loadFrom(ts.offPtr(p, a.off), a.v.T))
		}
		if varArr != nil {
			last := loaded[len(loaded)-1]
			_, n, c := ts.sliceParts(last)
			loaded[len(loaded)-1] = ts.makeSlice(ts.offPtr(p, varOff), n, c, last.T)
		}
		if boxRecv {
			// The box moves from the argument block to a slot of this
			// frame, so that it is dropped (or released) when the thunk
			// returns, including when the method ends the task with
			// rt.ExitTask.
			slot := ts.frameSlot(args[0].v.T, "box")
			ts.store(ssa.TPtr, slot, loaded[0].v)
		}
		emit(ts, loaded, pos)
		for i, a := range args {
			if boxRecv && i == 0 {
				continue
			}
			if ts.l.counted(a.v.T) {
				ts.releaseAt(ts.offPtr(p, a.off), a.v.T)
			}
		}
		if varArr != nil && ts.l.counted(varArr.Elem()) {
			ts.releaseRange(ts.offPtr(p, varOff), ts.constInt(ssa.TI64, varArr.Len()), varArr.Elem())
		}
		ts.returnValues(nil)
	}})

	// A func value whose closure record is in this task's stack (a
	// literal that captures variables, a method value) would dangle in the
	// new task: checkGoFunc panics on one, for the function started
	// and for every func-typed argument.
	if checkFunc != nil {
		s.callRuntime("checkGoFunc", nil, []*ssa.Value{checkFunc})
	}
	for _, f := range checkArgs {
		s.callRuntime("checkGoFunc", nil, []*ssa.Value{f})
	}
	rec := s.symAddr(s.l.closureRecord(thunk))
	// The bound argument is the stack the call is proven to need; 0 until
	// the compiler computes it (roadmap item 18).
	handedOff := s.temp(types2.Typ[types2.Bool], "goargs.handoff")
	s.store(ssa.TBool, handedOff, s.constBool(false))
	s.callRuntime("spawnGo", nil, []*ssa.Value{rec, block, s.constInt(ssa.TI64, off), s.constInt(ssa.TI64, max(align, 1)), s.constInt(ssa.TI64, 0), handedOff})
	s.ifThen(s.load(ssa.TBool, handedOff), func() {
		for _, argument := range args {
			if temporary := argument.v.tmp; temporary != nil && s.l.needsDrop(argument.v.T) && !temporary.consumed {
				s.zero(temporary.addr, s.l.sizeof(temporary.T))
			}
		}
		for _, reference := range provisional {
			s.zero(reference.tmp.addr, s.l.sizeof(reference.T))
		}
	})
	spawn := s.l.rtFunc("spawnGo")
	if s.callMayGoexit(callee{sym: s.l.funcSym(spawn, nil)}, spawn.Type().(*types2.Signature)) {
		s.checkGoexit(call.Pos())
	}
	// Successful task creation copied these owners into its argument record.
	// Until that handoff, an exit still leaves their cleanup in this frame.
	for _, argument := range args {
		s.consumeProducedOwner(argument.v)
	}
	for _, reference := range provisional {
		reference.tmp.consumed = true
	}
}

// ----------------------------------------------------------------------------
// Channel operations

// makeChan lowers make(chan T[, n][, a]).
func (s *fnState) makeChan(e *ast.CallExpr, T types2.Type) value {
	et := s.subst(chanElem(T))
	s.checkSendable(et, e.Pos(), "channel element type")
	size := s.constInt(ssa.TI64, 0)
	rest := e.Args[1:]
	if len(rest) > 0 && (isIntegerType(unwrap(s.typeOf(rest[0]))) || s.info.Types[rest[0]].Value != nil) {
		size = s.indexValue(rest[0])
		rest = rest[1:]
	}
	esize := s.constInt(ssa.TI64, s.l.sizeof(et))
	ealign := s.constInt(ssa.TI64, s.l.alignof(et))
	var p *ssa.Value
	if len(rest) > 0 {
		allocT := s.l.rtFunc("makechanWith").Type().(*types2.Signature).Params().At(0).Type()
		a := s.exprAs(rest[0], allocT)
		parts := s.leafValues(a, []leaf{{0, ssa.TPtr}, {8, ssa.TPtr}})
		p = s.callRuntime("makechanWith", []*ssa.Type{ssa.TPtr}, parts, []*ssa.Value{esize, ealign, size})[0]
	} else {
		p = s.callRuntime("makechan", []*ssa.Type{ssa.TPtr}, []*ssa.Value{esize, ealign, size})[0]
	}
	// The new channel's one reference is the statement's until a store
	// takes it.
	return s.wordTemp(p, T, "chan")
}

// chanOperand lowers the channel operand e of a send or receive, held if
// any of the later expressions, evaluated before the operation, calls a
// function that could release it.
func (s *fnState) chanOperand(e ast.Expr, later ...ast.Expr) *ssa.Value {
	return s.wordOf(s.holdAcross(s.expr(e), later...))
}

// sendStmt lowers ch <- v.
func (s *fnState) sendStmt(st *ast.SendStmt) {
	ct := s.typeOf(st.Chan)
	et := s.subst(chanElem(ct))
	c := s.chanOperand(st.Chan, st.Value)
	v := s.exprAs(st.Value, et)
	tmp := s.temp(et, "send")
	s.storeTo(tmp, v, et)
	if s.l.counted(et) {
		// The channel, and then the receiver, owns a reference to the
		// strings sent.
		s.takeRefAt(v, tmp, et)
	}
	s.callRuntime("chansend1", nil, []*ssa.Value{c, tmp})
}

// recv lowers <-ch, and v, ok := <-ch when commaOK is set.
func (s *fnState) recv(e *ast.UnaryExpr, commaOK bool) (value, *ssa.Value) {
	et := s.subst(chanElem(s.typeOf(e.X)))
	c := s.scalar(e.X)
	tmp := s.temp(et, "recv")
	s.zero(tmp, s.l.sizeof(et))
	var ok *ssa.Value
	if commaOK {
		ok = s.callRuntime("chanrecv2", []*ssa.Type{ssa.TBool}, []*ssa.Value{c, tmp})[0]
	} else {
		s.callRuntime("chanrecv1", nil, []*ssa.Value{c, tmp})
	}
	if s.l.counted(et) {
		// The value received comes with the sender's references: a
		// temporary of this statement unless a store takes it over.
		return s.ownTemp(tmp, et), ok
	}
	return s.loadFrom(tmp, et), ok
}

// isRecv reports whether e is a receive expression <-ch.
func isRecv(e ast.Expr) (*ast.UnaryExpr, bool) {
	u, ok := ast.Unparen(e).(*ast.UnaryExpr)
	return u, ok && u.Op == token.ARROW
}

// rangeChan lowers for v := range ch: receive until the channel is closed.
func (s *fnState) rangeChan(st *ast.RangeStmt, label *types2.Label) {
	ct := s.typeOf(st.X)
	et := s.subst(chanElem(ct))
	// The loop holds a reference of its own, whatever the body does to the
	// channel's variable.
	c := s.holdOwned(s.materialize(s.expr(st.X)))
	cslot := s.temp(ct, "rangechan")
	s.store(ssa.TPtr, cslot, s.wordOf(c))
	keyLV := s.rangeVar(st, st.Key)
	tmp := s.temp(et, "recv")

	head := s.newBlock(ssa.BlockInvalid)
	body := s.newBlock(ssa.BlockInvalid)
	done := s.newBlock(ssa.BlockInvalid)
	s.endPlain(head)
	s.startBlock(head)
	s.zero(tmp, s.l.sizeof(et))
	ok := s.callRuntime("chanrecv2", []*ssa.Type{ssa.TBool}, []*ssa.Value{s.load(ssa.TPtr, cslot), tmp})[0]
	s.endIf(ok, body, done)
	s.seal(body)
	s.startBlock(body)
	counted := s.l.counted(et)
	if keyLV != nil {
		v := s.materialize(s.loadFrom(tmp, et))
		if counted {
			v.cnt = cntStable
		}
		s.assign(*keyLV, s.assignConvert(v, unwrap(keyLV.T), st), false)
	}
	if counted {
		// The variable took its own reference; drop the one received.
		s.releaseAt(tmp, et)
	}
	s.targets = append(s.targets, &target{label: label, breakTo: done, contTo: head, depth: len(s.cleanups)})
	s.block(st.Body, st.Body.List)
	s.targets = s.targets[:len(s.targets)-1]
	s.endPlain(head)
	s.seal(head)
	s.seal(done)
	s.startBlock(done)
}

// ----------------------------------------------------------------------------
// select

// A selCase is one communication case of a select statement.
type selCase struct {
	clause *ast.CommClause
	recv   bool
	elem   types2.Type
	tmp    *ssa.Value // the value to send, or where the received value lands
	lhs    []ast.Expr // v, ok of a receive with an assignment
	define bool       // the assignment is :=
}

// selectStmt lowers a select statement: the channel operands and send
// values are evaluated in source order, the cases are written into an
// array in this frame, and one runtime call (selectgo, or selectnb with a
// default case) picks the case to run.
func (s *fnState) selectStmt(st *ast.SelectStmt, label *types2.Label) {
	var cases []*selCase
	var deflt *ast.CommClause
	var chans []*ssa.Value
	// The operands in the order they are evaluated: a channel is held if
	// one evaluated after it calls a function (chanOperand).
	var operands []ast.Expr
	for _, cl := range st.Body.List {
		switch comm := cl.(*ast.CommClause).Comm.(type) {
		case *ast.SendStmt:
			operands = append(operands, comm.Chan, comm.Value)
		case *ast.ExprStmt:
			u, _ := isRecv(comm.X)
			operands = append(operands, u.X)
		case *ast.AssignStmt:
			u, _ := isRecv(comm.Rhs[0])
			operands = append(operands, u.X)
		}
	}
	nextOp := 0
	chanOperand := func(e ast.Expr) *ssa.Value {
		nextOp++
		return s.chanOperand(e, operands[nextOp:]...)
	}
	for _, cl := range st.Body.List {
		cc := cl.(*ast.CommClause)
		switch comm := cc.Comm.(type) {
		case nil:
			deflt = cc
			continue
		case *ast.SendStmt:
			et := s.subst(chanElem(s.typeOf(comm.Chan)))
			c := chanOperand(comm.Chan)
			nextOp++ // the value
			tmp := s.temp(et, "selsend")
			v := s.exprAs(comm.Value, et)
			s.storeTo(tmp, v, et)
			if s.l.counted(et) {
				// A reference for the receiver, dropped again if another
				// case proceeds (releaseCases).
				s.takeRefAt(v, tmp, et)
			}
			cases = append(cases, &selCase{clause: cc, elem: et, tmp: tmp})
			chans = append(chans, c)
		case *ast.ExprStmt:
			u, _ := isRecv(comm.X)
			et := s.subst(chanElem(s.typeOf(u.X)))
			chans = append(chans, chanOperand(u.X))
			tmp := s.temp(et, "selrecv")
			cases = append(cases, &selCase{clause: cc, recv: true, elem: et, tmp: tmp})
		case *ast.AssignStmt:
			u, _ := isRecv(comm.Rhs[0])
			et := s.subst(chanElem(s.typeOf(u.X)))
			chans = append(chans, chanOperand(u.X))
			tmp := s.temp(et, "selrecv")
			cases = append(cases, &selCase{clause: cc, recv: true, elem: et, tmp: tmp, lhs: comm.Lhs, define: comm.Tok == token.DEFINE})
		default:
			s.l.internalf(cc.Pos(), "unexpected select case %T", comm)
			return
		}
	}
	n := len(cases)
	scaseT := s.l.rtType("scase")
	scaseSize := s.l.sizeof(scaseT)
	st0 := scaseT.Underlying().(*types2.Struct)
	offs := s.l.sizes.Offsetsof([]*types2.Var{st0.Field(0), st0.Field(1), st0.Field(2), st0.Field(3)})
	var arr, order *ssa.Value
	if n > 0 {
		arr = s.tempSize(scaseSize*int64(n), 8, "selcases")
		s.zero(arr, scaseSize*int64(n))
		order = s.tempSize(int64(4*n), 2, "selorder")
		for i, c := range cases {
			// c, elem, kind: caseRecv (1) or caseSend (2). A received
			// value is stored even if it is not used.
			base := s.offPtr(arr, int64(i)*scaseSize)
			kind := int64(2)
			if c.recv {
				kind = 1
			}
			s.store(ssa.TPtr, s.offPtr(base, offs[0]), chans[i])
			s.store(ssa.TPtr, s.offPtr(base, offs[1]), c.tmp)
			s.store(ssa.TI64, s.offPtr(base, offs[2]), s.constInt(ssa.TI64, kind))
		}
	} else {
		arr = s.constInt(ssa.TPtr, 0)
		order = s.constInt(ssa.TPtr, 0)
	}
	fn := "selectgo"
	if deflt != nil {
		fn = "selectnb"
	}
	res := s.callRuntime(fn, []*ssa.Type{ssa.TI64, ssa.TBool}, []*ssa.Value{arr, order, s.constInt(ssa.TI64, int64(n))})
	idx := s.temp(types2.Typ[types2.Int], "selidx")
	s.store(ssa.TI64, idx, res[0])
	okSlot := s.temp(types2.Typ[types2.Bool], "selok")
	s.store(ssa.TBool, okSlot, res[1])

	done := s.newBlock(ssa.BlockInvalid)
	bodies := make([]*ssa.Block, n)
	for i := range cases {
		bodies[i] = s.newBlock(ssa.BlockInvalid)
	}
	defBody := done
	if deflt != nil {
		defBody = s.newBlock(ssa.BlockInvalid)
	}
	for i := range cases {
		next := s.newBlock(ssa.BlockInvalid)
		s.endIf(s.newValue(ssa.OpEq, ssa.TBool, s.load(ssa.TI64, idx), s.constInt(ssa.TI64, int64(i))), bodies[i], next)
		s.seal(next)
		s.startBlock(next)
	}
	s.endPlain(defBody)

	s.targets = append(s.targets, &target{label: label, breakTo: done, isSwitch: true, depth: len(s.cleanups)})
	for i, c := range cases {
		s.seal(bodies[i])
		s.startBlock(bodies[i])
		clause := s.pushCleanup(c.clause, true)
		s.releaseCases(cases, i)
		if len(c.lhs) > 0 {
			s.selectAssign(c, s.load(ssa.TBool, okSlot))
		}
		if c.recv && s.l.counted(c.elem) {
			// The variables took their own references; drop the one
			// received.
			s.releaseAt(c.tmp, c.elem)
		}
		s.stmtList(c.clause.Body)
		s.popCleanup(clause)
		s.endPlain(done)
	}
	if deflt != nil {
		s.seal(defBody)
		s.startBlock(defBody)
		clause := s.pushCleanup(deflt, true)
		s.releaseCases(cases, -1)
		s.stmtList(deflt.Body)
		s.popCleanup(clause)
		s.endPlain(done)
	}
	s.targets = s.targets[:len(s.targets)-1]
	s.seal(done)
	s.startBlock(done)
}

// releaseCases drops the references held for the values of the send cases
// other than cases[chosen], which did not proceed.
func (s *fnState) releaseCases(cases []*selCase, chosen int) {
	for i, c := range cases {
		if i != chosen && !c.recv && s.l.counted(c.elem) {
			s.releaseAt(c.tmp, c.elem)
		}
	}
}

// selectAssign stores the value and ok of the receive case c that
// proceeded into the variables of its assignment.
func (s *fnState) selectAssign(c *selCase, ok *ssa.Value) {
	v := s.materialize(s.loadFrom(c.tmp, c.elem))
	if s.l.counted(c.elem) {
		v.cnt = cntStable // released by the caller after the stores
	}
	vals := []value{v, scalarValue(ok, types2.Typ[types2.Bool])}
	for i, lhs := range c.lhs {
		if id, isID := lhs.(*ast.Ident); isID && id.Name == "_" {
			continue
		}
		if c.define {
			if v, isVar := s.info.Defs[lhs.(*ast.Ident)].(*types2.Var); isVar {
				T := s.subst(v.Type())
				addr := s.declareVar(v)
				s.assign(lvalue{ptr: addr, T: T}, s.assignConvert(vals[i], T, c.clause), true)
			}
			continue
		}
		lv := s.lv(lhs)
		T := unwrap(lv.T)
		if lv.bits != nil {
			T = lv.bits.T
		}
		s.assign(lv, s.assignConvert(vals[i], T, c.clause), false)
	}
}
