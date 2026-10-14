package lower

import (
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// fnState holds the state of lowering one function body.
type fnState struct {
	l    *Lowerer
	pkg  *load.Package
	info *types2.Info
	f    *ssa.Func
	job  *funcJob
	tmap *typeMap
	abi  *abiSig

	b *ssa.Block // current block; nil while lowering unreachable code

	blocks []*blockState // per block ID

	vars    map[*types2.Var]*ssa.Value // address of each local variable's storage
	sret    *ssa.Value                 // hidden result pointer, if any
	results []*ssa.Value               // addresses of the result variables
	ctx     *ssa.Value                 // closure record pointer, for closures

	// Closure regions (regions.go): the region the caller provides, its
	// layout, and what goes in it: the records of these literals and
	// method values, these variables, and the regions of these calls.
	regionPtr    *ssa.Value
	region       *regionLayout
	regionNodes  map[ast.Node]bool
	regionVars   map[*types2.Var]bool
	regionCalls  map[*ast.CallExpr]bool
	callAt       *ast.CallExpr // the call whose arguments were just lowered
	regionLocals []regionLocal // the regions this function reserved
	resultRegion []bool        // which result variables are in the region

	targets []*target // enclosing breakable statements, innermost last
	labels  map[*types2.Label]*labelInfo

	defers    []*deferInfo
	deferBits *ssa.Value // freestanding builds: address of the defer bitmask slot
	// Hosted builds: the address of the defer record (deferrec.go), and
	// how much of it is in use.
	deferRec     *ssa.Value
	deferRecSize int64
	// gxBoundary says what a call that ends with rt.Goexit does here, when
	// it does not just leave the function (goexit.go).
	gxBoundary      gxBoundary
	cleanupGoexit   *ssa.Value  // generated destruction completes before propagating Goexit
	implicitGoexit  *ssa.Value  // exits raised by completed implicit destruction
	implicitGoexits int         // implicit destruction calls emitted in this function
	goexitCleared   *types2.Var // a whole variable destroyed before an implicit exit
	loopDepth       int
	closures        int // counter for naming closures
	taskStmts       int // counter for naming task statement thunks

	// Range over functions (rangefunc.go). rf is set in the yield function
	// lowered from the body of a range-over-func loop; capPtrs maps the
	// addresses it reaches in the enclosing function to the pointers it
	// loads from its closure record, and initMem is its entry memory.
	rf      *rangeFunc
	capPtrs map[*ssa.Value]*ssa.Value
	initMem *ssa.Value
	// In a function containing range-over-func loops: the address of the
	// variable through which a loop body asks for return, break, continue
	// or goto out of the loop, and the codes handed out for those exits.
	rangeNext  *ssa.Value
	rangeCodes map[any]int64
	rangeLoops int // counter for naming loop bodies

	// staticLits makes &T{...} allocate static storage: set while lowering
	// package initialisation, whose objects outlive the init function.
	staticLits bool
	// lastingFrame, set while a defer statement's arguments are evaluated
	// in a range-over-func loop body, is the function whose frame holds
	// the storage they keep (interface boxes, literals, variadic arrays),
	// since the deferred call runs after the body has returned.
	lastingFrame *fnState

	// inTemp is the owner temporary being evaluated into its slot
	// (scope.go).
	inTemp ast.Expr
	// leaving holds, while scopes are released for a jump or the end of a
	// block, what the move checker knows there: the variables that hold
	// nothing, which are not destroyed, and those that hold values only in
	// some parts, of which only those are (scope.go). curStmt is the
	// innermost statement being lowered, whose moves.Info.AtStmt a call
	// ending with rt.Goexit uses.
	leaving moves.Known
	curStmt ast.Stmt

	// evaluated holds the results of the calls, receives and logical
	// operations evaluated ahead of the rest of their expression
	// (order.go), until the expression uses them.
	evaluated map[ast.Expr]evaluated

	// header is the init statement of an if or switch statement about to
	// be lowered, whose temporaries are the enclosing statement's.
	header ast.Stmt

	// pos is the expression or statement being lowered, for errors
	// found below the AST (float operations in kernel mode).
	pos token.Pos
	// floatReported is set once a kernel-mode float error has been
	// reported for this function.
	floatReported bool

	// String counting (strcount.go, strscope.go).
	cleanups    []*cleanupEntry
	frameSlots  []countedSlot
	ownedParams []countedSlot
	returns     []pendingReturn // returns whose ends wait for the frame slots
	entryZeros  []countedSlot
	entryEnd    *ssa.Block // the entry block, once the body has its own
	bodyBlock   *ssa.Block
	varFacts    map[*types2.Var]*varFacts
	ownedVars   map[*types2.Var]bool // variables that hold references of their own
	labelScopes map[*types2.Label]ast.Node
	// bytewise is set while rt.SliceBytewise is expanded: its slice is not
	// checked for rune boundaries.
	bytewise bool

	// Count elision (strelide.go): every call emitted, in order, and the
	// references held to keep borrowed strings alive across calls.
	callLog           []*ssa.Value
	strHolds          []*holdRec
	holdOf            map[*ownedTemp]*holdRec
	tempRelease       map[*ssa.Value]bool // releases of statement temporaries
	referenceReleases map[*ssa.Value]bool // exact counted-release operations
}

type blockState struct {
	mem        *ssa.Value // current memory definition at the end of the block so far
	sealed     bool
	incomplete *ssa.Value // memory phi waiting for predecessors
}

// A target is a statement that break or continue may leave.
type target struct {
	label    *types2.Label
	breakTo  *ssa.Block
	contTo   *ssa.Block // nil for switch
	isSwitch bool
	depth    int // cleanup entries below this one stay open on a break or continue
	// rf is set for the range-over-func loop whose body is being lowered:
	// break and continue return from the yield function instead.
	rf *rangeFunc
}

type labelInfo struct {
	block *ssa.Block // goto target
}

// A deferInfo is one defer statement: the call to run, with its function
// and arguments already evaluated into slots.
type deferInfo struct {
	// run emits the deferred call; rec is the address of the defer record
	// in the function emitting it (nil in freestanding builds).
	run       func(s *fnState, rec *ssa.Value)
	mayGoexit bool // the call may end with rt.Goexit
}

// ----------------------------------------------------------------------------
// Blocks and memory

func (s *fnState) newBlock(kind ssa.BlockKind) *ssa.Block {
	b := s.f.NewBlock(kind)
	for len(s.blocks) <= b.ID {
		s.blocks = append(s.blocks, &blockState{})
	}
	return b
}

func (s *fnState) bs(b *ssa.Block) *blockState { return s.blocks[b.ID] }

// startBlock makes b the current block.
func (s *fnState) startBlock(b *ssa.Block) { s.b = b }

// endPlain ends the current block with a jump to target.
func (s *fnState) endPlain(target *ssa.Block) {
	if s.b == nil {
		return
	}
	s.b.Kind = ssa.BlockPlain
	s.b.AddEdgeTo(target)
	s.b = nil
}

// endIf ends the current block with a branch on cond.
func (s *fnState) endIf(cond *ssa.Value, yes, no *ssa.Block) {
	if s.b == nil {
		return
	}
	s.b.Kind = ssa.BlockIf
	s.b.SetControl(cond)
	s.b.AddEdgeTo(yes)
	s.b.AddEdgeTo(no)
	s.b = nil
}

// mem returns the current memory state.
func (s *fnState) mem() *ssa.Value { return s.readMem(s.b) }

// setMem records a new memory state in the current block.
func (s *fnState) setMem(m *ssa.Value) { s.bs(s.b).mem = m }

func (s *fnState) readMem(b *ssa.Block) *ssa.Value {
	if m := s.bs(b).mem; m != nil {
		return m
	}
	return s.readMemRecursive(b)
}

func (s *fnState) readMemRecursive(b *ssa.Block) *ssa.Value {
	st := s.bs(b)
	var m *ssa.Value
	switch {
	case !st.sealed:
		m = s.newPhi(b, ssa.TMem)
		st.incomplete = m
	case len(b.Preds) == 0:
		// Only the entry has no predecessors and it defines memory first,
		// so this block is unreachable.
		m = b.NewValue(ssa.OpUnreachableValue, ssa.TMem)
	case len(b.Preds) == 1:
		m = s.readMem(b.Preds[0].B)
	default:
		m = s.newPhi(b, ssa.TMem)
		st.mem = m // break cycles through loops
		s.addPhiOperands(b, m)
	}
	st.mem = m
	return m
}

func (s *fnState) newPhi(b *ssa.Block, t *ssa.Type) *ssa.Value {
	v := b.NewValue(ssa.OpPhi, t)
	// Move the phi to the front of the block.
	vs := b.Values
	copy(vs[1:], vs[:len(vs)-1])
	vs[0] = v
	return v
}

func (s *fnState) addPhiOperands(b *ssa.Block, phi *ssa.Value) {
	for _, e := range b.Preds {
		phi.AddArg(s.readMem(e.B))
	}
}

// seal records that all of b's predecessors are known.
func (s *fnState) seal(b *ssa.Block) {
	st := s.bs(b)
	if st.sealed {
		return
	}
	st.sealed = true
	if phi := st.incomplete; phi != nil {
		st.incomplete = nil
		s.addPhiOperands(b, phi)
	}
}

// ----------------------------------------------------------------------------
// Values

func (s *fnState) newValue(op ssa.Op, t *ssa.Type, args ...*ssa.Value) *ssa.Value {
	if s.f.NoSSE {
		s.checkFloatOp(op, t, args)
	}
	return s.b.NewValue(op, t, args...)
}

func (s *fnState) constInt(t *ssa.Type, c int64) *ssa.Value { return s.f.ConstInt(t, c) }

func (s *fnState) constBool(c bool) *ssa.Value { return s.f.ConstBool(c) }

func (s *fnState) slotAddr(slot *ssa.Slot) *ssa.Value {
	v := s.f.Entry.NewValue(ssa.OpSlotAddr, ssa.TPtr)
	v.Aux = slot
	// Keep address values at the front of the entry block, ahead of any use.
	vs := s.f.Entry.Values
	i := 0
	for i < len(vs)-1 && vs[i].Op == ssa.OpPhi {
		i++
	}
	copy(vs[i+1:], vs[i:len(vs)-1])
	vs[i] = v
	return v
}

// temp returns the address of a new stack slot for a T.
func (s *fnState) temp(T types2.Type, name string) *ssa.Value {
	slot := s.f.NewSlot(name, s.l.sizeof(T), s.l.alignof(T))
	for _, lf := range s.l.leaves(T) {
		slot.Leaves = append(slot.Leaves, ssa.Leaf{Off: lf.off, Type: lf.t})
	}
	if slot.Leaves != nil {
		s.l.appendPads(&slot.Pads, T, 0)
	}
	return s.slotAddr(slot)
}

// tempSize returns the address of a new stack slot of the given size.
func (s *fnState) tempSize(size, align int64, name string) *ssa.Value {
	return s.slotAddr(s.f.NewSlot(name, size, align))
}

func (s *fnState) load(t *ssa.Type, ptr *ssa.Value) *ssa.Value {
	return s.newValue(ssa.OpLoad, t, ptr, s.mem())
}

// loadStatic loads from static data the compiler lays out (an itab or a
// type descriptor), which no generation check need guard.
func (s *fnState) loadStatic(t *ssa.Type, ptr *ssa.Value) *ssa.Value {
	v := s.load(t, ptr)
	v.AuxInt |= ssa.MemStatic
	return v
}

func (s *fnState) store(t *ssa.Type, ptr, val *ssa.Value) {
	st := s.newValue(ssa.OpStore, ssa.TMem, ptr, val, s.mem())
	st.Aux = t
	s.setMem(st)
}

func (s *fnState) offPtr(ptr *ssa.Value, off int64) *ssa.Value {
	if off == 0 {
		return ptr
	}
	v := s.newValue(ssa.OpOffPtr, ssa.TPtr, ptr)
	v.AuxInt = off
	return v
}

func (s *fnState) zero(ptr *ssa.Value, size int64) {
	if size == 0 {
		return
	}
	z := s.newValue(ssa.OpZero, ssa.TMem, ptr, s.mem())
	z.AuxInt = size
	s.setMem(z)
}

func (s *fnState) move(dst, src *ssa.Value, size int64) {
	if size == 0 {
		return
	}
	m := s.newValue(ssa.OpMove, ssa.TMem, dst, src, s.mem())
	m.AuxInt = size
	s.setMem(m)
}

// typeOf returns the concrete type of e.
func (s *fnState) typeOf(e ast.Expr) types2.Type {
	tv, ok := s.info.Types[e]
	if !ok {
		s.l.internalf(e.Pos(), "no type for expression")
		return types2.Typ[types2.Invalid]
	}
	return s.tmap.subst(tv.Type)
}

// subst applies the current function's type substitution.
func (s *fnState) subst(T types2.Type) types2.Type { return s.tmap.subst(T) }

// ----------------------------------------------------------------------------
// Function bodies

func (l *Lowerer) newFnState(job *funcJob) *fnState {
	f := ssa.NewFunc(job.sym.Name)
	f.Sym = job.sym
	pkgPath := ""
	if job.pkg != nil {
		pkgPath = job.pkg.Path
	}
	l.genPragmas(f, pkgPath)
	s := &fnState{
		l:         l,
		pkg:       job.pkg,
		f:         f,
		job:       job,
		tmap:      job.tmap,
		vars:      map[*types2.Var]*ssa.Value{},
		labels:    map[*types2.Label]*labelInfo{},
		ownedVars: map[*types2.Var]bool{},

		holdOf:      map[*ownedTemp]*holdRec{},
		tempRelease: map[*ssa.Value]bool{},

		regionNodes: map[ast.Node]bool{},
		regionVars:  map[*types2.Var]bool{},
		regionCalls: map[*ast.CallExpr]bool{},
	}
	if job.pkg != nil {
		s.info = job.pkg.Info
	}
	// In kernel mode only //pu:fpu functions, and the closures inside
	// them, may use the FPU (applyPragmas clears NoSSE for those).
	f.NoSSE = l.cfg.Kernel
	if job.parent != nil {
		f.NoSSE = job.parent.f.NoSSE
		// A function literal inside a //pu:nochecks function is part of its
		// body: the checks are off in it too (spec section 20.3, Directives).
		if job.parent.f.Pragmas&ssa.PragmaNoBounds != 0 {
			f.Pragmas |= ssa.PragmaNoBounds
			f.Pragmas &^= ssa.PragmaGenCheck
		}
	}
	for range f.Blocks {
		s.blocks = append(s.blocks, &blockState{})
	}
	s.abi = l.lowerSig(job.sig)
	f.Params = s.abi.flatParams
	f.Results = s.abi.flatResults
	f.ABI = ssa.ABIInternal
	return s
}

func (l *Lowerer) lowerFunc(job *funcJob) {
	s := l.newFnState(job)
	f := s.f
	entry := f.Entry
	s.bs(entry).sealed = true
	s.startBlock(entry)
	s.setMem(entry.NewValue(ssa.OpInitMem, ssa.TMem))

	if job.decl != nil {
		if fn := l.symFuncs[job.sym]; fn != nil && (fn == l.gx.catch || fn == l.gx.taskMain || fn == l.panicFn) {
			s.gxBoundary = gxCatch
		}
		f.Pos, f.End = job.decl.Pos(), job.decl.End()
		s.applyPragmas(job.decl.Doc)
		s.checkStackDirective(job.decl.Doc)
	} else if job.lit != nil {
		f.Pos, f.End = job.lit.Pos(), job.lit.End()
	} else {
		f.Pos, f.End = job.pos, job.end
	}
	if job.pkg != nil {
		f.Pkg = job.pkg.Path
	}
	// Values take the position of the code being lowered: the function's
	// declaration for its setup, then each statement and expression.
	f.SetPos(f.Pos)
	defer f.SetPos(token.NoPos)
	s.pos = f.Pos
	if f.NoSSE {
		s.checkFloatSignature()
	}

	l.sigs[job.sym] = job.sig
	if job.build != nil {
		if job.setup != nil {
			job.setup(s)
		}
		s.startBody()
		root := s.pushCleanup(nil, true)
		job.build(s)
		s.popCleanup(root)
	} else {
		var body *ast.BlockStmt
		if job.decl != nil {
			body = job.decl.Body
		} else {
			body = job.lit.Body
		}
		l.bodies[job.sym] = funcBody{body: body, info: s.info}
		s.varFacts = analyzeVars(body, s.info)
		s.labelScopes = labelBlocks(body, s.info)
		s.planClosures()
		s.bindParams()
		if hasDefer(body) {
			s.startDefers()
		}
		s.startBody()
		root := s.pushCleanup(body, true)
		s.stmtList(body.List)
		if s.b != nil {
			// Falling off the end: only valid for functions without results
			// (the checker rejects missing returns otherwise).
			s.leavingThrough(s.emptyAt(body), func() { s.emitReturn(body.Rbrace) })
		}
		s.popCleanup(root)
	}
	s.finishReturns()
	s.finishEntry()
	s.recordHolds()
	s.endRegion()
	s.finishDefers()

	for _, b := range f.Blocks {
		s.seal(b)
	}
	for _, b := range f.Blocks {
		if b.Kind == ssa.BlockInvalid {
			// A block never finished: unreachable code after a label
			// nobody jumps to, for example. Make it a dead exit.
			b.Kind = ssa.BlockExit
			s.b = b
			b.SetControl(s.readMem(b))
			s.b = nil
		}
	}
	f.RemoveDeadBlocks()
	f.Exports = l.exports[job.sym]
	l.out.Funcs = append(l.out.Funcs, f)
}

func (s *fnState) applyPragmas(doc *ast.CommentGroup) {
	for _, d := range directives(doc) {
		switch d {
		case "inline":
			s.f.Pragmas |= ssa.PragmaInline
		case "noinline":
			s.f.Pragmas |= ssa.PragmaNoInline
		case "nochecks":
			s.f.Pragmas |= ssa.PragmaNoBounds
			s.f.Pragmas &^= ssa.PragmaGenCheck
		case "interrupt":
			s.f.Pragmas |= ssa.PragmaInterrupt
		case "nosimd":
			s.f.Pragmas |= ssa.PragmaNoSIMD
		case "fpu":
			s.f.NoSSE = false
		}
	}
}

// bindParams gives every parameter, receiver and named result its storage.
func (s *fnState) bindParams() {
	argIdx := 0
	nextArg := func(t *ssa.Type) *ssa.Value {
		v := s.newValue(ssa.OpArg, t)
		v.AuxInt = int64(argIdx)
		argIdx++
		return v
	}
	if s.abi.sret {
		s.sret = nextArg(ssa.TPtr)
	}
	if s.abi.region {
		s.regionArg(argIdx)
		argIdx++
	}
	if s.job.lit != nil {
		s.ctx = s.newValue(ssa.OpClosurePtr, ssa.TPtr)
	}

	var paramVars []*types2.Var
	sig := s.signatureVars()
	if sig.recv != nil {
		paramVars = append(paramVars, sig.recv)
	}
	paramVars = append(paramVars, sig.params...)
	for i, p := range s.abi.params {
		v := paramVars[i]
		if p.byRef {
			ptr := nextArg(ssa.TPtr)
			if v != nil && s.regionVars[v] {
				// The caller's copy does not outlive the caller: the
				// region keeps a copy, and references, of its own.
				addr := s.regionVar(p.typ)
				s.move(addr, ptr, s.l.sizeof(p.typ))
				if s.l.counted(p.typ) {
					s.retainAt(addr, p.typ)
				}
				s.vars[v] = addr
				continue
			}
			if v != nil {
				s.vars[v] = ptr
				if s.l.counted(p.typ) && s.ownsParam(v) {
					// The caller's copy is borrowed; the function changes
					// it, so it takes references of its own, released when
					// it returns.
					s.retainAt(ptr, p.typ)
					s.ownedParams = append(s.ownedParams, countedSlot{base: ptr, T: p.typ})
				}
			}
			continue
		}
		if v != nil && s.regionVars[v] {
			// Captured by a closure the function returns: the region keeps
			// the parameter, and references of its own (regions.go).
			addr := s.regionVar(p.typ)
			for _, lf := range p.leaves {
				s.store(lf.t, s.offPtr(addr, lf.off), nextArg(lf.t))
			}
			if s.l.counted(p.typ) {
				s.retainAt(addr, p.typ)
			}
			s.vars[v] = addr
			continue
		}
		addr := s.temp(p.typ, varName(v))
		if v != nil && s.l.cfg.GenCheck && len(p.leaves) == 1 {
			if w, ok := unwrap(s.subst(v.Type())).Underlying().(*types2.Wrapper); ok && w.Kind() == types2.WrapOwned {
				// The function holds the box: nothing it does not pass
				// it to can free it (opt.GenCheck).
				s.f.GenOwnerArgs = append(s.f.GenOwnerArgs, int64(argIdx))
			}
		}
		for _, lf := range p.leaves {
			s.store(lf.t, s.offPtr(addr, lf.off), nextArg(lf.t))
		}
		if v != nil {
			s.vars[v] = addr
			if s.l.counted(p.typ) && s.ownsParam(v) {
				s.retainAt(addr, p.typ)
				s.ownedParams = append(s.ownedParams, countedSlot{base: addr, T: p.typ})
				s.ownedVars[v] = true
			}
		}
	}

	// Parameters that are owners belong to the function, which destroys
	// them when it returns; a Drop method's receiver is the value being
	// destroyed, whose fields the drop glue destroys after it.
	dropRecv := s.job.decl != nil && s.job.decl.Recv != nil && s.job.decl.Name.Name == "Drop"
	for i, v := range paramVars {
		if v == nil || (i == 0 && sig.recv != nil && dropRecv) {
			continue
		}
		if T := s.subst(v.Type()); s.l.needsDrop(T) {
			s.ownedParams = append(s.ownedParams, countedSlot{base: s.vars[v], T: T, pos: v.Pos(), v: v})
		}
	}

	// Result variables, named or not, live in slots so that return
	// statements and deferred closures can update them.
	s.resultRegion = make([]bool, len(s.abi.results))
	for i, p := range s.abi.results {
		var addr *ssa.Value
		if v := sig.results[i]; v != nil && s.regionVars[v] {
			addr = s.regionVar(p.typ)
			s.resultRegion[i] = true
		} else {
			addr = s.temp(p.typ, "result")
		}
		s.zero(addr, s.l.sizeof(p.typ))
		s.results = append(s.results, addr)
		if v := sig.results[i]; v != nil && v.Name() != "" && v.Name() != "_" {
			s.vars[v] = addr
		}
	}

	// Closures reach captured variables through the closure record:
	// word 0 is the code pointer, word i+1 the address of capture i.
	for i, v := range s.job.captures {
		s.vars[v] = s.load(ssa.TPtr, s.offPtr(s.ctx, int64(8*(i+1))))
	}
}

type sigVars struct {
	recv    *types2.Var
	params  []*types2.Var
	results []*types2.Var
}

// signatureVars returns the source-level variables of the function's
// parameters and results, which carry the names the body refers to.
func (s *fnState) signatureVars() sigVars {
	var sig *types2.Signature
	switch {
	case s.job.decl != nil:
		sig = s.info.Defs[s.job.decl.Name].Type().(*types2.Signature)
	case s.job.lit != nil:
		sig = s.info.Types[s.job.lit].Type.(*types2.Signature)
	}
	var out sigVars
	if sig == nil {
		return out
	}
	out.recv = sig.Recv()
	for i := 0; i < sig.Params().Len(); i++ {
		out.params = append(out.params, sig.Params().At(i))
	}
	for i := 0; i < sig.Results().Len(); i++ {
		out.results = append(out.results, sig.Results().At(i))
	}
	return out
}

func varName(v *types2.Var) string {
	if v == nil || v.Name() == "" {
		return "tmp"
	}
	return v.Name()
}

// A pendingReturn is the end of a return statement, built once the whole
// body is lowered (finishReturns).
type pendingReturn struct {
	b       *ssa.Block
	pos     token.Pos
	results []*ssa.Value // the result variables it returns
	gx      *ssa.Value   // its Goexit flag, if it may leave for Goexit
	leaving moves.Known  // what is known of the parameters there
}

// emitReturn runs deferred calls and returns the result variables.
func (s *fnState) emitReturn(pos token.Pos) { s.emitReturnFor(pos, false) }

// emitReturnFor is emitReturn; goexit says the function leaves because a
// call it made ended with rt.Goexit, which it passes on to its caller once
// its deferred calls have run and its variables are released (goexit.go).
func (s *fnState) emitReturnFor(pos token.Pos, goexit bool) {
	defer s.collectCleanupGoexit()()
	if pos.IsValid() {
		s.f.SetPos(pos)
	}
	var gx *ssa.Value
	if goexit || s.deferGoexits() {
		gx = s.temp(types2.Typ[types2.Bool], "goexit")
		s.store(ssa.TBool, gx, s.constBool(goexit))
	}
	if s.hasDefers() {
		s.runDeferList(s.defers, s.deferBitsAddr(), s.deferRec, gx)
	}
	// Deferred calls may use the function's variables, so they are
	// released only now: the variables in scope here, and then the frame
	// slots, retained parameters and regions of the whole function. A slot
	// lowered after this return can still be filled before it runs, in an
	// earlier iteration of a loop around both or after a goto back, so the
	// rest of the return waits until the body is lowered and every slot is
	// known.
	s.releaseScopes()
	if s.b == nil {
		return // a deferred call never returns
	}
	end := s.newBlock(ssa.BlockInvalid)
	s.endPlain(end)
	s.seal(end)
	s.returns = append(s.returns, pendingReturn{b: end, pos: pos, results: s.results, gx: gx, leaving: s.leaving})
}

// finishReturns ends every return statement: it releases what the
// function holds as a whole and returns the result variables.
func (s *fnState) finishReturns() {
	for _, r := range s.returns {
		s.startBlock(r.b)
		if r.pos.IsValid() {
			s.f.SetPos(r.pos)
		}
		s.leavingThrough(r.leaving, s.releaseFrame)
		s.finishReturn(r)
	}
	s.returns = nil
}

// finishReturn returns r's result variables from the current block.
func (s *fnState) finishReturn(r pendingReturn) {
	pos, gx, results := r.pos, r.gx, r.results
	if s.l.gx.active && s.implicitGoexits != 0 {
		if gx == nil {
			gx = s.temp(types2.Typ[types2.Bool], "cleanup.goexit")
			s.store(ssa.TBool, gx, s.constBool(false))
		}
		s.noteGoexit(gx)
		if s.implicitGoexit != nil {
			s.ifThen(s.load(ssa.TBool, s.implicitGoexit), func() { s.store(ssa.TBool, gx, s.constBool(true)) })
		}
	}
	if gx != nil {
		// An exiting caller never receives these results. Finish their
		// destruction here, including values installed by deferred calls.
		s.ifThen(s.load(ssa.TBool, gx), func() {
			saved := s.cleanupGoexit
			s.cleanupGoexit = gx
			defer func() { s.cleanupGoexit = saved }()
			for i := len(s.abi.results) - 1; i >= 0; i-- {
				T := s.abi.results[i].typ
				if i < len(s.resultRegion) && s.resultRegion[i] && s.l.counted(T) {
					continue // the caller's region still owns this reference
				}
				if s.l.needsDrop(T) || s.l.counted(T) {
					s.destroyAt(results[i], T)
					s.zero(results[i], s.l.sizeof(T))
				}
			}
		})
	}
	for i, inRegion := range s.resultRegion {
		// A result variable in the region keeps its references; the
		// caller gets references of its own.
		if T := s.abi.results[i].typ; inRegion && s.l.counted(T) {
			if gx == nil {
				s.retainAt(results[i], T)
			} else {
				s.ifThen(s.newValue(ssa.OpNot, ssa.TBool, s.load(ssa.TBool, gx)), func() {
					s.retainAt(results[i], T)
				})
			}
		}
	}
	if gx != nil {
		s.passGoexit(gx)
	}
	controls := []*ssa.Value{}
	if s.abi.sret {
		for i, p := range s.abi.results {
			off := s.resultOffset(i)
			s.move(s.offPtr(s.sret, off), results[i], s.l.sizeof(p.typ))
		}
		controls = append(controls, s.mem())
	} else {
		var leaves []*ssa.Value
		for i, p := range s.abi.results {
			for _, lf := range p.leaves {
				leaves = append(leaves, s.load(lf.t, s.offPtr(results[i], lf.off)))
			}
		}
		controls = append(controls, s.mem())
		controls = append(controls, leaves...)
	}
	// The copies above may have split the block (generation checks).
	ret := s.b
	ret.Kind = ssa.BlockRet
	ret.Pos = pos
	ret.SetControl(controls...)
	s.b = nil
}

// resultOffset returns the offset of result i in an sret buffer, which is
// laid out like a struct of the results.
func (s *fnState) resultOffset(i int) int64 {
	return s.l.resultOffsets(s.abi)[i]
}

// resultOffsets lays out an sret buffer.
func (l *Lowerer) resultOffsets(a *abiSig) []int64 {
	var offs []int64
	var off int64
	for _, p := range a.results {
		al := l.alignof(p.typ)
		off = (off + al - 1) &^ (al - 1)
		offs = append(offs, off)
		off += l.sizeof(p.typ)
	}
	return offs
}

// sretSize returns the size and alignment of an sret buffer.
func (l *Lowerer) sretSize(a *abiSig) (int64, int64) {
	var off, align int64 = 0, 1
	for _, p := range a.results {
		al := l.alignof(p.typ)
		align = max(align, al)
		off = (off+al-1)&^(al-1) + l.sizeof(p.typ)
	}
	return (off + align - 1) &^ (align - 1), align
}
