package lower

import (
	"fmt"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// A callee is what a call invokes.
type callee struct {
	sym      *ssa.Sym   // static target
	code     *ssa.Value // indirect: code pointer
	ctx      *ssa.Value // indirect: closure record passed to the callee
	noReturn bool
	site     *callSite // the call as written, for the interrupt-context check
	// deferred marks a deferred call, after which the caller does not
	// check for rt.Goexit: it runs its remaining deferred calls first.
	deferred bool
	// alloc marks a call to an Allocator's Alloc or Free method.
	alloc ssa.AllocMethod
}

// callExpr lowers a call expression and returns its single result (or the
// first result; multi-value calls go through callResults).
func (s *fnState) callExpr(e *ast.CallExpr) value {
	res := s.callResults(e)
	if len(res) == 0 {
		return value{}
	}
	return res[0]
}

// callResults lowers a call and returns all of its results.
func (s *fnState) callResults(e *ast.CallExpr) []value {
	funTV := s.info.Types[e.Fun]
	if funTV.IsType() {
		return []value{s.conversion(e)}
	}
	if funTV.IsBuiltin() {
		return s.builtin(e)
	}

	fun := ast.Unparen(e.Fun)
	if ix, ok := fun.(*ast.IndexExpr); ok && s.isFuncInstance(ix.X) {
		fun = ix.X
	} else if ix, ok := fun.(*ast.IndexListExpr); ok {
		fun = ix.X
	}

	if t := s.l.scoped.Tasks[e]; t != nil {
		s.scopedTask(e, t)
		return nil
	}
	s.goFuncCall(e, fun)

	// Method calls, and constructions of union variants (U.V(payload)).
	if se, ok := fun.(*ast.SelectorExpr); ok {
		if v, ok := s.info.Uses[se.Sel].(*types2.Variant); ok {
			return []value{s.variantCall(e, v)}
		}
		if sel := s.info.Selections[se]; sel != nil {
			switch sel.Kind() {
			case types2.MethodVal:
				return s.methodCall(e, se, sel)
			case types2.MethodExpr:
				sig := s.subst(s.info.Types[e.Fun].Type).(*types2.Signature)
				sym := s.methodExpr(se, sel, sig)
				if sym == nil {
					return nil
				}
				args := s.callArgs(e, sig, nil)
				return s.emitCall(callee{sym: sym}, sig, nil, args, e.Pos())
			}
		}
	}

	// Static calls of declared functions.
	if id := staticFuncIdent(fun, s.info); id != nil {
		if fn, ok := s.info.Uses[id].(*types2.Func); ok {
			if fn.Pkg() != nil && fn.Pkg().Path()+"."+fn.Name() == handlerAddrFunc {
				return []value{s.handlerAddr(e, s.typeOf(e))}
			}
			if types2.IsErrorsNew(fn) {
				return s.errorsNew(e, fn)
			}
			if res, ok := s.intrinsic(fn, e); ok {
				return res
			}
			if res, ok := s.countIntrinsic(fn, e, id); ok {
				return res
			}
			if s.l.isPanicHook(fn) {
				return s.callPanicHandler(e)
			}
			targs := s.instanceArgs(id)
			sig := s.subst(s.info.Types[e.Fun].Type).(*types2.Signature)
			sym := s.l.funcSym(fn, targs)
			args := s.callArgs(e, sig, &defaultsOf{fn, targs})
			site := &callSite{desc: types2.ExprString(e.Fun)}
			return s.emitCall(callee{sym: sym, noReturn: s.l.isNoReturn(fn), site: site}, sig, nil, args, e.Pos())
		}
	}

	// A call through a func value.
	fv := s.scalar(e.Fun)
	sig := s.typeOf(e.Fun).Underlying().(*types2.Signature)
	args := s.callArgs(e, sig, nil)
	code := s.load(ssa.TPtr, fv)
	site := &callSite{desc: types2.ExprString(e.Fun), fsig: sig}
	return s.emitCall(callee{code: code, ctx: fv, site: site}, sig, nil, args, e.Pos())
}

func (s *fnState) isFuncInstance(x ast.Expr) bool {
	_, ok := s.typeOf(x).Underlying().(*types2.Signature)
	return ok
}

// An argSource is one argument of a call: an expression still to be
// evaluated, or a value already computed (one result of f(g()) where g
// returns several).
type argSource struct {
	e ast.Expr
	v value
}

// argSources returns the arguments of a call, spreading the results of a
// lone multi-value call argument.
func (s *fnState) argSources(args []ast.Expr) []argSource {
	if len(args) == 1 {
		if call, ok := ast.Unparen(args[0]).(*ast.CallExpr); ok {
			if tup, ok := s.info.Types[call].Type.(*types2.Tuple); ok && tup.Len() > 1 {
				var out []argSource
				for _, r := range s.callResults(call) {
					out = append(out, argSource{v: r})
				}
				return out
			}
		}
	}
	out := make([]argSource, len(args))
	for i, a := range args {
		out[i] = argSource{e: a}
	}
	return out
}

// defaultsOf identifies the declared function a call names, and the type
// arguments of the instance it calls, so that the call can evaluate the
// default values of the parameters it omits.
type defaultsOf struct {
	fn    *types2.Func
	targs []types2.Type
}

// callArgs evaluates a call's arguments in order, packing variadic
// arguments into a slice whose backing array lives in this frame. A call
// that names its callee (def is not nil) may omit trailing parameters that
// have default values; their defaults are evaluated after the explicit
// arguments.
func (s *fnState) callArgs(e *ast.CallExpr, sig *types2.Signature, def *defaultsOf) []value {
	args := s.lowerArgs(e, sig, def)
	s.callAt = e // for callRegion
	return args
}

func (s *fnState) lowerArgs(e *ast.CallExpr, sig *types2.Signature, def *defaultsOf) []value {
	params := sig.Params()
	n := params.Len()
	args := s.argSources(e.Args)
	as := func(a argSource, T types2.Type) value {
		var v value
		if a.e != nil {
			v = s.exprAs(a.e, T)
		} else {
			v = s.assignConvert(a.v, T, e)
		}
		// The callee borrows its arguments; one that the call could
		// change under it is kept alive by a reference of the caller's.
		return s.hold(v)
	}

	var out []value
	fixed := n
	if sig.Variadic() && !e.Ellipsis.IsValid() {
		fixed = n - 1
	}
	for i := 0; i < fixed && i < len(args); i++ {
		out = append(out, s.materialize(as(args[i], s.subst(params.At(i).Type()))))
	}
	if !sig.Variadic() && len(args) < n {
		if def == nil {
			s.l.internalf(e.Pos(), "call has %d arguments, signature has %d parameters", len(args), n)
			return out
		}
		for i := len(args); i < n; i++ {
			out = append(out, s.defaultArg(def, i, n, s.subst(params.At(i).Type()), e))
		}
	}
	if sig.Variadic() && !e.Ellipsis.IsValid() {
		sliceT := s.subst(params.At(n - 1).Type())
		et := sliceT.Underlying().(*types2.Slice).Elem()
		extra := args[min(fixed, len(args)):]
		if len(extra) == 0 {
			out = append(out, s.zeroValue(sliceT))
		} else {
			arrT := types2.NewArray(et, int64(len(extra)))
			esize := s.l.sizeof(et)
			if s.l.counted(et) && !s.staticLits {
				// The array may outlive the statement (a closure
				// constructor keeps it), so it holds references of its
				// own until the function returns.
				arr := s.frameSlot(arrT, "varargs")
				for i, a := range extra {
					s.storeCounted(lvalue{ptr: s.offPtr(arr, int64(i)*esize), T: et}, as(a, et), false)
				}
				nv := s.constInt(ssa.TI64, int64(len(extra)))
				out = append(out, s.makeSlice(arr, nv, nv, sliceT))
				return out
			}
			arr := s.lastingTemp(arrT, "varargs")
			for i, a := range extra {
				v := as(a, et)
				dst := s.offPtr(arr, int64(i)*esize)
				s.storeTo(dst, v, et)
				if s.l.counted(et) {
					s.takeRefAt(v, dst, et) // static storage, kept for good
				}
			}
			nv := s.constInt(ssa.TI64, int64(len(extra)))
			out = append(out, s.makeSlice(arr, nv, nv, sliceT))
		}
	}
	return out
}

// defaultArg evaluates the default value of parameter i (of n) of the
// callee def names, converted to the parameter type T. The default is
// lowered here, in the caller's frame, as if it were written at the call
// site; but its names were resolved in the callee's declaration, so it is
// lowered with the callee package's type information and the callee
// instance's type arguments. That lets it use the callee package's
// unexported names and the callee's type parameters.
func (s *fnState) defaultArg(def *defaultsOf, i, n int, T types2.Type, e *ast.CallExpr) value {
	fn := def.fn.Origin()
	defs := fn.Defaults()
	k := i - (n - len(defs))
	d := s.l.decls[fn]
	if k < 0 || d == nil {
		s.l.internalf(e.Pos(), "no argument for parameter %d of %s", i+1, fn.Name())
		return s.zeroValue(T)
	}
	pkg, info, tmap := s.pkg, s.info, s.tmap
	s.pkg, s.info, s.tmap = d.pkg, d.pkg.Info, nil
	if len(def.targs) > 0 {
		s.tmap = newTypeMap(s.l, fn, def.targs)
	}
	firstValue, firstBlock := s.f.NumValues(), s.f.NumBlocks()
	v := s.hold(s.materialize(s.exprAs(defs[k], T)))
	s.pkg, s.info, s.tmap = pkg, info, tmap
	s.moveToCall(firstValue, firstBlock, e.Pos())
	return v
}

// moveToCall gives the code made for a default argument, the values and
// blocks numbered from firstValue and firstBlock on, the position of the
// call. The default's own positions are in the callee's declaration, but
// its code is the caller's, and a stack trace or debugger reports a frame
// at a line of its own function. (Errors still name the default, through
// fnState.pos.)
func (s *fnState) moveToCall(firstValue, firstBlock int, call token.Pos) {
	if !call.IsValid() {
		return
	}
	for _, b := range s.f.Blocks {
		if b.ID >= firstBlock && b.Pos.IsValid() {
			b.Pos = call
		}
		for _, w := range b.Values {
			if w.ID >= firstValue && w.Pos.IsValid() {
				w.Pos = call
			}
		}
	}
}

// emitCall emits a call and returns its results as values.
func (s *fnState) emitCall(c callee, sig *types2.Signature, recv *value, args []value, pos token.Pos) []value {
	if c.sym != nil {
		s.checkNotHandler(c.sym, pos, "called")
		if s.l.isCFunc(c.sym) {
			// An unsupported signature was reported at its declaration.
			if cs, err := s.l.cSignature(sig); err == nil && !cs.internal {
				return s.emitCCall(c, cs, recv, args, pos)
			}
		}
	}
	abi := s.l.lowerSig(sig)
	var flat []*ssa.Value
	var sretBuf *ssa.Value
	if abi.sret {
		size, align := s.l.sretSize(abi)
		sretBuf = s.tempSize(size, align, "sret")
		// Leaves let SROA split the buffer once the call is inlined. They
		// must cover every result, so a result held in memory as a unit
		// leaves the buffer unsplittable. The pads are the padding inside
		// each result and the alignment gaps between them.
		slot := sretBuf.Aux.(*ssa.Slot)
		offs := s.l.resultOffsets(abi)
		for i, p := range abi.results {
			lfs := s.l.leaves(p.typ)
			if lfs == nil && s.l.sizeof(p.typ) > 0 {
				slot.Leaves, slot.Pads = nil, nil
				break
			}
			for _, lf := range lfs {
				slot.Leaves = append(slot.Leaves, ssa.Leaf{Off: offs[i] + lf.off, Type: lf.t})
			}
			s.l.appendPads(&slot.Pads, p.typ, offs[i])
			end := slot.Size
			if i+1 < len(offs) {
				end = offs[i+1]
			}
			if gap := offs[i] + s.l.sizeof(p.typ); end > gap {
				slot.Pads = append(slot.Pads, ssa.Pad{Off: gap, Size: end - gap})
			}
		}
		flat = append(flat, sretBuf)
	}
	if abi.region {
		flat = append(flat, s.callRegion(c, sig, pos))
	}
	all := args
	if recv != nil {
		all = append([]value{*recv}, args...)
	}
	if len(all) != len(abi.params) {
		s.l.internalf(pos, "call has %d arguments, signature has %d parameters", len(all), len(abi.params))
		return nil
	}
	for i, p := range abi.params {
		v := all[i]
		if p.byRef {
			// The callee owns this copy and may modify it.
			cp := s.temp(p.typ, "arg")
			s.storeTo(cp, v, p.typ)
			flat = append(flat, cp)
			continue
		}
		flat = append(flat, s.leafValues(v, p.leaves)...)
	}

	call := s.callValue(c, flat, abi.flatResults)
	if pos.IsValid() {
		call.Pos = pos
	}
	s.endCall(call, c.noReturn)
	if s.callMayGoexit(c, sig) {
		s.checkGoexit(pos)
	}

	var out []value
	if abi.sret {
		offs := s.l.resultOffsets(abi)
		for i, p := range abi.results {
			if s.l.counted(p.typ) {
				// The result's references are the caller's: keep them in
				// a temporary of their own, since the sret buffer is
				// reused by every execution of this call.
				tmp := s.temp(p.typ, "res")
				s.move(tmp, s.offPtr(sretBuf, offs[i]), s.l.sizeof(p.typ))
				out = append(out, s.ownTemp(tmp, p.typ))
				continue
			}
			out = append(out, s.loadFrom(s.offPtr(sretBuf, offs[i]), p.typ))
		}
		return out
	}
	k := 1
	for _, p := range abi.results {
		if len(p.leaves) == 1 && p.leaves[0].off == 0 && s.l.scalarType(p.typ) != nil {
			r := s.selectN(call, k, p.leaves[0].t)
			k++
			if s.l.counted(p.typ) {
				// A shared, synced or weak reference: the callee handed
				// over a reference, which the statement holds.
				out = append(out, s.wordTemp(r, p.typ, "res"))
				continue
			}
			out = append(out, scalarValue(r, p.typ))
			continue
		}
		tmp := s.temp(p.typ, "res")
		for _, lf := range p.leaves {
			s.store(lf.t, s.offPtr(tmp, lf.off), s.selectN(call, k, lf.t))
			k++
		}
		if s.l.counted(p.typ) {
			// Results are owned: the callee handed over a reference.
			out = append(out, s.ownTemp(tmp, p.typ))
			continue
		}
		out = append(out, aggValue(tmp, p.typ))
	}
	return out
}

// endCall records the memory state after call and, if the callee never
// returns, ends the block there.
func (s *fnState) endCall(call *ssa.Value, noReturn bool) {
	s.setMem(s.newValue(ssa.OpSelectN, ssa.TMem, call))
	if noReturn {
		call.Call.NoReturn = true
		s.b.Kind = ssa.BlockExit
		s.b.SetControl(s.mem())
		s.b = nil
		// Code after a call that does not return is unreachable; give it
		// a block so lowering can continue harmlessly.
		dead := s.newBlock(ssa.BlockInvalid)
		s.seal(dead)
		s.startBlock(dead)
	}
}

func (s *fnState) selectN(call *ssa.Value, i int, t *ssa.Type) *ssa.Value {
	v := s.newValue(ssa.OpSelectN, t, call)
	v.AuxInt = int64(i)
	return v
}

// callValue emits the call op itself.
func (s *fnState) callValue(c callee, flat []*ssa.Value, results []*ssa.Type) *ssa.Value {
	if s.f.NoSSE {
		s.checkFloatCall(flat, results)
	}
	rt := ssa.TupleOf(append([]*ssa.Type{ssa.TMem}, results...)...)
	if c.sym != nil && c.sym.Kind == ssa.SymExtern {
		// Code outside Plutonium sees plain addresses.
		for i, a := range flat {
			if a.Type.IsPtr() {
				flat[i] = s.untagged(a)
			}
		}
	}
	var call *ssa.Value
	if c.sym != nil {
		call = s.newValue(ssa.OpCall, rt)
		call.Aux = c.sym
	} else {
		call = s.newValue(ssa.OpCallInd, rt, c.code, c.ctx)
	}
	if c.site != nil {
		s.l.sites[call] = c.site
	}
	for _, a := range flat {
		call.AddArg(a)
	}
	call.AddArg(s.mem())
	s.callLog = append(s.callLog, call) // for string holds (strcount.go)
	call.Call = &ssa.CallInfo{ABI: ssa.ABIInternal, Results: results, NoReturn: c.noReturn, Alloc: c.alloc}
	return call
}

// ----------------------------------------------------------------------------
// Runtime calls

// rtFunc returns the named function of package pluto/rt.
func (l *Lowerer) rtFunc(name string) *types2.Func {
	for _, p := range l.prog.Packages {
		if p.Path == "pluto/rt" {
			if fn, ok := p.Types.Scope().Lookup(name).(*types2.Func); ok {
				return fn
			}
		}
	}
	panic(fmt.Sprintf("lower: pluto/rt.%s is missing", name))
}

// callRuntime calls pluto/rt.name with already-flattened scalar arguments and
// returns its scalar results.
func (s *fnState) callRuntime(name string, results []*ssa.Type, argGroups ...[]*ssa.Value) []*ssa.Value {
	var flat []*ssa.Value
	fn := s.l.rtFunc(name)
	sym := s.l.funcSym(fn, nil)
	if sig := fn.Type().(*types2.Signature); regionSig(sig) {
		flat = append(flat, s.callRegion(callee{sym: sym}, sig, token.NoPos))
	}
	for _, g := range argGroups {
		flat = append(flat, g...)
	}
	call := s.callValue(callee{sym: sym}, flat, results)
	call.Call.Implicit = true
	s.setMem(s.newValue(ssa.OpSelectN, ssa.TMem, call))
	out := make([]*ssa.Value, len(results))
	for i, t := range results {
		out[i] = s.selectN(call, i+1, t)
	}
	return out
}

// callRuntimeNoReturn calls a pluto/rt panic function and ends the block.
func (s *fnState) callRuntimeNoReturn(name string, args ...*ssa.Value) {
	sym := s.l.funcSym(s.l.rtFunc(name), nil)
	call := s.callValue(callee{sym: sym, noReturn: true}, args, nil)
	s.setMem(s.newValue(ssa.OpSelectN, ssa.TMem, call))
	s.b.Kind = ssa.BlockExit
	s.b.SetControl(s.mem())
	s.b = nil
}

// ----------------------------------------------------------------------------
// Methods

// methodCall lowers x.m(args).
func (s *fnState) methodCall(e *ast.CallExpr, se *ast.SelectorExpr, sel *types2.Selection) []value {
	fn := sel.Obj().(*types2.Func)
	recvT := s.typeOf(se.X)
	sig := s.subst(s.info.Types[e.Fun].Type).(*types2.Signature)

	// x.Drop() on an owner variable ends its life: its whole drop glue
	// runs (Drop, then its fields), and x is left clear, so that the end
	// of its scope destroys nothing (language spec section 18.6).
	if fn.Name() == "Drop" && len(e.Args) == 0 && s.isMove(se.X) {
		lv := s.lv(se.X)
		T := unwrap(lv.T)
		s.dropAt(lv.ptr, T)
		s.zero(lv.ptr, s.l.sizeof(T))
		return nil
	}

	// weak[P].Upgrade and owned[T].Clone (box.go).
	if w := types2.BoxMethodOf(fn); w != nil && fn.Name() != "Drop" {
		rw := unwrap(s.subst(recvT)).Underlying().(*types2.Wrapper)
		if fn.Name() == "Clone" {
			return []value{s.cloneBox(s.expr(se.X), rw, s.typeOf(e))}
		}
		return s.upgrade(s.expr(se.X), rw, s.typeOf(e))
	}

	// Interface method calls.
	desc := types2.ExprString(e.Fun)
	if isInterface(recvT) && len(sel.Index()) == 1 {
		iv := s.expr(se.X)
		args := s.callArgs(e, sig, nil)
		return s.ifaceCall(iv, recvT, fn, sig, args, e.Pos(), desc)
	}

	fn, path, ok := s.selectedMethod(se, sel)
	if !ok {
		return nil
	}
	// Built-in ring methods.
	if w := s.ringOf(fn); w != nil {
		return s.ringCall(e, se, fn, path, w)
	}
	recv := s.methodReceiver(se.X, fn, path)
	if isIfaceMethod(fn) {
		// A method promoted from an embedded interface field.
		args := s.callArgs(e, sig, nil)
		return s.ifaceCall(recv, recv.T, fn, sig, args, e.Pos(), desc)
	}
	targs := s.methodTypeArgs(fn, se, recvT)
	sym := s.l.funcSym(fn, targs)
	full := s.l.methodSig(fn, targs)
	recv = s.hold(recv)
	args := s.callArgs(e, sig, &defaultsOf{fn, targs})
	site := &callSite{desc: desc, recv: types2.ExprString(se.X)}
	return s.emitCall(callee{sym: sym, noReturn: s.l.isNoReturn(fn), site: site}, full, &recv, args, e.Pos())
}

// methodTypeArgs returns the type arguments of the method instance that
// se selects with a receiver of type recvT: the receiver type's arguments,
// followed by the method's own when it has type parameters. The checker
// records the method's own arguments, explicit or inferred, on se.Sel.
func (s *fnState) methodTypeArgs(fn *types2.Func, se *ast.SelectorExpr, recvT types2.Type) []types2.Type {
	targs := s.recvTypeArgs(fn.Type().(*types2.Signature).Recv().Type())
	if len(targs) == 0 {
		// The receiver's type arguments come from the receiver expression.
		targs = s.recvTypeArgs(s.subst(recvT))
	}
	return s.withMethodTypeArgs(fn, se.Sel, targs)
}

// withMethodTypeArgs appends the type arguments of the instance of method
// fn named by id to the receiver's type arguments rtargs.
func (s *fnState) withMethodTypeArgs(fn *types2.Func, id *ast.Ident, rtargs []types2.Type) []types2.Type {
	n := fn.Origin().Type().(*types2.Signature).TypeParams().Len()
	if n == 0 {
		return rtargs
	}
	own := s.instanceArgs(id)
	if len(own) != n {
		s.l.internalf(id.Pos(), "method %s is not instantiated", fn.Name())
		return rtargs
	}
	return append(append([]types2.Type(nil), rtargs...), own...)
}

// methodSig returns a method's signature with its receiver, instantiated.
func (l *Lowerer) methodSig(fn *types2.Func, targs []types2.Type) *types2.Signature {
	sig := fn.Origin().Type().(*types2.Signature)
	if len(targs) == 0 {
		return sig
	}
	tm := newTypeMap(l, fn.Origin(), targs)
	inst := tm.subst(sig).(*types2.Signature)
	// Subst leaves the receiver alone (it would recurse through the
	// receiver's own methods), so substitute it separately.
	recv := sig.Recv()
	r := types2.NewVar(recv.Pos(), recv.Pkg(), recv.Name(), tm.subst(recv.Type()))
	return types2.NewSignatureType(r, nil, nil, inst.Params(), inst.Results(), inst.Variadic())
}

// selectedMethod returns the method that the selection x.m reaches in this
// instance of the function, with the path to it as in Selection.Index. When
// x is a value of a type parameter, the checker selected a method of the
// constraint; the call goes to the method of the type argument instead,
// which may be promoted from an embedded field or have a pointer receiver.
func (s *fnState) selectedMethod(se *ast.SelectorExpr, sel *types2.Selection) (*types2.Func, []int, bool) {
	fn := sel.Obj().(*types2.Func)
	if _, ok := types2.Unalias(s.info.Types[se.X].Type).(*types2.TypeParam); !ok {
		return fn, sel.Index(), true
	}
	T := s.typeOf(se.X)
	obj, index, _ := types2.LookupFieldOrMethod(T, false, fn.Pkg(), fn.Name())
	m, ok := obj.(*types2.Func)
	if !ok {
		s.l.internalf(se.Sel.Pos(), "%s has no method %s", T, fn.Name())
		return nil, nil, false
	}
	return m, index, true
}

// isIfaceMethod reports whether fn is declared by an interface.
func isIfaceMethod(fn *types2.Func) bool {
	recv := fn.Type().(*types2.Signature).Recv()
	return recv != nil && isInterface(recv.Type())
}

// methodReceiver evaluates the receiver of a method call or method value,
// following embedded fields and taking the address or dereferencing as the
// method's receiver type requires. fn is the method and path the selection
// path to it.
func (s *fnState) methodReceiver(x ast.Expr, fn *types2.Func, path []int) value {
	// Named types cannot be pointers when they have methods, so a pointer
	// receiver is always a literal *T.
	_, wantPtr := fn.Type().(*types2.Signature).Recv().Type().(*types2.Pointer)

	xt := s.typeOf(x)
	var cur lvalue
	switch {
	case s.isMove(x) && len(path) == 1 && !wantPtr:
		// A value receiver that moves out of its variable.
		return s.expr(x)
	case s.isTemp(x):
		// The method works on the statement's temporary itself, which is
		// destroyed afterwards, not on a copy.
		cur = lvalue{ptr: s.tempSlot(x, xt), T: xt}
		return s.receiverAt(cur, wantPtr, path)
	}
	if isPointerLike(xt) {
		cur = lvalue{ptr: s.scalar(x), T: pointerElem(xt)}
	} else if isAddressable(x, s.info) {
		cur = s.lv(x)
	} else {
		v := s.materialize(s.expr(x))
		if wantPtr && s.l.counted(xt) {
			// The method may change the temporary through the pointer, so
			// it must hold references of its own.
			v = s.holdOwned(v)
		}
		if !v.addr {
			if len(path) == 1 && !wantPtr {
				return v
			}
			tmp := s.temp(xt, "recv")
			s.storeTo(tmp, v, xt)
			cur = lvalue{ptr: tmp, T: xt}
		} else {
			cur = lvalue{ptr: v.v, T: xt}
		}
	}
	return s.receiverAt(cur, wantPtr, path)
}

// receiverAt follows path from the variable cur to the receiver of the
// method it ends in, and returns the receiver: its address if wantPtr,
// otherwise its value.
func (s *fnState) receiverAt(cur lvalue, wantPtr bool, path []int) value {
	if len(path) > 1 {
		cur = s.fieldPath(cur, path[:len(path)-1])
		if isPointerLike(unwrap(cur.T)) {
			cur = lvalue{ptr: s.loadLV(cur).v, T: pointerElem(unwrap(cur.T))}
		}
	}
	if wantPtr {
		return scalarValue(s.absAddr(cur.ptr), types2.NewPointer(cur.T))
	}
	return s.loadLV(cur)
}

// receiverFromValue returns the receiver of method fn, reached by path,
// from a value v of the type the method was selected on.
func (s *fnState) receiverFromValue(v value, fn *types2.Func, path []int) value {
	_, wantPtr := fn.Type().(*types2.Signature).Recv().Type().(*types2.Pointer)
	if len(path) == 1 && !wantPtr && !isPointerLike(v.T) {
		return v
	}
	var cur lvalue
	switch {
	case isPointerLike(v.T):
		cur = lvalue{ptr: v.v, T: pointerElem(v.T)}
	case s.l.counted(v.T):
		// A copy the method may change: it holds references of its own.
		cur = lvalue{ptr: s.holdOwned(v).v, T: v.T, stable: true}
	default:
		tmp := s.temp(v.T, "recv")
		s.storeTo(tmp, v, v.T)
		cur = lvalue{ptr: tmp, T: v.T}
	}
	return s.receiverAt(cur, wantPtr, path)
}

// methodExpr returns the function that the method expression e (R.m)
// denotes: it takes the receiver, of type R, as its first parameter. That is
// the method itself when R is its receiver type; otherwise a thunk reaches
// the method by dereferencing R, following embedded fields or calling
// through an interface. sig is the expression's type, instantiated when
// the method has type parameters of its own (R.m[int]).
func (s *fnState) methodExpr(e *ast.SelectorExpr, sel *types2.Selection, sig *types2.Signature) *ssa.Sym {
	R := s.subst(sel.Recv())
	fn := sel.Obj().(*types2.Func)
	path := sel.Index()
	if _, ok := types2.Unalias(sel.Recv()).(*types2.TypeParam); ok {
		obj, index, _ := types2.LookupFieldOrMethod(R, false, fn.Pkg(), fn.Name())
		m, ok := obj.(*types2.Func)
		if !ok {
			s.l.internalf(e.Sel.Pos(), "%s has no method %s", R, fn.Name())
			return nil
		}
		fn, path = m, index
	}
	if isIfaceMethod(fn) {
		return s.l.methodExprThunk(R, fn, path, nil, nil, sig)
	}
	w := s.ringOf(fn)
	var targs []types2.Type
	if w == nil {
		targs = s.recvTypeArgs(fn.Type().(*types2.Signature).Recv().Type())
		if len(targs) == 0 {
			targs = s.recvTypeArgs(R)
		}
		targs = s.withMethodTypeArgs(fn, e.Sel, targs)
	}
	target, full := s.l.methodFunc(fn, targs, w, e.Sel.Pos(), "used as a method expression")
	if len(path) == 1 && types2.Identical(full.Recv().Type(), R) {
		return target
	}
	return s.l.methodExprThunk(R, fn, path, target, full, sig)
}

// methodFunc returns the function implementing method fn instantiated with
// targs, and its signature with the receiver. w is fn's ring type if fn is
// a built-in ring method, or nil.
// pos and what describe the use for the error reported when fn is a ring
// iterator method, which can only be called.
func (l *Lowerer) methodFunc(fn *types2.Func, targs []types2.Type, w *types2.Wrapper, pos token.Pos, what string) (*ssa.Sym, *types2.Signature) {
	if w != nil {
		return l.ringMethodFunc(fn, w, pos, what)
	}
	return l.funcSym(fn, targs), l.methodSig(fn, targs)
}

// methodExprThunk synthesizes the function of a method expression R.m whose
// receiver type R differs from the method's: sig takes the R first. The
// thunk reaches method fn by path and calls target, whose signature is
// full, or calls fn through an interface if it is an interface method.
func (l *Lowerer) methodExprThunk(R types2.Type, fn *types2.Func, path []int, target *ssa.Sym, full, sig *types2.Signature) *ssa.Sym {
	name := typeString(R) + "." + fn.Name() + "·x"
	if target != nil {
		name = typeString(R) + "." + target.Name + "·x"
	}
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	l.gxWrapped[sym] = fn
	var rest []*types2.Var
	for i := 1; i < sig.Params().Len(); i++ {
		rest = append(rest, sig.Params().At(i))
	}
	plain := types2.NewSignatureType(nil, nil, nil, types2.NewTuple(rest...), sig.Results(), sig.Variadic())
	l.queue = append(l.queue, &funcJob{sym: sym, sig: sig, build: func(s *fnState) {
		in := s.incomingArgs(sig)
		recv := s.receiverFromValue(in[0], fn, path)
		if target == nil {
			s.returnValues(s.ifaceCall(recv, recv.T, fn, plain, in[1:], token.NoPos, fn.Name()))
			return
		}
		s.returnValues(s.emitCall(callee{sym: target}, full, &recv, in[1:], token.NoPos))
	}})
	return sym
}

// ----------------------------------------------------------------------------
// Function values and closures

// funcValue returns a func value for a declared function: the address of a
// static closure record holding its code pointer.
func (s *fnState) funcValue(fn *types2.Func, targs []types2.Type, pos token.Pos) *ssa.Value {
	s.l.errorsNewValue(fn, pos, "used as a func value")
	if fn.Pkg() != nil && fn.Pkg().Path() == "pluto/rt" && countIntrinsics[fn.Name()] {
		s.l.errorf(pos, diag.NotYetSupported, "rt.%s is expanded inline by the compiler and cannot be used as a func value", fn.Name())
	}
	sym := s.l.funcSym(fn, targs)
	s.checkNotHandler(sym, pos, "used as a func value")
	if s.l.isCFunc(sym) {
		// Func values use the internal ABI; a C function whose signature
		// needs translating is reached through a thunk.
		if cs, err := s.l.cSignature(fn.Type().(*types2.Signature)); err == nil && !cs.internal {
			sym = s.l.cThunk(fn, sym)
		}
	}
	sig := fn.Type().(*types2.Signature)
	if len(targs) > 0 {
		sig = newTypeMap(s.l, fn.Origin(), targs).subst(sig).(*types2.Signature)
	}
	s.l.funcValues[sym] = sig
	return s.symAddr(s.l.closureRecord(sym))
}

// closure lowers a function literal. A literal that captures nothing is a
// static closure record; otherwise the record is built in this frame:
// the code pointer followed by the address of each captured variable.
func (s *fnState) closure(lit *ast.FuncLit) *ssa.Value {
	captures := s.capturedVars(lit)
	return s.closureWith(lit, captures, s.closureFunc(lit, captures))
}

// closureWith builds the closure record of literal lit, whose function is
// sym and whose captured variables are captures.
func (s *fnState) closureWith(lit *ast.FuncLit, captures []*types2.Var, sym *ssa.Sym) *ssa.Value {
	if len(captures) == 0 {
		return s.symAddr(s.l.closureRecord(sym))
	}
	addrs := make([]*ssa.Value, len(captures))
	for i, v := range captures {
		addrs[i] = s.varLV(v, lit).ptr
	}
	if s.regionNodes[lit] {
		// Returned: built in the region the caller provides (regions.go).
		rec := s.regionRecord(int64(8*(1+len(addrs))), true)
		s.storeRecord(rec, sym, addrs)
		return rec
	}
	return s.closureRecord(sym, addrs)
}

// closureFunc queues the function of a literal with the given captures.
func (s *fnState) closureFunc(lit *ast.FuncLit, captures []*types2.Var) *ssa.Sym {
	s.closures++
	name := fmt.Sprintf("%s.func%d", s.f.Name, s.closures)
	sym := s.l.out.Sym(name, ssa.SymFunc)
	sig := s.subst(s.info.Types[lit].Type).(*types2.Signature)
	s.l.funcValues[sym] = sig
	s.l.queue = append(s.l.queue, &funcJob{
		sym: sym, pkg: s.pkg, lit: lit, sig: sig, tmap: s.tmap, captures: captures, parent: s,
	})
	return sym
}

// capturedVars lists the local variables of enclosing functions that lit
// refers to, in first-use order.
func (s *fnState) capturedVars(lit *ast.FuncLit) []*types2.Var {
	var out []*types2.Var
	seen := map[*types2.Var]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := s.info.Uses[id].(*types2.Var)
		if !ok || seen[v] || v.IsField() {
			return true
		}
		// Captured if declared outside the literal but inside a function.
		if v.Pkg() != nil && v.Parent() == v.Pkg().Scope() {
			return true
		}
		if v.Pos() >= lit.Pos() && v.Pos() < lit.End() {
			return true
		}
		if s.lookupVar(v) != nil {
			seen[v] = true
			out = append(out, v)
		}
		return true
	})
	return out
}

