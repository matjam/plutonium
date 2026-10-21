package eir

import (
	"github.com/matjam/plutonium/internal/escape2/contract"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Calls. Every call internal/lower makes for a call expression is a call
// here, by the same choices (lower/call.go callResults): conversions and
// built-ins first, scoped tasks, union variants, method calls (box methods,
// interface methods, type-parameter methods, promoted interface methods,
// declared methods), method expressions, then declared functions, which
// the lowerer may expand inline (the contract table), and calls through
// func values.

// emitCall emits the call c at pos and returns its result registers (0 for
// a result without pointers).
func (fb *fnb) emitCall(c *Call, pos token.Pos) []Reg {
	if c.Sig != nil {
		res := c.Sig.Results()
		c.Results = make([]Reg, res.Len())
		for i := 0; i < res.Len(); i++ {
			if hasPointers(res.At(i).Type()) {
				c.Results[i] = fb.newReg()
			}
		}
	}
	c.Site = fb.newSite()
	fb.emit(Instr{Op: OpCall, Call: c, Pos: pos})
	switch c.Implicit {
	case ImplNone, ImplIndex, ImplSetIndex, ImplIndexOk, ImplOpAssign, ImplLen, ImplAll:
	default:
		// The lowerer does not end the block after these.
		return c.Results
	}
	if c.Kind == CallStatic && fb.b.neverReturns(c.Callee) {
		// The code after the call is unreachable: the lowerer removes it.
		fb.cur = nil
		return c.Results
	}
	if fb.callMayExit(c) {
		fb.leaveForExit(pos)
	}
	return c.Results
}

// neverReturns reports whether a call of function id never returns.
func (b *builder) neverReturns(id FuncID) bool {
	if id == NoFunc {
		return false
	}
	fn := b.p.Funcs[id].Key.Obj
	if fn == nil {
		return false
	}
	return b.isNoReturn(fn) || fn == b.panicFn
}

// resultTypes returns the types of a signature's results.
func resultTypes(sig *types2.Signature) []types2.Type {
	if sig == nil {
		return nil
	}
	out := make([]types2.Type, sig.Results().Len())
	for i := range out {
		out[i] = sig.Results().At(i).Type()
	}
	return out
}

// callResults builds a call expression and returns its results and their
// types.
func (fb *fnb) callResults(e *ast.CallExpr) ([]Reg, []types2.Type) {
	saved := fb.pos
	fb.pos = e.Pos()
	defer func() { fb.pos = saved }()
	funTV := fb.b.types[e.Fun]
	if funTV.IsType() {
		return []Reg{fb.conversion(e)}, []types2.Type{fb.typeOf(e)}
	}
	if funTV.IsBuiltin() {
		return fb.builtin(e)
	}
	if t := fb.b.scoped.Tasks[e]; t != nil && t.Lit != nil {
		fb.scopedTask(e, t)
		return nil, nil
	}
	fun := ast.Unparen(e.Fun)
	if se, ok := fun.(*ast.SelectorExpr); ok {
		if v, ok := fb.b.uses[se.Sel].(*types2.Variant); ok {
			// U.V(payload): a union value holding the payload.
			_ = v
			r := fb.exprAs(e.Args[0], v.Payload())
			return []Reg{r}, []types2.Type{fb.typeOf(e)}
		}
		if sel := fb.b.selections[se]; sel != nil && sel.Kind() == types2.MethodVal {
			if res, types, done := fb.specialMethodCall(e, se, sel); done {
				return res, types
			}
		}
	}
	c, args, _ := fb.calleeAndArgs(e, false)
	if c == nil {
		return nil, nil
	}
	c.Args = args
	return fb.emitCall(c, e.Pos()), resultTypes(c.Sig)
}

// specialMethodCall builds the method calls that are not calls of a method:
// x.Drop() that ends an owner variable's life, and the built-in box
// methods.
func (fb *fnb) specialMethodCall(e *ast.CallExpr, se *ast.SelectorExpr, sel *types2.Selection) ([]Reg, []types2.Type, bool) {
	fn := sel.Obj().(*types2.Func)
	if fn.Name() == "Drop" && len(e.Args) == 0 && (fb.isMove(se.X) || isImplicitDrop(fn) && isIdent(se.X)) {
		lv := fb.lvalue(se.X)
		T := unwrap(lv.T)
		if lv.kind == lvMem {
			fb.dropAt(lv.addr, T, e.Pos())
		}
		return nil, nil, true
	}
	if w := types2.BoxMethodOf(fn); w != nil && fn.Name() != "Drop" {
		x := fb.expr(se.X)
		types := resultTypes(fb.b.types[e.Fun].Type.(*types2.Signature))
		if fn.Name() == "Clone" {
			// A new box holding a copy of the object.
			nb := fb.heap(HeapAlloc)
			fb.store(nb, fb.load(x, w.Elem()), w.Elem())
			return []Reg{nb}, types, true
		}
		// Upgrade: a strong reference to the same object.
		return []Reg{x, 0}, types, true
	}
	return nil, nil, false
}

// isMove reports whether e moves an owner out of its variable.
func (fb *fnb) isMove(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && fb.b.moves.Moves[id]
}

func isIdent(e ast.Expr) bool {
	_, ok := ast.Unparen(e).(*ast.Ident)
	return ok
}

// calleeAndArgs returns the call a call expression makes, without its
// arguments, and the arguments (the receiver first for a method), built
// now. It is nil for a call that makes no call (an error was reported).
func (fb *fnb) calleeAndArgs(e *ast.CallExpr, deferred bool) (*Call, []Reg, []types2.Type) {
	fun := ast.Unparen(e.Fun)
	if ix, ok := fun.(*ast.IndexExpr); ok && signatureOf(fb.typeOf(ix.X)) != nil {
		fun = ix.X
	} else if ix, ok := fun.(*ast.IndexListExpr); ok {
		fun = ix.X
	}
	if se, ok := fun.(*ast.SelectorExpr); ok {
		if sel := fb.b.selections[se]; sel != nil {
			switch sel.Kind() {
			case types2.MethodVal:
				if c, args, types, ok := fb.methodCallee(e, se, sel, deferred); ok {
					return c, args, types
				}
			case types2.MethodExpr:
				sig := fb.b.types[e.Fun].Type.(*types2.Signature)
				id := fb.methodExprFunc(se, sel)
				if id == NoFunc {
					return nil, nil, nil
				}
				args, types := fb.args(e, sig, nil)
				return &Call{Kind: CallStatic, Callee: id, Sig: sig}, args, types
			}
		}
	}
	if id := staticFuncIdent(fb.b, fun); id != nil {
		if fn, ok := fb.b.uses[id].(*types2.Func); ok {
			fb.noteInstance(id)
			sig := fb.b.types[e.Fun].Type.(*types2.Signature)
			if isGateFunc(fn) {
				return fb.gateCall(e, fn)
			}
			if name, ok := fb.b.inlineContract(fn, e); ok {
				args, types := fb.args(e, sig, nil)
				return &Call{Kind: CallContract, Contract: name, Sig: sig}, args, types
			}
			if fb.b.isPanicHook(fn) {
				// The runtime's call of the program's panic handler.
				args, types := fb.args(e, sig, nil)
				h := fb.b.panicHandler()
				if h == nil {
					return nil, nil, nil
				}
				return &Call{Kind: CallStatic, Callee: fb.b.funcOf(h), Sig: sig}, args, types
			}
			fid := fb.b.funcOf(fn)
			if fid == NoFunc {
				fb.b.errorf(e.Pos(), "function %s has no source", fn.Name())
				return nil, nil, nil
			}
			args, types := fb.args(e, sig, fn)
			return &Call{Kind: CallStatic, Callee: fid, Sig: sig}, args, types
		}
	}
	// A call through a func value; its core type is a signature, a type
	// parameter's included.
	fv := fb.expr(e.Fun)
	sig := signatureOf(fb.typeOf(e.Fun))
	if sig == nil {
		fb.b.errorf(e.Pos(), "call of %s, which is not a function", fb.typeOf(e.Fun))
		return nil, nil, nil
	}
	args, types := fb.args(e, sig, nil)
	return &Call{Kind: CallFunc, Target: fv, Sig: sig}, args, types
}

// gateCall builds x86.InterruptGate(h, ...) and x86.TrapGate(h, ...): a
// call of x86.gateAt with the handler's address (lower/interrupt.go).
func (fb *fnb) gateCall(e *ast.CallExpr, gate *types2.Func) (*Call, []Reg, []types2.Type) {
	at, _ := gate.Pkg().Scope().Lookup("gateAt").(*types2.Func)
	if at == nil || len(e.Args) != 3 {
		for _, a := range e.Args {
			fb.expr(a)
		}
		return nil, nil, nil
	}
	sig := at.Type().(*types2.Signature)
	var h Reg
	if id := identOf(ast.Unparen(e.Args[0])); id != nil {
		if fn, ok := fb.b.uses[id].(*types2.Func); ok {
			h = fb.funcValue(fn, id)
		}
	}
	a1 := fb.exprAs(e.Args[1], sig.Params().At(1).Type())
	a2 := fb.exprAs(e.Args[2], sig.Params().At(2).Type())
	types := []types2.Type{sig.Params().At(0).Type(), sig.Params().At(1).Type(), sig.Params().At(2).Type(), sig.Params().At(3).Type()}
	return &Call{Kind: CallStatic, Callee: fb.b.funcOf(at), Sig: sig}, []Reg{h, a1, a2, 0}, types
}

// methodCallee returns the call of method x.m(args) and its receiver and
// arguments. ok is false for a method that is no call (an implicit Drop
// on an owner with none).
func (fb *fnb) methodCallee(e *ast.CallExpr, se *ast.SelectorExpr, sel *types2.Selection, deferred bool) (*Call, []Reg, []types2.Type, bool) {
	fn := sel.Obj().(*types2.Func)
	recvT := fb.typeOf(se.X)
	sig := fb.b.types[e.Fun].Type.(*types2.Signature)
	if isInterface(recvT) && len(sel.Index()) == 1 {
		iv := fb.expr(se.X)
		args, types := fb.args(e, sig, nil)
		return &Call{Kind: CallIface, Method: fn, Iface: recvT, Target: iv, Sig: sig}, args, types, true
	}
	if isImplicitDrop(fn) {
		// x.Drop() of an owner whose type declares none, not on the
		// variable itself: there is no code to call.
		return nil, nil, nil, true
	}
	if isTypeParam(recvT) || isTypeParam(pointerElem(recvT)) {
		// A method of a type parameter's constraint: every method of that
		// name over the instantiations (the receiver is the operand's
		// address when it has one, as a pointer method takes it).
		// The instance's method may take the operand's address (a pointer
		// method of a type argument that is not a pointer) or its value:
		// Args[0] is the value, RecvAddr the address, and the solver binds
		// the one each candidate takes.
		var recv, addr Reg
		tp := recvT
		if el := pointerElem(recvT); el != nil {
			tp = el
			addr = fb.expr(se.X)
			recv = fb.load(addr, el)
		} else if a, ok := fb.addrIfPlace(se.X); ok {
			addr = a
			recv = fb.load(a, recvT)
		} else {
			recv = fb.expr(se.X)
		}
		args, types := fb.args(e, sig, nil)
		return &Call{Kind: CallTypeParam, Method: fn, Iface: tp, Sig: sig, RecvAddr: addr}, append([]Reg{recv}, args...), append([]types2.Type{tp}, types...), true
	}
	if fb.isRingMethod(fn) {
		// The methods of ring[T, N] have pointer receivers: the contract
		// takes the ring's address (its place, as the lowerer does).
		x := fb.methodReceiver(se.X, fn, sel.Index())
		args, types := fb.args(e, sig, nil)
		return &Call{Kind: CallContract, Contract: contract.RingMethod, Sig: sig}, append([]Reg{x}, args...), append([]types2.Type{fn.Type().(*types2.Signature).Recv().Type()}, types...), true
	}
	recv := fb.methodReceiver(se.X, fn, sel.Index())
	if isIfaceMethod(fn) {
		// Promoted from an embedded interface field.
		args, types := fb.args(e, sig, nil)
		I := fb.recvFieldType(recvT, sel.Index())
		return &Call{Kind: CallIface, Method: fn, Iface: I, Target: recv, Sig: sig}, args, types, true
	}
	fid := fb.b.funcOf(fn)
	if fid == NoFunc {
		fb.b.errorf(e.Pos(), "method %s has no source", fn.Name())
		return nil, nil, nil, false
	}
	fb.noteMethodInstance(fn, se)
	full := fn.Type().(*types2.Signature)
	args, types := fb.args(e, sig, fn)
	all, allTypes := append([]Reg{recv}, args...), append([]types2.Type{full.Recv().Type()}, types...)
	if q := methodName(fn); contract.IsTrusted(q) {
		c := &Call{Kind: CallContract, Contract: q, Sig: full}
		if ent, _ := contract.Lookup(q); ent.Construct == contract.TaskStart {
			// A function that starts a task with its arguments (section
			// 12.12): what it keeps, it keeps in the task.
			c.Implicit = ImplTask
		}
		return c, all, allTypes, true
	}
	return &Call{Kind: CallStatic, Callee: fid, Sig: full}, all, allTypes, true
}

// methodName names a method of a named type as the contract table does:
// "path.Type.Method".
func methodName(fn *types2.Func) string {
	recv := fn.Type().(*types2.Signature).Recv()
	if recv == nil || fn.Pkg() == nil {
		return ""
	}
	T := recv.Type()
	if p := pointerElem(T); p != nil {
		T = p
	}
	n, ok := types2.Unalias(T).(*types2.Named)
	if !ok {
		return ""
	}
	return fn.Pkg().Path() + "." + n.Obj().Name() + "." + fn.Name()
}

// recvFieldType returns the type of the embedded field that holds a
// promoted method's receiver.
func (fb *fnb) recvFieldType(T types2.Type, path []int) types2.Type {
	cur := T
	for _, i := range path[:len(path)-1] {
		if el := pointerElem(cur); el != nil {
			cur = el
		}
		st, ok := coreType(unwrap(cur)).(*types2.Struct)
		if !ok {
			return cur
		}
		cur = st.Field(i).Type()
	}
	return cur
}

// isIfaceMethod reports whether fn is declared by an interface.
func isIfaceMethod(fn *types2.Func) bool {
	recv := fn.Type().(*types2.Signature).Recv()
	return recv != nil && isInterface(recv.Type())
}

// isRingMethod reports whether fn is a built-in method of ring[T, N],
// which the lowerer expands inline.
func (fb *fnb) isRingMethod(fn *types2.Func) bool {
	if fn.Pkg() != nil {
		return false
	}
	recv := fn.Type().(*types2.Signature).Recv()
	if recv == nil {
		return false
	}
	T := recv.Type()
	if p := pointerElem(T); p != nil {
		T = p
	}
	w, ok := unwrap(T).Underlying().(*types2.Wrapper)
	return ok && w.Kind() == types2.WrapRing
}

// methodReceiver builds the receiver of a method call or method value on
// x, following the embedded fields of path, as the method's receiver type
// requires: the receiver's address for a pointer method, its value
// otherwise.
func (fb *fnb) methodReceiver(x ast.Expr, fn *types2.Func, path []int) Reg {
	_, wantPtr := fn.Type().(*types2.Signature).Recv().Type().(*types2.Pointer)
	xt := fb.typeOf(x)
	if el := pointerElem(xt); el != nil {
		return fb.receiverAt(fb.pointee(x), el, path, wantPtr, fn)
	}
	if len(path) == 1 && !wantPtr {
		return fb.expr(x)
	}
	if fb.b.moves.Temps[x] {
		// The method works on the statement's temporary itself.
		r := fb.expr(x)
		o := fb.lastTempObj()
		if o == NoObj {
			o = fb.newObj(Object{Kind: ObjTemp, Type: xt, Name: "recv"})
			fb.store(fb.addr(o), r, xt)
		}
		return fb.receiverAt(fb.addr(o), xt, path, wantPtr, fn)
	}
	if a, ok := fb.addrIfPlace(x); ok {
		return fb.receiverAt(a, unwrap(xt), path, wantPtr, fn)
	}
	v := fb.expr(x)
	o := fb.newObj(Object{Kind: ObjTemp, Type: xt, Name: "recv"})
	fb.store(fb.addr(o), v, xt)
	return fb.receiverAt(fb.addr(o), xt, path, wantPtr, fn)
}

// lastTempObj returns the statement temporary just made, or NoObj.
func (fb *fnb) lastTempObj() ObjID {
	if n := len(fb.scopes); n > 0 {
		sc := fb.scopes[n-1]
		if m := len(sc.slots); m > 0 && sc.slots[m-1].v == nil {
			return sc.slots[m-1].obj
		}
	}
	return NoObj
}

// receiverAt follows path from the place addr, of type T, to the receiver
// of the method at its end.
func (fb *fnb) receiverAt(addr Reg, T types2.Type, path []int, wantPtr bool, fn *types2.Func) Reg {
	if len(path) > 1 {
		addr, T = fb.fieldPath(addr, T, path[:len(path)-1])
		if el := pointerElem(T); el != nil {
			addr = fb.load(addr, T)
			T = el
		}
	}
	if wantPtr {
		return addr
	}
	return fb.load(addr, T)
}

// args builds a call's arguments for signature sig: variadic arguments
// packed into an array in this frame, and the default values of the
// parameters a call of the declared function fn omits.
func (fb *fnb) args(e *ast.CallExpr, sig *types2.Signature, fn *types2.Func) ([]Reg, []types2.Type) {
	params := sig.Params()
	n := params.Len()
	type src struct {
		e ast.Expr
		r Reg
		T types2.Type
	}
	var srcs []src
	if len(e.Args) == 1 {
		if call, ok := ast.Unparen(e.Args[0]).(*ast.CallExpr); ok {
			if tup, ok := fb.b.types[call].Type.(*types2.Tuple); ok && tup.Len() > 1 {
				res, types := fb.callResults(call)
				for i, r := range res {
					srcs = append(srcs, src{r: r, T: types[i]})
				}
			}
		}
	}
	if srcs == nil {
		for _, a := range e.Args {
			srcs = append(srcs, src{e: a})
		}
	}
	eval := func(s src, T types2.Type) Reg {
		if s.e != nil {
			return fb.exprAs(s.e, T)
		}
		return fb.convertReg(s.r, s.T, T, e.Pos())
	}
	var out []Reg
	var types []types2.Type
	fixed := n
	if sig.Variadic() && !e.Ellipsis.IsValid() {
		fixed = n - 1
	}
	for i := 0; i < fixed && i < len(srcs); i++ {
		T := params.At(i).Type()
		out = append(out, eval(srcs[i], T))
		types = append(types, T)
	}
	if !sig.Variadic() && len(srcs) < n {
		for i := len(srcs); i < n; i++ {
			T := params.At(i).Type()
			out = append(out, fb.defaultArg(fn, i, n, T, e))
			types = append(types, T)
		}
	}
	if sig.Variadic() && !e.Ellipsis.IsValid() {
		sliceT := params.At(n - 1).Type()
		et := sliceElem(sliceT)
		extra := srcs[min(fixed, len(srcs)):]
		if len(extra) == 0 {
			out = append(out, 0)
		} else {
			arrT := types2.NewArray(et, int64(len(extra)))
			var base Reg
			if fb.f.Key.Init != nil {
				base = fb.heap(HeapStatic)
			} else {
				o := fb.root.newObj(Object{Kind: ObjVarargs, Type: arrT, Name: "variadic arguments", Pos: e.Pos()})
				base = fb.objAddr(fb.root, o)
			}
			for _, a := range extra {
				fb.store(fb.elem(base), eval(a, et), et)
			}
			out = append(out, base)
		}
		types = append(types, sliceT)
	}
	return out, types
}

// defaultArg builds the default value of parameter i (of n) of fn, which a
// call omits: the default's expression, built in the caller (section 11.6).
func (fb *fnb) defaultArg(fn *types2.Func, i, n int, T types2.Type, e *ast.CallExpr) Reg {
	if fn == nil {
		fb.b.errorf(e.Pos(), "call has %d arguments, signature has %d parameters", len(e.Args), n)
		return 0
	}
	defs := fn.Origin().Defaults()
	k := i - (n - len(defs))
	if k < 0 || k >= len(defs) {
		fb.b.errorf(e.Pos(), "no argument for parameter %d of %s", i+1, fn.Name())
		return 0
	}
	saved := fb.pos
	fb.pos = e.Pos()
	defer func() { fb.pos = saved }()
	return fb.exprAs(defs[k], T)
}

// staticFuncIdent returns the identifier of a declared function that fun
// names (f, pkg.F), or nil.
func staticFuncIdent(b *builder, fun ast.Expr) *ast.Ident {
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		if _, ok := b.uses[f].(*types2.Func); ok {
			return f
		}
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			if _, isPkg := b.uses[id].(*types2.PkgName); isPkg {
				if _, ok := b.uses[f.Sel].(*types2.Func); ok {
					return f.Sel
				}
			}
		}
	}
	return nil
}

