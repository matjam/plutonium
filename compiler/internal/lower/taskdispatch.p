package lower

import (
	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/sort"
)

// Task statements that call through an interface method or a func value.
//
// A task's counted arguments belong to its parameters (language spec
// sections 12.12 and 18.7). A task statement that names its function
// calls the function's task entry (taskEntry), which owns them. A task
// statement that calls through an interface or a func value does not know
// the function, so its thunk calls a dispatcher shared by all such
// statements of the same interface method or signature. The dispatcher
// owns the counted arguments the thunk passes. It compares the code
// pointer with each function that the program puts in an itab slot of the
// interface, or takes as a func value of the signature, and that has a
// task variant, and calls the variant on a match: the function's task
// entry, or a wrapper that calls it. Otherwise it calls the code it was
// given, which borrows the arguments, and releases them when the call
// returns.
//
// The candidates are known only when the whole program is lowered, so the
// dispatchers are built last (finishTaskDispatch). Only the functions that
// change a counted parameter get a variant, and only when a matching task
// statement exists.

// A taskVariant says how to build the task variant of a function that a
// dispatcher may reach.
type taskVariant struct {
	kind  taskVariantKind
	fn    *types2.Func  // the declared function, or the method a wrapper calls
	targs []types2.Type // fn's type arguments

	// tvLit: the literal and the function that lowered it.
	parent   *fnState
	lit      *ast.FuncLit
	captures []*types2.Var

	// tvWrap: where the wrapper finds the receiver, and what it calls.
	recv  recvSource
	sig   *types2.Signature // the wrapper's signature
	full  *types2.Signature // fn's signature with the receiver
	recvT types2.Type       // the receiver's type: fn's, or the interface's
	T     types2.Type       // recvData: the concrete type
	index []int             // recvData: the path to the method in T
	path  []int             // recvArg: the path from the first argument
	iface bool              // fn is an interface method: call its dispatcher
	plain *types2.Signature // iface: fn's signature without the receiver

	built bool
	entry *ssa.Sym // the variant, or nil
}

type taskVariantKind int

const (
	tvFunc taskVariantKind = iota // a declared function or method: its taskEntry
	tvLit                         // a function literal, lowered again as a task entry
	tvWrap                        // a wrapper that finds a receiver and calls a method
)

// recvSource says where a wrapper finds its receiver.
type recvSource int

const (
	recvData recvSource = iota // an itab adapter: the interface's data word
	recvCtx                    // a method value's wrapper: its closure record
	recvArg                    // a method expression's thunk: its first argument
)

// A taskDispatch is the dispatcher of the task statements that call one
// interface method, or call func values of one signature.
type taskDispatch struct {
	sym    *ssa.Sym
	I      types2.Type       // the interface, or nil for func values
	idx    int               // the method's index in I
	fn     *types2.Func      // the method
	sig    *types2.Signature // the method's or the func value's signature
	dsig   *types2.Signature // the dispatcher's: code, context or data word, then sig's parameters
	cands  []taskCandidate
	queued bool
}

// A taskCandidate is a function a dispatcher recognizes and its variant.
type taskCandidate struct {
	orig, entry *ssa.Sym
}

// noteTaskVariant records how to build the task variant of sym.
func (l *Lowerer) noteTaskVariant(sym *ssa.Sym, v *taskVariant) {
	if sym != nil && l.taskVariants[sym] == nil {
		l.taskVariants[sym] = v
	}
}

// noteTaskFunc records that sym is the code of fn instantiated with targs.
func (l *Lowerer) noteTaskFunc(sym *ssa.Sym, fn *types2.Func, targs []types2.Type) {
	l.noteTaskVariant(sym, pu.New(taskVariant{kind: tvFunc, fn: fn, targs: targs}))
}

// ifaceMethodIndex returns the index of method fn in interface I, or -1.
func ifaceMethodIndex(I types2.Type, fn *types2.Func) int {
	iface := I.Underlying().(*types2.Interface)
	for i := 0; i < iface.NumMethods(); i++ {
		if iface.Method(i).Name() == fn.Name() && iface.Method(i).Pkg() == fn.Pkg() || iface.Method(i) == fn {
			return i
		}
	}
	return -1
}