// methodValue lowers x.m used as a value: a closure record in this frame
// holding a wrapper's code pointer and a copy of the receiver.
func (s *fnState) methodValue(se *ast.SelectorExpr, sel *types2.Selection) *ssa.Value {
	recvT := s.typeOf(se.X)
	inRegion := s.regionNodes[se]
	if isInterface(recvT) {
		return s.ifaceMethodValue(s.expr(se.X), sel.Obj().(*types2.Func), inRegion)
	}
	fn, path, ok := s.selectedMethod(se, sel)
	if !ok {
		return s.constInt(ssa.TPtr, 0)
	}
	recv := s.methodReceiver(se.X, fn, path)
	if isIfaceMethod(fn) {
		// A method promoted from an embedded interface field.
		return s.ifaceMethodValue(recv, fn, inRegion)
	}
	var targs []types2.Type
	w := s.ringOf(fn)
	if w == nil {
		targs = s.methodTypeArgs(fn, se, recvT)
	}
	wrapper := s.l.boundMethodWrapper(s.l.methodFunc(fn, targs, w, se.Sel.Pos(), "used as a method value"))
	size := 8 + s.l.sizeof(recv.T)
	rec := s.methodValueRecord((size+7)&^7, inRegion)
	s.store(ssa.TPtr, rec, s.funcAddr(wrapper))
	if s.l.counted(recv.T) {
		if inRegion {
			// The region keeps the copy, released by its drop function
			// (regions.go); the caller dropped what it held before.
			s.regionCountedAt(8, recv.T)
			s.storeCounted(lvalue{ptr: s.offPtr(rec, 8), T: recv.T}, recv, true)
			return rec
		}
		// The record keeps a copy of the receiver until the function
		// returns (or for ever, in a package initialiser).
		if !s.staticLits {
			s.addFrameSlot(rec, 8, recv.T)
		}
		s.storeCounted(lvalue{ptr: s.offPtr(rec, 8), T: recv.T}, recv, s.staticLits)
		return rec
	}
	s.storeTo(s.offPtr(rec, 8), recv, recv.T)
	return rec
}