// staticCallee returns the declared function x names, with a body, which
// a range over it calls directly, or nil.
func (fb *fnb) staticCallee(x ast.Expr) *types2.Func {
	id := staticFuncIdent(fb.b, x)
	if id == nil {
		return nil
	}
	fn, ok := fb.b.uses[id].(*types2.Func)
	if !ok {
		return nil
	}
	d := fb.b.decls[fn.Origin()]
	if d == nil || d.decl.Body == nil || types2.IsErrorsNew(fn) || isExternalDecl(d.decl) {
		return nil
	}
	return fn
}

// noteInstance records the instantiation an identifier makes, if any.
func (fb *fnb) noteInstance(e ast.Expr) {
	id := identOf(e)
	if id == nil {
		return
	}
	inst, ok := fb.b.instances[id]
	if !ok || inst.TypeArgs == nil || inst.TypeArgs.Len() == 0 {
		return
	}
	obj := fb.b.uses[id]
	if obj == nil {
		return
	}
	args := make([]types2.Type, inst.TypeArgs.Len())
	for i := range args {
		args[i] = inst.TypeArgs.At(i)
	}
	if fn, ok := obj.(*types2.Func); ok {
		obj = fn.Origin()
	}
	fb.f.Facts.Instances = append(fb.f.Facts.Instances, Instance{Obj: obj, Args: args})
}

