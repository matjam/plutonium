package lower

import (
	"github.com/matjam/plutonium/internal/load"
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

	targets []*target // enclosing breakable statements, innermost last
	labels  map[*types2.Label]*labelInfo

	defers    []*deferInfo
	deferBits *ssa.Value // address of the defer bitmask slot, created on first defer
	loopDepth int
	closures  int // counter for naming closures

	// staticLits makes &T{...} allocate static storage: set while lowering
	// package initialisation, whose objects outlive the init function.
	staticLits bool

	// pos is the expression or statement being lowered, for errors
	// found below the AST (float operations in kernel mode).
	pos token.Pos
	// floatReported is set once a kernel-mode float error has been
	// reported for this function.
	floatReported bool
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
}

type labelInfo struct {
	block *ssa.Block // goto target
}

// A deferInfo is one defer statement: the call to run, with its function
// and arguments already evaluated into slots.
type deferInfo struct {
	run func(s *fnState) // emits the deferred call
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
	s := &fnState{
		l:      l,
		pkg:    job.pkg,
		f:      f,
		job:    job,
		tmap:   job.tmap,
		vars:   map[*types2.Var]*ssa.Value{},
		labels: map[*types2.Label]*labelInfo{},
	}
	if job.pkg != nil {
		s.info = job.pkg.Info
	}
	// In kernel mode only //pu:fpu functions, and the closures inside
	// them, may use the FPU (applyPragmas clears NoSSE for those).
	f.NoSSE = l.cfg.Kernel
	if job.parent != nil {
		f.NoSSE = job.parent.f.NoSSE
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
		f.Pos, f.End = job.decl.Pos(), job.decl.End()
		s.applyPragmas(job.decl.Doc)
	} else if job.lit != nil {
		f.Pos, f.End = job.lit.Pos(), job.lit.End()
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

	if job.build != nil {
		job.build(s)
	} else {
		s.bindParams()
		var body *ast.BlockStmt
		if job.decl != nil {
			body = job.decl.Body
		} else {
			body = job.lit.Body
		}
		if hasDefer(body) {
			s.deferBits = s.temp(types2.Typ[types2.Uint64], "deferbits")
			s.store(ssa.TI64, s.deferBits, s.constInt(ssa.TI64, 0))
		}
		s.stmtList(body.List)
		if s.b != nil {
			// Falling off the end: only valid for functions without results
			// (the checker rejects missing returns otherwise).
			s.emitReturn(body.Rbrace)
		}
	}

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
		case "nobounds":
			s.f.Pragmas |= ssa.PragmaNoBounds
		case "noreturn":
			s.f.Pragmas |= ssa.PragmaNoReturn
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
			if v != nil {
				s.vars[v] = ptr
			}
			continue
		}
		addr := s.temp(p.typ, varName(v))
		for _, lf := range p.leaves {
			s.store(lf.t, s.offPtr(addr, lf.off), nextArg(lf.t))
		}
		if v != nil {
			s.vars[v] = addr
		}
	}

	// Result variables, named or not, live in slots so that return
	// statements and deferred closures can update them.
	for i, p := range s.abi.results {
		addr := s.temp(p.typ, "result")
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

// emitReturn runs deferred calls and returns the result variables.
func (s *fnState) emitReturn(pos token.Pos) {
	if pos.IsValid() {
		s.f.SetPos(pos)
	}
	s.runDefers()
	ret := s.b
	ret.Kind = ssa.BlockRet
	ret.Pos = pos
	controls := []*ssa.Value{}
	if s.abi.sret {
		for i, p := range s.abi.results {
			off := s.resultOffset(i)
			s.move(s.offPtr(s.sret, off), s.results[i], s.l.sizeof(p.typ))
		}
		controls = append(controls, s.mem())
	} else {
		var leaves []*ssa.Value
		for i, p := range s.abi.results {
			for _, lf := range p.leaves {
				leaves = append(leaves, s.load(lf.t, s.offPtr(s.results[i], lf.off)))
			}
		}
		controls = append(controls, s.mem())
		controls = append(controls, leaves...)
	}
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

// runDefers emits the pending deferred calls, last first. A defer whose
// statement might not have run is guarded by its bit in the defer mask.
func (s *fnState) runDefers() {
	for i := len(s.defers) - 1; i >= 0; i-- {
		d := s.defers[i]
		bits := s.load(ssa.TI64, s.deferBits)
		bit := s.newValue(ssa.OpAnd, ssa.TI64, bits, s.constInt(ssa.TI64, 1<<i))
		cond := s.newValue(ssa.OpNe, ssa.TBool, bit, s.constInt(ssa.TI64, 0))
		call := s.newBlock(ssa.BlockInvalid)
		next := s.newBlock(ssa.BlockInvalid)
		s.endIf(cond, call, next)
		s.seal(call)
		s.startBlock(call)
		d.run(s)
		s.endPlain(next)
		s.seal(next)
		s.startBlock(next)
	}
}