// methodValueRecord returns storage for the closure record of a method
// value: in this frame, or in static storage during package
// initialisation, whose values outlive the init function.
func (s *fnState) methodValueRecord(size int64, inRegion bool) *ssa.Value {
	if inRegion {
		// Returned: built in the region the caller provides (regions.go).
		return s.regionRecord(size, false)
	}
	if s.staticLits {
		return s.symAddr(s.l.staticBytes(size, 8))
	}
	return s.tempSize(size, 8, "methodvalue")
}

// ifaceMethodValue lowers the method value iv.fn of an interface value: a
// closure record holding a wrapper's code pointer and the interface value.
func (s *fnState) ifaceMethodValue(iv value, fn *types2.Func, inRegion bool) *ssa.Value {
	iv = s.materialize(iv)
	wrapper := s.l.ifaceMethodValueWrapper(iv.T, fn)
	rec := s.methodValueRecord(24, inRegion)
	s.store(ssa.TPtr, rec, s.funcAddr(wrapper))
	s.move(s.offPtr(rec, 8), iv.v, 16)
	return rec
}

func (s *fnState) funcAddr(sym *ssa.Sym) *ssa.Value {
	v := s.f.Entry.NewValue(ssa.OpFuncAddr, ssa.TPtr)
	v.Aux = sym
	s.hoistEntry(v)
	return v
}