// noteMethodInstance records the instantiation a method call makes: the
// receiver type's arguments, then the method's own.
func (fb *fnb) noteMethodInstance(fn *types2.Func, se *ast.SelectorExpr) {
	var args []types2.Type
	recv := fn.Type().(*types2.Signature).Recv().Type()
	if p := pointerElem(recv); p != nil {
		recv = p
	}
	if n, ok := types2.Unalias(recv).(*types2.Named); ok && n.TypeArgs() != nil {
		for i := 0; i < n.TypeArgs().Len(); i++ {
			args = append(args, n.TypeArgs().At(i))
		}
	}
	if inst, ok := fb.b.instances[se.Sel]; ok && inst.TypeArgs != nil {
		for i := 0; i < inst.TypeArgs.Len(); i++ {
			args = append(args, inst.TypeArgs.At(i))
		}
	}
	if len(args) > 0 {
		fb.f.Facts.Instances = append(fb.f.Facts.Instances, Instance{Obj: fn.Origin(), Args: args})
	}
}

// ----------------------------------------------------------------------------
// Conversions and built-ins

// conversion builds T(x).
func (fb *fnb) conversion(e *ast.CallExpr) Reg {
	to := fb.typeOf(e)
	arg := e.Args[0]
	from := fb.typeOf(arg)
	if isInterface(to) {
		return fb.exprAs(arg, to)
	}
	if isBox(from) && pointerElem(to) != nil && !isBox(to) {
		// (*T)(b) of an owned, shared or synced b: a view of its object.
		return fb.pointee(arg)
	}
	v := fb.expr(arg)
	if isString(to) && !isString(from) {
		// string(runes) and string(r) copy into a new counted string.
		return fb.heap(HeapCountedCurrent)
	}
	if !hasPointers(to) {
		return 0
	}
	if !hasPointers(from) {
		// A pointer made from a uintptr (or another number): heap memory
		// by the rule of section 18.3.
		return fb.heap(HeapUintptr)
	}
	if sl, ok := coreType(unwrap(from)).(*types2.Slice); ok {
		if arr, ok := coreType(unwrap(to)).(*types2.Array); ok {
			// A slice to an array: a copy of its elements.
			_ = sl
			return fb.emitDst(Instr{Op: OpCompose, Args: []Reg{fb.load(fb.elem(v), arr.Elem())}, Fields: [][]int{{ElemStep}}, Type: to, Pos: e.Pos()})
		}
	}
	if isUnsafePointer(from) && pointerElem(to) != nil {
		// An unsafe.Pointer made a *T: it may point anywhere in the object
		// (an offset made with unsafe.Add, or a reinterpretation, such as
		// a slice header read as a struct).
		return fb.field(v, AnyStep)
	}
	return v
}