// ifaceDispatcher returns the dispatcher of method fn of interface I,
// whose signature is sig.
func (l *Lowerer) ifaceDispatcher(I types2.Type, idx int, fn *types2.Func, sig *types2.Signature) *taskDispatch {
	for _, d := range l.taskDispatches {
		if d.I != nil && d.idx == idx && types2.Identical(d.I, I) {
			return d
		}
	}
	return l.newDispatch(I, idx, fn, sig, compat.Concat(typeString(I), ".", fn.Name(), "·dispatch"))
}

// funcDispatcher returns the dispatcher of func values of signature sig.
func (l *Lowerer) funcDispatcher(sig *types2.Signature) *taskDispatch {
	for _, d := range l.taskDispatches {
		if d.I == nil && types2.Identical(d.sig, sig) {
			return d
		}
	}
	return l.newDispatch(nil, 0, nil, sig, compat.Sprintfb("func·dispatch%d", compat.ArgI(int64(len(l.taskDispatches)))))
}

func (l *Lowerer) newDispatch(I types2.Type, idx int, fn *types2.Func, sig *types2.Signature, name string) *taskDispatch {
	unsafePtr := types2.Typ[types2.UnsafePointer]
	params := compat.Clone([]*types2.Var{types2.NewParam(token.NoPos, nil, "code", unsafePtr), types2.NewParam(token.NoPos, nil, "ctx", unsafePtr)})
	for i := 0; i < sig.Params().Len(); i++ {
		params = compat.Append(params, sig.Params().At(i))
	}
	dsig := types2.NewSignatureType(nil, nil, nil, types2.NewTuple(params...), sig.Results(), false)
	d := pu.New(taskDispatch{sym: l.out.Sym(name, ssa.SymFunc), I: I, idx: idx, fn: fn, sig: sig, dsig: dsig})
	l.taskDispatches = compat.Append(l.taskDispatches, d)
	return d
}

// site describes a call the dispatcher makes, or a call of it, as the
// call the task statement wrote, for the checks that look at calls.
func (d *taskDispatch) site() *callSite {
	if d.I != nil {
		return pu.New(callSite{desc: d.fn.Name(), iface: d.I, method: d.idx})
	}
	return pu.New(callSite{fsig: d.sig})
}

// callDispatcher calls d with the code pointer, the context or data word
// and the arguments, whose counted references d takes over.
func (s *fnState) callDispatcher(d *taskDispatch, code, ctx *ssa.Value, args []value) []value {
	unsafePtr := types2.Typ[types2.UnsafePointer]
	all := compat.Clone([]value{scalarValue(code, unsafePtr), scalarValue(ctx, unsafePtr)})
	all = compat.Append(all, args...)
	return s.emitCall(callee{code: s.funcAddr(d.sym), ctx: s.constInt(ssa.TPtr, 0), site: d.site()}, d.dsig, nil, all, token.NoPos)
}

// ifaceTaskCall calls method fn on interface value iv through the
// method's dispatcher, which takes over the counted arguments.
func (s *fnState) ifaceTaskCall(iv value, I types2.Type, fn *types2.Func, sig *types2.Signature, args []value) []value {
	idx := ifaceMethodIndex(I, fn)
	if idx < 0 {
		s.l.internalf(token.NoPos, "method %s not found in %s", pu.Box(fn.Name()), I)
		return nil
	}
	d := s.l.ifaceDispatcher(I, idx, fn, sig)
	tab, data := s.ifaceParts(iv)
	s.callNilCheck(tab, token.NoPos)
	code := s.loadStatic(ssa.TPtr, s.offPtr(tab, int64(itabHeader+8*idx)))
	return s.callDispatcher(d, code, data, args)
}