// boundMethodWrapper synthesizes the function a method value's record
// points at: it loads the receiver from the record and calls the method.
func (l *Lowerer) boundMethodWrapper(target *ssa.Sym, full *types2.Signature) *ssa.Sym {
	name := target.Name + "-fm"
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	plain := types2.NewSignatureType(nil, nil, nil, full.Params(), full.Results(), full.Variadic())
	l.funcValues[sym] = plain
	recvT := full.Recv().Type()
	l.queue = append(l.queue, &funcJob{sym: sym, sig: plain, build: func(s *fnState) {
		ctx := s.newValue(ssa.OpClosurePtr, ssa.TPtr)
		recv := s.loadFrom(s.offPtr(ctx, 8), recvT)
		args := s.incomingArgs(plain)
		res := s.emitCall(callee{sym: target}, full, &recv, args, token.NoPos)
		s.returnValues(res)
	}})
	return sym
}

// incomingArgs rebuilds a synthesized function's parameters as values.
func (s *fnState) incomingArgs(sig *types2.Signature) []value {
	idx := 0
	if s.abi.sret {
		s.sret = s.newValue(ssa.OpArg, ssa.TPtr)
		idx++
	}
	if s.abi.region {
		s.regionArg(idx)
		idx++
	}
	var out []value
	for _, p := range s.abi.params {
		if p.byRef {
			a := s.newValue(ssa.OpArg, ssa.TPtr)
			a.AuxInt = int64(idx)
			idx++
			v := aggValue(a, p.typ)
			v.cnt = cntStable
			out = append(out, v)
			continue
		}
		tmp := s.temp(p.typ, "arg")
		for _, lf := range p.leaves {
			a := s.newValue(ssa.OpArg, lf.t)
			a.AuxInt = int64(idx)
			idx++
			s.store(lf.t, s.offPtr(tmp, lf.off), a)
		}
		v := s.loadFrom(tmp, p.typ)
		// Parameters are borrowed from the caller, who keeps them alive.
		v.cnt = cntStable
		out = append(out, v)
	}
	return out
}

// returnValues returns the given values from a synthesized function.
// Results are owned, so each takes a reference (strcount.go).
func (s *fnState) returnValues(res []value) {
	for i, p := range s.abi.results {
		addr := s.temp(p.typ, "result")
		s.assign(lvalue{ptr: addr, T: p.typ}, res[i], true)
		s.results = append(s.results, addr)
	}
	s.emitReturn(token.NoPos)
}

// lastingTemp returns storage for a T that the value being built keeps: a
// slot in this frame, or static storage during package initialisation,
// whose values outlive the init function, or, for the arguments of a defer
// statement in a range-over-func loop body, a slot in the frame that runs
// the deferred call (lastingFrame).
func (s *fnState) lastingTemp(T types2.Type, name string) *ssa.Value {
	if s.staticLits {
		return s.symAddr(s.l.staticObject(T))
	}
	if o := s.lastingFrame; o != nil && o != s {
		return s.outerPtr(o.temp(T, name), o)
	}
	return s.temp(T, name)
}