// builtin builds a call of a built-in function.
func (fb *fnb) builtin(e *ast.CallExpr) ([]Reg, []types2.Type) {
	id := identOf(ast.Unparen(e.Fun))
	bi, _ := fb.b.uses[id].(*types2.Builtin)
	if bi == nil {
		fb.b.errorf(e.Pos(), "unknown built-in")
		return nil, nil
	}
	T := fb.typeOf(e)
	one := func(r Reg) ([]Reg, []types2.Type) { return []Reg{r}, []types2.Type{T} }
	switch bi.Name() {
	case "len", "cap":
		xt := fb.typeOf(e.Args[0])
		if bi.Name() == "len" && types2.LenByMethod(xt) {
			if fb.isRingType(xt) {
				fb.expr(e.Args[0])
				return one(0)
			}
			res, _ := fb.containerCall(e.Args[0], e.Args[0], types2.LenMethodName, nil, ImplLen)
			_ = res
			return one(0)
		}
		if m := mapOf(xt); m != nil {
			mv := fb.expr(e.Args[0])
			fb.mapCall("mapLen", m, []Reg{mv}, e.Args[0].Pos(), ImplLen)
			return one(0)
		}
		fb.expr(e.Args[0])
		return one(0)
	case "close":
		fb.expr(e.Args[0])
		return nil, nil
	case "copy":
		d := fb.expr(e.Args[0])
		s := fb.expr(e.Args[1])
		fb.copyElems(d, s, sliceElem(fb.typeOf(e.Args[0])), e.Pos())
		return one(0)
	case "Drop":
		p := fb.expr(e.Args[0])
		fb.dropAt(p, pointerElem(fb.typeOf(e.Args[0])), e.Pos())
		return nil, nil
	case "take":
		p := fb.expr(e.Args[0])
		return one(fb.load(p, T))
	case "delete":
		mT := fb.typeOf(e.Args[0])
		m := mapOf(mT)
		mv := fb.expr(e.Args[0])
		k := fb.exprAs(e.Args[1], m.Key())
		fb.mapCall("mapDelete", m, []Reg{mv, k}, e.Pos(), ImplNone)
		return nil, nil
	case "clear":
		x := fb.expr(e.Args[0])
		fb.clear(x, fb.typeOf(e.Args[0]), e.Pos(), ImplNone)
		return nil, nil
	case "min", "max":
		if !hasPointers(T) {
			for _, a := range e.Args {
				fb.expr(a)
			}
			return one(0)
		}
		// One of the operands: merged through a temporary.
		o := fb.newObj(Object{Kind: ObjTemp, Type: T, Name: bi.Name()})
		for _, a := range e.Args {
			fb.store(fb.addr(o), fb.exprAs(a, T), T)
		}
		return one(fb.load(fb.addr(o), T))
	case "new":
		return one(fb.newBuiltin(e, T))
	case "make":
		return one(fb.makeBuiltin(e, T))
	case "panic":
		fb.exprAs(e.Args[0], types2.NewInterfaceType(nil, nil))
		fb.cur = nil
		return nil, nil
	case "print", "println":
		for _, a := range e.Args {
			fb.expr(a)
		}
		return nil, nil
	case "Sizeof", "Alignof", "Offsetof":
		return one(0)
	case "Add", "Slice", "SliceData", "String", "StringData":
		p := fb.expr(e.Args[0])
		for _, a := range e.Args[1:] {
			fb.expr(a)
		}
		if bi.Name() == "Add" {
			// An offset that is not known: anywhere in the object.
			p = fb.field(p, AnyStep)
		}
		return one(p)
	case "complex", "real", "imag":
		for _, a := range e.Args {
			fb.expr(a)
		}
		return one(0)
	}
	fb.b.errorf(e.Pos(), "built-in %s", bi.Name())
	return nil, nil
}