// funcTaskCall calls func value f, of signature sig, through the
// signature's dispatcher, which takes over the counted arguments.
func (s *fnState) funcTaskCall(f *ssa.Value, sig *types2.Signature, args []value) []value {
	s.callNilCheck(f, token.NoPos)
	return s.callDispatcher(s.l.funcDispatcher(sig), s.load(ssa.TPtr, f), f, args)
}

// finishTaskDispatch builds the dispatchers once lowering has found every
// candidate. It reports whether it queued new work.
func (l *Lowerer) finishTaskDispatch() bool {
	before := len(l.queue)
	// The variants first: lowering them can add candidates and dispatchers.
	for i := 0; i < len(l.taskDispatches); i++ {
		l.taskCandidates(l.taskDispatches[i])
	}
	if len(l.queue) > before {
		return true
	}
	for _, d := range l.taskDispatches {
		if d.queued {
			continue
		}
		d.queued = true
		d.cands = l.taskCandidates(d)
		l.queue = compat.Append(l.queue, pu.New(funcJob{sym: d.sym, sig: d.dsig, build: pu.New(dispatchBody{d: d})}))
	}
	return len(l.queue) > before
}

// taskCandidates lists the functions d recognizes, building their
// variants: the adapters in the slot of d's method of every itab of d's
// interface, or the func values of d's signature.
func (l *Lowerer) taskCandidates(d *taskDispatch) []taskCandidate {
	var origs []*ssa.Sym
	if d.I != nil {
		var keys []string
		for k, it := range l.types.itabs {
			if d.idx < len(it.methods) && types2.Identical(it.I, d.I) {
				keys = compat.Append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			origs = compat.Append(origs, l.types.itabs[k].methods[d.idx])
		}
	} else {
		for sym, sig := range l.funcValues {
			if l.taskVariants[sym] != nil && types2.Identical(plainSigOf(plainSig(sig)), d.sig) {
				origs = compat.Append(origs, sym)
			}
		}
		sort.SortFunc(origs, func(a, b *ssa.Sym) bool { return a.Name < b.Name })
	}
	var out []taskCandidate
	for _, orig := range origs {
		if entry := l.taskVariantOf(orig); entry != nil {
			out = compat.Append(out, taskCandidate{orig: orig, entry: entry})
		}
	}
	return out
}

// taskVariantOf returns the task variant of sym, building it the first
// time, or nil when sym has none.
func (l *Lowerer) taskVariantOf(sym *ssa.Sym) *ssa.Sym {
	v := l.taskVariants[sym]
	if v == nil {
		return nil
	}
	if !v.built {
		v.built = true
		v.entry = l.buildTaskVariant(sym, v)
	}
	return v.entry
}

func (l *Lowerer) buildTaskVariant(sym *ssa.Sym, v *taskVariant) *ssa.Sym {
	switch v.kind {
	case tvFunc:
		return l.taskEntry(v.fn, v.targs)
	case tvLit:
		if !v.parent.litOwnsParam(v.lit) {
			return nil
		}
		return v.parent.closureJob(v.lit, v.captures, true)
	}
	var target *ssa.Sym
	if !v.iface {
		if target = l.taskEntry(v.fn, v.targs); target == nil {
			return nil
		}
	}
	name := compat.Concat(sym.Name, "·task")
	entry := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = entry
	l.queue = compat.Append(l.queue, pu.New(funcJob{sym: entry, sig: v.sig, build: pu.New(variantBody{v: v, target: target})}))
	return entry
}

// litOwnsParam reports whether literal lit, lowered in s, changes a
// counted parameter, so that a task calling it needs a task entry.
func (s *fnState) litOwnsParam(lit *ast.FuncLit) bool {
	sig := s.info.Types[lit].Type.(*types2.Signature)
	if regionSig(s.subst(sig).(*types2.Signature)) {
		return false
	}
	var params []*types2.Var
	for i := 0; i < sig.Params().Len(); i++ {
		params = compat.Append(params, sig.Params().At(i))
	}
	return s.l.ownsCountedParam(analyzeVars(lit.Body, s.info), params, s.tmap)
}

// ownsCountedParam reports whether one of params, whose types tm
// instantiates, is counted and assigned, has its address taken or is
// captured (facts), so that the function owns it.
func (l *Lowerer) ownsCountedParam(facts map[*types2.Var]*varFacts, params []*types2.Var, tm *typeMap) bool {
	for _, v := range params {
		T := v.Type()
		if tm != nil {
			T = tm.subst(T)
		}
		if f := facts[v]; f != nil && (f.assigned || f.addrTaken || f.captured) && l.counted(T) {
			return true
		}
	}
	return false
}

// dropFirstParam returns sig without its first parameter: a method
// expression's signature as the method's, without the receiver.
func dropFirstParam(sig *types2.Signature) *types2.Signature {
	var rest []*types2.Var
	for i := 1; i < sig.Params().Len(); i++ {
		rest = compat.Append(rest, sig.Params().At(i))
	}
	return types2.NewSignatureType(nil, nil, nil, types2.NewTuple(rest...), sig.Results(), sig.Variadic())
}

// dispatchBody builds a dispatcher.
type dispatchBody struct {
	d *taskDispatch
}

func (b *dispatchBody) Call(s *fnState) {
	d := b.d
	in := s.incomingArgs(d.dsig)
	code, ctx := in[0].v, in[1].v
	args := in[2:]
	sig := d.sig
	var recv *value
	if d.I != nil {
		// An itab adapter takes the data word as its receiver.
		sig = adapterSignature(d.sig)
		recv = pu.New(in[1])
		ctx = s.constInt(ssa.TPtr, 0)
	}
	for _, c := range d.cands {
		hit := s.newValue(ssa.OpEq, ssa.TBool, code, s.funcAddr(c.orig))
		entry := c.entry
		s.ifThen(hit, func() {
			s.returnValues(s.emitCall(callee{code: s.funcAddr(entry), ctx: ctx, site: d.site()}, sig, recv, args, token.NoPos))
		})
	}
	// A function without a task variant borrows the arguments: this
	// function keeps their references until it returns.
	for _, a := range args {
		if s.l.counted(a.T) {
			s.storeTo(s.frameSlot(a.T, "arg"), a, a.T)
		}
	}
	s.returnValues(s.emitCall(callee{code: code, ctx: ctx, site: d.site()}, sig, recv, args, token.NoPos))
}

// variantBody builds the task variant of a wrapper: it finds the receiver
// as the wrapper does and calls the method's task entry, target, or the
// interface method's dispatcher. Its counted arguments are the callee's.
type variantBody struct {
	v      *taskVariant
	target *ssa.Sym
}

func (b *variantBody) Call(s *fnState) {
	v := b.v
	var ctx *ssa.Value
	if v.recv == recvCtx {
		ctx = s.newValue(ssa.OpClosurePtr, ssa.TPtr)
	}
	in := s.incomingArgs(v.sig)
	args := in
	var recv value
	switch v.recv {
	case recvData:
		recv = s.receiverFromData(in[0].v, v.T, v.index, v.recvT)
		args = in[1:]
	case recvCtx:
		if v.iface {
			recv = aggValue(s.offPtr(ctx, 8), v.recvT)
		} else {
			recv = s.loadFrom(s.offPtr(ctx, 8), v.recvT)
		}
	case recvArg:
		// The first argument's references are this function's; the
		// receiver is found in it, and they are released when it returns.
		if s.l.counted(in[0].T) {
			s.storeTo(s.frameSlot(in[0].T, "recv"), in[0], in[0].T)
		}
		recv = s.receiverFromValue(in[0], v.fn, v.path)
		args = in[1:]
	}
	if v.iface {
		s.returnValues(s.ifaceTaskCall(recv, recv.T, v.fn, v.plain, args))
		return
	}
	if s.l.counted(recv.T) {
		// The task entry owns its receiver too: give it a reference.
		slot := s.temp(recv.T, "recv")
		s.storeTo(slot, recv, recv.T)
		s.retainAt(slot, recv.T)
		recv = s.loadFrom(slot, recv.T)
	}
	s.returnValues(s.emitCall(callee{sym: b.target}, v.full, &recv, args, token.NoPos))
}