// isRingType reports whether T, or what it points to, is a ring.
func (fb *fnb) isRingType(T types2.Type) bool {
	if p := pointerElem(T); p != nil {
		T = p
	}
	w, ok := unwrap(T).Underlying().(*types2.Wrapper)
	return ok && w.Kind() == types2.WrapRing
}

// newBuiltin builds new(T) and new(v), with an optional allocator: an owned
// box, a heap object.
func (fb *fnb) newBuiltin(e *ast.CallExpr, T types2.Type) Reg {
	w, _ := unwrap(T).Underlying().(*types2.Wrapper)
	var v Reg
	isValue := !fb.b.types[e.Args[0]].IsType()
	if isValue && w != nil {
		v = fb.exprAs(e.Args[0], w.Elem())
	}
	box := fb.allocBox("boxNewWith", e.Args[1:], e.Pos())
	if w != nil {
		fb.store(box, v, w.Elem())
	}
	return box
}

// allocBox returns a new heap object, from the allocator args name if one
// is given: a call of the runtime function that takes it, which calls the
// allocator.
func (fb *fnb) allocBox(withName string, alloc []ast.Expr, pos token.Pos) Reg {
	if len(alloc) > 0 {
		fn := fb.b.rtFunc(withName)
		if fn != nil {
			sig := fn.Type().(*types2.Signature)
			args := make([]Reg, sig.Params().Len())
			if len(args) > 0 {
				args[0] = fb.exprAs(alloc[0], sig.Params().At(0).Type())
			}
			fb.emitCall(&Call{Kind: CallStatic, Callee: fb.b.funcOf(fn), Sig: sig, Args: args}, pos)
		} else {
			for _, a := range alloc {
				fb.expr(a)
			}
		}
		return fb.heap(HeapAlloc)
	}
	// No allocator: the current one (alloc.Default(), or alloc.Use's).
	return fb.heap(HeapCurrent)
}

// makeBuiltin builds make(chan T, ...) and make(map[K]V, ...).
func (fb *fnb) makeBuiltin(e *ast.CallExpr, T types2.Type) Reg {
	if m := mapOf(T); m != nil {
		rest := e.Args[1:]
		if len(rest) > 0 && !fb.isAllocatorArg(rest[0]) {
			fb.expr(rest[0])
			rest = rest[1:]
		}
		if len(rest) > 0 {
			fn := fb.b.rtFunc("mapMakeWith")
			var a Reg
			if fn != nil {
				a = fb.exprAs(rest[0], fn.Type().(*types2.Signature).Params().At(0).Type())
			}
			res := fb.mapCall("mapMakeWith", m, []Reg{a, 0}, e.Pos(), ImplNone)
			if len(res) > 0 {
				return res[0]
			}
			return 0
		}
		res := fb.mapCall("mapMake", m, []Reg{0}, e.Pos(), ImplNone)
		if len(res) > 0 {
			return res[0]
		}
		return 0
	}
	rest := e.Args[1:]
	if len(rest) > 0 && !fb.isAllocatorArg(rest[0]) {
		fb.expr(rest[0])
		rest = rest[1:]
	}
	return fb.allocBox("makechanWith", rest, e.Pos())
}

// isAllocatorArg reports whether an argument of make is an allocator, not
// a size.
func (fb *fnb) isAllocatorArg(a ast.Expr) bool {
	T := fb.typeOf(a)
	if T == nil {
		return false
	}
	if b, ok := unwrap(T).Underlying().(*types2.Basic); ok && b.Info()&types2.IsInteger != 0 {
		return false
	}
	if tv := fb.b.types[a]; !tv.Value.IsNil() {
		return false
	}
	return true
}

// clear builds clear(x): a map's entries are deleted (mapClear); a slice's
// elements are destroyed and zeroed.
func (fb *fnb) clear(x Reg, T types2.Type, pos token.Pos, impl Implicit) {
	if m := mapOf(T); m != nil {
		fb.mapCall("mapClear", m, []Reg{x}, pos, impl)
		return
	}
	if et := sliceElem(T); et != nil && fb.b.needsDrop(et) {
		fb.dropAt(fb.elem(x), et, pos)
	}
}

// copyElems builds copy(dst, src).
func (fb *fnb) copyElems(dst, src Reg, et types2.Type, pos token.Pos) {
	if et == nil || !hasPointers(et) {
		return
	}
	fb.store(fb.elem(dst), fb.load(fb.elem(src), et), et)
}

// dropAt destroys the value of type T at addr: a call of its drop glue,
// if destroying it runs any (section 18.6).
func (fb *fnb) dropAt(addr Reg, T types2.Type, pos token.Pos) {
	if T == nil || !fb.b.needsDrop(T) {
		return
	}
	if isTypeParam(T) {
		// The drop of a T: each instantiation's drop glue (a call with
		// no method on the type parameter, which sched resolves).
		fb.drops++
		p := types2.NewVar(token.NoPos, nil, "p", types2.NewPointer(T))
		sig := types2.NewSignatureType(nil, nil, nil, types2.NewTuple(p), nil, false)
		fb.emitCall(&Call{Kind: CallTypeParam, Iface: T, Sig: sig, Args: []Reg{addr}, Implicit: ImplDrop}, pos)
		return
	}
	id := fb.b.synth(Synth{Kind: SynthDropGlue, Recv: T})
	sig := fb.b.p.Funcs[id].Sig
	fb.drops++
	fb.emitCall(&Call{Kind: CallStatic, Callee: id, Sig: sig, Args: []Reg{addr}, Implicit: ImplDrop}, pos)
}

// ----------------------------------------------------------------------------
// The runtime's generic functions

// rtFunc returns the function name of package pluto/rt, or nil.
func (b *builder) rtFunc(name string) *types2.Func {
	for _, p := range b.prog.Packages {
		if p.Path == "pluto/rt" {
			fn, _ := p.Types.Scope().Lookup(name).(*types2.Func)
			return fn
		}
	}
	return nil
}

// rtInstance instantiates the generic type name of pluto/rt with a map's
// key and element types.
func (b *builder) rtInstance(name string, m *types2.Map) types2.Type {
	for _, p := range b.prog.Packages {
		if p.Path != "pluto/rt" {
			continue
		}
		tn, ok := p.Types.Scope().Lookup(name).(*types2.TypeName)
		if !ok {
			return nil
		}
		T, err := types2.Instantiate(b.ctxt, tn.Type(), []types2.Type{m.Key(), m.Elem()}, false)
		if err != nil {
			return nil
		}
		return T
	}
	return nil
}

// mapCall calls the generic function name of pluto/rt (map.p) for map
// type m: the lowerer's map operations are calls of these.
func (fb *fnb) mapCall(name string, m *types2.Map, args []Reg, pos token.Pos, impl Implicit) []Reg {
	fn := fb.b.rtFunc(name)
	if fn == nil {
		fb.b.errorf(pos, "pluto/rt.%s is missing", name)
		return nil
	}
	fb.f.Facts.Instances = append(fb.f.Facts.Instances, Instance{Obj: fn, Args: []types2.Type{m.Key(), m.Elem()}})
	sig := fn.Type().(*types2.Signature)
	if q := "pluto/rt." + name; contract.IsTrusted(q) {
		return fb.emitCall(&Call{Kind: CallContract, Contract: q, Sig: sig, Args: args, Implicit: impl}, pos)
	}
	res := fb.emitCall(&Call{Kind: CallStatic, Callee: fb.b.funcOf(fn), Sig: sig, Args: args, Implicit: impl}, pos)
	if name == "mapMake" || name == "mapMakeWith" {
		// A new table, made from the allocator's memory through unsafe
		// pointers: a counted reference to heap memory.
		if name == "mapMake" {
			// No allocator argument: the current allocator's memory.
			return []Reg{fb.heap(HeapCountedCurrent)}
		}
		return []Reg{fb.heap(HeapCounted)}
	}
	return res
}

// methodOn calls method name of T on the T at recv.
func (fb *fnb) methodOn(T types2.Type, name string, recv Reg, args []Reg, pos token.Pos, impl Implicit) []Reg {
	obj, _, _ := types2.LookupFieldOrMethod(T, true, nil, name)
	fn, ok := obj.(*types2.Func)
	if !ok {
		fb.b.errorf(pos, "%s has no method %s", T, name)
		return nil
	}
	sig := fn.Type().(*types2.Signature)
	if _, byPtr := sig.Recv().Type().(*types2.Pointer); !byPtr {
		recv = fb.load(recv, T)
	}
	if n, ok := types2.Unalias(T).(*types2.Named); ok && n.TypeArgs() != nil {
		var targs []types2.Type
		for i := 0; i < n.TypeArgs().Len(); i++ {
			targs = append(targs, n.TypeArgs().At(i))
		}
		fb.f.Facts.Instances = append(fb.f.Facts.Instances, Instance{Obj: fn.Origin(), Args: targs})
		if q := fn.Pkg().Path() + "." + n.Obj().Name() + "." + name; contract.IsTrusted(q) {
			return fb.emitCall(&Call{Kind: CallContract, Contract: q, Sig: sig, Args: append([]Reg{recv}, args...), Implicit: impl}, pos)
		}
	}
	return fb.emitCall(&Call{Kind: CallStatic, Callee: fb.b.funcOf(fn), Sig: sig, Args: append([]Reg{recv}, args...), Implicit: impl}, pos)
}

// ----------------------------------------------------------------------------
// Containers (section 11.11)

// isContainerIndex reports whether e calls a container's indexing methods.
func (fb *fnb) isContainerIndex(e *ast.IndexExpr) bool {
	tv := fb.b.types[e.X]
	return tv.Type != nil && !tv.IsType() && types2.IndexesByMethod(tv.Type)
}

// isMapIndex reports whether e indexes a built-in map.
func (fb *fnb) isMapIndex(e *ast.IndexExpr) bool {
	return mapOf(fb.typeOf(e.X)) != nil
}

// indexOk builds v, ok := x[k].
func (fb *fnb) indexOk(e *ast.IndexExpr) ([]Reg, []types2.Type) {
	lv := fb.lvalue(e)
	if lv.kind == lvMap {
		res := fb.mapCall("mapAccessOk", lv.c.mapT, []Reg{lv.c.mapV, lv.c.key}, e.Pos(), ImplIndexOk)
		return res, []types2.Type{lv.c.mapT.Elem(), types2.Typ[types2.Bool]}
	}
	if lv.kind == lvContainer {
		return fb.contMethod(lv.c, types2.IndexOkMethodName, []Reg{lv.c.key}, ImplIndexOk)
	}
	return []Reg{0, 0}, []types2.Type{types2.Typ[types2.Int], types2.Typ[types2.Bool]}
}

// container evaluates a container operand once, as the receiver of its
// methods.
func (fb *fnb) container(x ast.Expr, at ast.Expr) *contRef {
	T := unwrap(fb.typeOf(x))
	c := &contRef{T: T, x: x, at: at}
	switch {
	case isInterface(T):
		c.iface = fb.expr(x)
		c.isIfc = true
	case pointerElem(T) != nil:
		c.base = fb.pointee(x)
		c.baseT = pointerElem(T)
	default:
		if a, ok := fb.addrIfPlace(x); ok {
			c.base, c.baseT = a, T
			break
		}
		v := fb.expr(x)
		o := fb.newObj(Object{Kind: ObjTemp, Type: T, Name: "recv"})
		fb.store(fb.addr(o), v, T)
		c.base, c.baseT = fb.addr(o), T
	}
	return c
}

// containerCall calls the method name of the container x, at the
// expression at.
func (fb *fnb) containerCall(x, at ast.Expr, name string, args []Reg, impl Implicit) ([]Reg, []types2.Type) {
	return fb.contMethod(fb.container(x, at), name, args, impl)
}

// contMethod calls the method name of the container c refers to.
func (fb *fnb) contMethod(c *contRef, name string, args []Reg, impl Implicit) ([]Reg, []types2.Type) {
	T := c.T
	if !c.isIfc {
		T = c.baseT
	}
	obj, path, _ := types2.LookupFieldOrMethod(unwrap(T), true, nil, name)
	fn, ok := obj.(*types2.Func)
	if !ok {
		fb.b.errorf(c.at.Pos(), "%s has no method %s", T, name)
		return nil, nil
	}
	sig := fn.Type().(*types2.Signature)
	pos := c.at.Pos()
	if c.isIfc {
		return fb.emitCall(&Call{Kind: CallIface, Method: fn, Iface: c.T, Target: c.iface, Sig: sig, Args: args, Implicit: impl}, pos), resultTypes(sig)
	}
	_, wantPtr := sig.Recv().Type().(*types2.Pointer)
	if isTypeParam(T) {
		// The value, or for a pointer method of an instance that is not a
		// pointer, the address (RecvAddr).
		recv := fb.load(c.base, T)
		return fb.emitCall(&Call{Kind: CallTypeParam, Method: fn, Iface: T, Sig: sig, Args: append([]Reg{recv}, args...), Implicit: impl, RecvAddr: c.base}, pos), resultTypes(sig)
	}
	if isIfaceMethod(fn) {
		recv := fb.receiverAt(c.base, c.baseT, path, false, fn)
		I := fb.recvFieldType(c.baseT, path)
		return fb.emitCall(&Call{Kind: CallIface, Method: fn, Iface: I, Target: recv, Sig: sig, Args: args, Implicit: impl}, pos), resultTypes(sig)
	}
	if fb.isRingMethod(fn) {
		// A ring's methods are expanded inline; its iterators are
		// functions the lowerer makes, which read the ring.
		return fb.emitCall(&Call{Kind: CallContract, Contract: contract.RingMethod, Sig: sig, Args: append([]Reg{c.base}, args...), Implicit: impl}, pos), resultTypes(sig)
	}
	recv := fb.receiverAt(c.base, c.baseT, path, wantPtr, fn)
	if n, ok := types2.Unalias(fb.recvFieldTypeDeref(c.baseT, path)).(*types2.Named); ok && n.TypeArgs() != nil {
		var targs []types2.Type
		for i := 0; i < n.TypeArgs().Len(); i++ {
			targs = append(targs, n.TypeArgs().At(i))
		}
		fb.f.Facts.Instances = append(fb.f.Facts.Instances, Instance{Obj: fn.Origin(), Args: targs})
	}
	return fb.emitCall(&Call{Kind: CallStatic, Callee: fb.b.funcOf(fn), Sig: sig, Args: append([]Reg{recv}, args...), Implicit: impl}, pos), resultTypes(sig)
}

// recvFieldTypeDeref is recvFieldType without the pointer.
func (fb *fnb) recvFieldTypeDeref(T types2.Type, path []int) types2.Type {
	R := fb.recvFieldType(T, path)
	if el := pointerElem(R); el != nil {
		return el
	}
	return R
}

// indexKeyType returns the key type of a container's indexing methods.
func (fb *fnb) indexKeyType(c *contRef) types2.Type {
	if sig := fb.contSig(c, types2.IndexMethodName, types2.SetIndexMethodName, types2.IndexOkMethodName); sig != nil {
		return sig.Params().At(0).Type()
	}
	return nil
}

// indexElemType returns the element type of a container's indexing
// methods.
func (fb *fnb) indexElemType(c *contRef) types2.Type {
	for _, name := range []string{types2.IndexMethodName, types2.SetIndexMethodName, types2.IndexOkMethodName} {
		if sig := fb.contSig(c, name); sig != nil {
			if name == types2.SetIndexMethodName {
				return sig.Params().At(1).Type()
			}
			return sig.Results().At(0).Type()
		}
	}
	return nil
}

// contSig returns the signature of the first of the named methods the
// container has.
func (fb *fnb) contSig(c *contRef, names ...string) *types2.Signature {
	T := c.T
	if !c.isIfc {
		T = c.baseT
	}
	for _, name := range names {
		obj, _, _ := types2.LookupFieldOrMethod(unwrap(T), true, nil, name)
		if fn, ok := obj.(*types2.Func); ok {
			return fn.Type().(*types2.Signature)
		}
	}
	return nil
}
