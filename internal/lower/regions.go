package lower

// Closure regions (language spec section 4.5, abi.md section 4.1).
//
// A function whose results can hold a func value, as a func, in an
// interface, or in a field or element of a struct or array result, takes a
// hidden region pointer after the result pointer. The caller provides the
// region, a block of memory in its own frame, and the function builds there
// the closures that may reach its results: their closure records, the
// variables they capture, and the regions of the calls whose closures it
// returns in turn. So a returned closure lives as long as its caller's
// frame, captures by reference as in Go, and costs no allocation.
//
// The size of a function's region is known only once every function is
// lowered: it holds the regions of the calls nested in it, and a call
// through a func value or an interface may reach any function of its
// signature. Lowering places the function's own variables and records at
// fixed offsets, and finishRegions then sizes the regions, places the
// nested ones after them, and sizes the slots callers reserved. A region
// that holds itself, through a recursive chain of calls, has no size and is
// an error (E3102).
//
// A region that holds anything starts with a drop word: the closure record
// of a function that releases the strings the region holds (its counted
// variables, and the receivers its method values copied) and drops the
// regions nested in it, or 0 when there is nothing to release. The function
// that fills the region sets it. The region belongs to the caller's frame
// (language spec section 6.10): the caller zeroes it on entry, drops it
// before each call refills it, and drops it when it returns.

import (
	"fmt"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// regionSig reports whether a function of signature sig takes a region:
// whether a result can hold a func value without a pointer in between.
func regionSig(sig *types2.Signature) bool {
	r := sig.Results()
	for i := 0; i < r.Len(); i++ {
		if holdsFunc(r.At(i).Type(), map[types2.Type]bool{}) {
			return true
		}
	}
	return false
}

func holdsFunc(T types2.Type, seen map[types2.Type]bool) bool {
	if seen[T] {
		return false
	}
	seen[T] = true
	switch t := types2.Unalias(T).Underlying().(type) {
	case *types2.Signature, *types2.Interface, *types2.TypeParam:
		return true
	case *types2.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if holdsFunc(t.Field(i).Type(), seen) {
				return true
			}
		}
	case *types2.Array:
		return holdsFunc(t.Elem(), seen)
	case *types2.Wrapper:
		return !t.IsPointer() && holdsFunc(t.Elem(), seen)
	case *types2.TaggedUnion:
		for i := 0; i < t.NumVariants(); i++ {
			if holdsFunc(t.Variant(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}

// A regionKey names the region a call needs: the callee's, for a call of a
// known function, or the largest of any function of the callee's
// signature, for a call through a func value or an interface.
type regionKey struct {
	sym  *ssa.Sym
	name string // a function's symbol name, for a key made before it is lowered
	sig  string
}

// A regionCand is an error to report if the region of key is not empty: a
// closure of a call kept where it cannot be (closureflow.go).
type regionCand struct {
	key regionKey
	pos token.Pos
	d   *diag.Diagnostic
}

// sigKey identifies the signature of sig, receiver aside: signatures
// identical but for parameter names have the same key.
func (l *Lowerer) sigKey(sig *types2.Signature) string {
	plain := types2.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
	bucket := fmt.Sprintf("%d/%d/%v", sig.Params().Len(), sig.Results().Len(), sig.Variadic())
	for i, other := range l.sigKeys[bucket] {
		if types2.Identical(plain, other) {
			return fmt.Sprintf("%s#%d", bucket, i)
		}
	}
	l.sigKeys[bucket] = append(l.sigKeys[bucket], plain)
	return fmt.Sprintf("%s#%d", bucket, len(l.sigKeys[bucket])-1)
}

// A regionLayout describes the region of one function.
type regionLayout struct {
	sym    *ssa.Sym
	parts  []ssa.Part // its own variables and records, at fixed offsets
	fixed  int64      // their end, after the drop word
	align  int64
	nested []*regionNested

	counted    []regionCounted // what its drop function releases
	dropStore  *ssa.Value      // the store of the drop word on entry
	dropSym    *ssa.Sym        // its drop function, if it needs one
	dropQueued bool
}

// A regionCounted is counted storage in a region: a variable, or a method
// value's copy of its receiver.
type regionCounted struct {
	off int64
	T   types2.Type
}

// A regionLocal is a region a function reserved in its frame, zeroed on
// entry (zero, whose size finishRegions sets) and dropped on return.
type regionLocal struct {
	slot *ssa.Slot
	key  regionKey
	zero *ssa.Value
}

// A regionStatic is a region in static storage, for a call made while
// initialising packages.
type regionStatic struct {
	data *ssa.Data
	key  regionKey
}

// A regionDrop is the drop of a region before a call refills it or when the
// function that reserved it returns. finishRegions sets cond, a constant,
// to false when the region holds nothing to release, and then points call,
// the call of a known function's drop function, at one that does nothing.
type regionDrop struct {
	cond *ssa.Value
	call *ssa.Value
	key  regionKey
}

// A regionNested is the region of a call that a function's region holds.
type regionNested struct {
	key  regionKey
	pos  token.Pos
	ptrs []*ssa.Value // OffPtrs whose AuxInt is its offset
	off  int64
}

// A regionSlot is a region a caller reserved in its frame.
type regionSlot struct {
	slot *ssa.Slot
	key  regionKey
	pos  token.Pos
}

// regionArg makes the region pointer argument of a function that takes
// one, the scalar after the result pointer.
func (s *fnState) regionArg(idx int) {
	v := s.newValue(ssa.OpArg, ssa.TPtr)
	v.AuxInt = int64(idx)
	s.regionPtr = v
	s.region = &regionLayout{sym: s.job.sym, align: 8, fixed: 8}
	s.l.regionLayouts[s.job.sym] = s.region
	// The drop word, set to the drop function by endRegion if the region
	// holds anything to release.
	s.region.dropStore = s.newValue(ssa.OpStore, ssa.TMem, v, s.constInt(ssa.TPtr, 0), s.mem())
	s.region.dropStore.Aux = ssa.TPtr
	s.setMem(s.region.dropStore)
	// A method is called indirectly through an interface, with the
	// receiver aside, and as a method expression, with the receiver first.
	sig := s.job.sig
	keys := []string{s.l.sigKey(sig)}
	if r := sig.Recv(); r != nil {
		ps := []*types2.Var{r}
		for i := 0; i < sig.Params().Len(); i++ {
			ps = append(ps, sig.Params().At(i))
		}
		keys = append(keys, s.l.sigKey(types2.NewSignatureType(nil, nil, nil, types2.NewTuple(ps...), sig.Results(), sig.Variadic())))
	}
	for _, key := range keys {
		s.l.regionMembers[key] = append(s.l.regionMembers[key], s.job.sym)
	}
}

// regionAlloc places p in this function's region and returns its address.
func (s *fnState) regionAlloc(p ssa.Part) *ssa.Value {
	rl := s.region
	p.Align = max(p.Align, 1)
	off := (rl.fixed + p.Align - 1) &^ (p.Align - 1)
	p.Off = off
	rl.fixed = off + p.Size
	rl.align = max(rl.align, p.Align)
	rl.parts = append(rl.parts, p)
	return s.offPtr(s.regionPtr, off)
}

// regionVar places a variable of type T in the region. The region holds
// its strings, released by the region's drop function.
func (s *fnState) regionVar(T types2.Type) *ssa.Value {
	p := ssa.Part{Size: s.l.sizeof(T), Align: s.l.alignof(T)}
	for _, lf := range s.l.leaves(T) {
		p.Leaves = append(p.Leaves, ssa.Leaf{Off: lf.off, Type: lf.t})
	}
	if p.Leaves != nil {
		s.l.appendPads(&p.Pads, T, 0)
	}
	addr := s.regionAlloc(p)
	if s.l.counted(T) {
		s.regionCountedAt(0, T)
	}
	return addr
}

// regionCountedAt records that the last part placed in the region holds a
// counted T at off.
func (s *fnState) regionCountedAt(off int64, T types2.Type) {
	rl := s.region
	rl.counted = append(rl.counted, regionCounted{off: rl.parts[len(rl.parts)-1].Off + off, T: T})
}

// endRegion sets the drop word of a function's region to its drop
// function, which finishRegions makes, if the region holds anything to
// release; otherwise the drop word stays 0, and is not even stored.
func (s *fnState) endRegion() {
	rl := s.region
	if rl == nil {
		return
	}
	if len(rl.counted) == 0 && len(rl.nested) == 0 {
		rl.dropStore.CopyOf(rl.dropStore.Args[2])
		return
	}
	rl.dropSym = s.l.dropSymFor(rl.sym)
	s.l.funcValues[rl.dropSym] = s.l.dropSig()
	rl.dropStore.SetArg(1, s.symAddr(s.l.closureRecord(rl.dropSym)))
}

// dropSig is the signature of a region's drop function.
func (l *Lowerer) dropSig() *types2.Signature {
	p := types2.NewVar(token.NoPos, nil, "region", types2.Typ[types2.UnsafePointer])
	return types2.NewSignatureType(nil, nil, nil, types2.NewTuple(p), nil, false)
}

// dropRegion drops the region at p, of key: calls the known function's
// drop function, or, for a call through a func value or an interface, the
// one its drop word holds, if any. It is all left out (finishRegions) when
// the region turns out to hold nothing to release.
func (s *fnState) dropRegion(p *ssa.Value, key regionKey) {
	if s.b == nil {
		return
	}
	d := regionDrop{cond: s.f.ConstBool(true), key: key}
	s.ifThen(d.cond, func() {
		// The drop function reads the whole region (OpRegion).
		whole := s.newValue(ssa.OpRegion, ssa.TPtr, p)
		if key.sym != nil {
			d.call = s.callValue(callee{sym: s.l.dropSymFor(key.sym)}, []*ssa.Value{whole}, nil)
			s.setMem(s.newValue(ssa.OpSelectN, ssa.TMem, d.call))
			return
		}
		s.dropWith(s.load(ssa.TPtr, p), whole)
	})
	s.l.regionDrops = append(s.l.regionDrops, d)
}

// dropSymFor returns the symbol of the drop function of sym's region.
func (l *Lowerer) dropSymFor(sym *ssa.Sym) *ssa.Sym {
	return l.out.Sym(sym.Name+"·rdrop", ssa.SymFunc)
}

// needsDrop reports whether the region key names may hold something to
// release, once regions are sized.
func (l *Lowerer) needsDrop(k regionKey) bool {
	if size, _ := l.keySize(k); size == 0 {
		return false
	}
	if rl := l.keyLayout(k); rl != nil {
		return rl.dropSym != nil
	}
	if k.sym != nil || k.name != "" {
		return false
	}
	for _, sym := range l.regionMembers[k.sig] {
		if l.needsDrop(regionKey{sym: sym}) {
			return true
		}
	}
	return false
}

// dropWith calls the drop function whose record is w, if w is not 0, on the
// region at p.
func (s *fnState) dropWith(w, p *ssa.Value) {
	s.ifThen(s.newValue(ssa.OpNe, ssa.TBool, w, s.constInt(ssa.TPtr, 0)), func() {
		code := s.load(ssa.TPtr, w)
		call := s.callValue(callee{code: code, ctx: w}, []*ssa.Value{p}, nil)
		s.setMem(s.newValue(ssa.OpSelectN, ssa.TMem, call))
	})
}

// zeroRegions zeroes, in the entry block, the regions this function
// reserved; finishRegions sets the sizes.
func (s *fnState) zeroRegions() {
	for i := range s.regionLocals {
		r := &s.regionLocals[i]
		r.zero = s.newValue(ssa.OpZero, ssa.TMem, s.slotAddr(r.slot), s.mem())
		r.zero.AuxInt = 8
		s.setMem(r.zero)
		s.l.regionZeros = append(s.l.regionZeros, *r)
	}
}

// dropLocalRegions drops the regions this function reserved, when it
// returns.
func (s *fnState) dropLocalRegions() {
	for _, r := range s.regionLocals {
		s.dropRegion(s.slotAddr(r.slot), r.key)
	}
}

// regionRecord places a closure record of size bytes in the region.
func (s *fnState) regionRecord(size int64, closure bool) *ssa.Value {
	p := ssa.Part{Size: size, Align: 8, Closure: closure}
	for off := int64(0); off < size; off += 8 {
		p.Leaves = append(p.Leaves, ssa.Leaf{Off: off, Type: ssa.TPtr})
	}
	return s.regionAlloc(p)
}

// callRegion returns the region to pass to a call of c, of signature sig:
// part of this function's region when the call's closures may reach its
// results, otherwise a slot in this frame.
func (s *fnState) callRegion(c callee, sig *types2.Signature, pos token.Pos) *ssa.Value {
	key := regionKey{sym: c.sym}
	if c.sym == nil {
		key = regionKey{sig: s.l.sigKey(sig)}
	}
	at := s.callAt
	s.callAt = nil
	if at != nil {
		// Only the call whose arguments were just lowered.
		if fs, ok := s.typeOf(at.Fun).Underlying().(*types2.Signature); !ok || s.l.sigKey(fs) != s.l.sigKey(sig) {
			at = nil
		}
	}
	var p *ssa.Value
	if s.regionPtr != nil && (at != nil && s.regionCalls[at] || at == nil && s.job.build != nil) {
		// A synthesized function (a wrapper) returns what it calls.
		n := &regionNested{key: key, pos: pos}
		p = s.newValue(ssa.OpOffPtr, ssa.TPtr, s.regionPtr)
		n.ptrs = append(n.ptrs, p)
		s.region.nested = append(s.region.nested, n)
	} else if s.staticLits {
		// Package initialisation: what it builds outlives it.
		sym := s.l.staticBytes(8, 8)
		s.l.regionStatics = append(s.l.regionStatics, regionStatic{data: s.l.out.Data[len(s.l.out.Data)-1], key: key})
		p = s.symAddr(sym)
	} else {
		slot := s.f.NewSlot("region", 0, 8)
		s.l.regionSlots = append(s.l.regionSlots, regionSlot{slot: slot, key: key, pos: pos})
		s.regionLocals = append(s.regionLocals, regionLocal{slot: slot, key: key})
		p = s.slotAddr(slot)
	}
	// The call refills the region: what it held is released first.
	s.dropRegion(p, key)
	return s.newValue(ssa.OpRegion, ssa.TPtr, p)
}

// finishRegions sizes every region once all functions are lowered, and
// queues the regions' drop functions. It reports whether it queued any; it
// runs again once they are lowered, and whatever else is.
func (l *Lowerer) finishRegions() bool {
	l.sizeRegions()
	for _, rl := range l.regionLayouts {
		off := rl.fixed
		if size, _ := l.keySize(regionKey{sym: rl.sym}); size == 0 {
			// An empty region: its calls' regions are empty too.
			for _, n := range rl.nested {
				for _, p := range n.ptrs {
					p.AuxInt = 0
				}
			}
			if st := rl.dropStore; st.Op == ssa.OpStore {
				// Nothing to drop after all, and no drop word to set.
				st.CopyOf(st.Args[2])
			}
			continue
		}
		for _, n := range rl.nested {
			size, align := l.keySize(n.key)
			off = (off + align - 1) &^ (align - 1)
			n.off = off
			for _, p := range n.ptrs {
				p.AuxInt = off
			}
			off += size
		}
	}
	for _, cands := range l.regionPending {
		for _, c := range cands {
			if size, _ := l.keySize(c.key); size > 0 {
				l.report(c.pos, c.d)
				break
			}
		}
	}
	l.regionPending = nil
	for _, rs := range l.regionSlots {
		size, align := l.keySize(rs.key)
		rs.slot.Size, rs.slot.Align = size, align
		if rl := l.keyLayout(rs.key); rl != nil {
			rs.slot.Parts = l.regionParts(rl, map[*regionLayout]bool{})
		}
	}
	for _, rs := range l.regionStatics {
		size, align := l.keySize(rs.key)
		rs.data.Size, rs.data.Align = max(size, 1), align
	}
	for _, z := range l.regionZeros {
		if size, _ := l.keySize(z.key); size > 0 {
			z.zero.AuxInt = size
		} else if z.zero.Op == ssa.OpZero {
			z.zero.CopyOf(z.zero.Args[1])
		}
	}
	l.regionZeros = nil
	for _, d := range l.regionDrops {
		if l.needsDrop(d.key) {
			continue
		}
		d.cond.AuxInt = 0
		if d.call != nil {
			d.call.Aux = l.noDrop()
		}
	}
	l.regionDrops = nil
	queued := false
	for _, rl := range l.sortedLayouts() {
		if rl.dropSym != nil && !rl.dropQueued {
			rl.dropQueued = true
			l.queueDrop(rl)
			queued = true
		}
	}
	return queued
}

// noDrop returns a drop function that does nothing, which the calls of the
// drop functions of regions that turned out to need none call, if they run
// at all.
func (l *Lowerer) noDrop() *ssa.Sym {
	sym := l.out.Sym("pluto.noRegionDrop", ssa.SymFunc)
	if l.funcs[sym.Name] == nil {
		l.funcs[sym.Name] = sym
		sig := l.dropSig()
		l.queue = append(l.queue, &funcJob{sym: sym, sig: sig, build: func(s *fnState) {
			s.incomingArgs(sig)
			s.returnValues(nil)
		}})
	}
	return sym
}

// queueDrop queues the drop function of rl's region: it releases the
// counted storage, drops the nested regions and clears the drop word.
func (l *Lowerer) queueDrop(rl *regionLayout) {
	sig := l.dropSig()
	l.queue = append(l.queue, &funcJob{sym: rl.dropSym, sig: sig, build: func(s *fnState) {
		p := s.incomingArgs(sig)[0].v
		if size, _ := l.keySize(regionKey{sym: rl.sym}); size == 0 {
			// Never called: the region turned out empty.
			s.returnValues(nil)
			return
		}
		for _, c := range rl.counted {
			s.releaseAt(s.offPtr(p, c.off), c.T)
			s.zero(s.offPtr(p, c.off), s.l.sizeof(c.T))
		}
		for _, n := range rl.nested {
			if !l.needsDrop(n.key) {
				continue
			}
			q := s.newValue(ssa.OpRegion, ssa.TPtr, s.offPtr(p, n.off))
			if k := n.key; k.sym != nil {
				call := s.callValue(callee{sym: l.dropSymFor(k.sym)}, []*ssa.Value{q}, nil)
				s.setMem(s.newValue(ssa.OpSelectN, ssa.TMem, call))
				continue
			}
			s.dropWith(s.load(ssa.TPtr, s.offPtr(p, n.off)), q)
		}
		s.store(ssa.TPtr, p, s.constInt(ssa.TPtr, 0))
		s.returnValues(nil)
	}})
}

// A regionNode is a region to size: a function's, or the largest of a
// signature's (a class).
type regionNode struct {
	rl    *regionLayout
	class string

	index, low   int
	onStack, set bool
	size, align  int64
}

// keyLayout returns the layout of the function k names, if any.
func (l *Lowerer) keyLayout(k regionKey) *regionLayout {
	if k.name != "" {
		k.sym = l.funcs[k.name]
	}
	if k.sym == nil {
		return nil
	}
	return l.regionLayouts[k.sym]
}

// sizeRegions sizes the regions, the strongly connected components of the
// graph of regions and the regions they hold first. A region that holds
// itself, through a cycle, has size 0 if nothing on the cycle builds
// anything and no fixed size otherwise, which is an error (E3102).
func (l *Lowerer) sizeRegions() {
	l.regionNodes = map[any]*regionNode{}
	node := func(k regionKey) *regionNode {
		var id any
		if rl := l.keyLayout(k); rl != nil {
			id = rl
		} else if k.sym == nil && k.name == "" && len(l.regionMembers[k.sig]) > 0 {
			id = k.sig
		} else {
			return nil
		}
		n := l.regionNodes[id]
		if n == nil {
			n = &regionNode{index: -1, align: 8}
			if rl, ok := id.(*regionLayout); ok {
				n.rl = rl
			} else {
				n.class = id.(string)
			}
			l.regionNodes[id] = n
		}
		return n
	}
	succ := func(n *regionNode) []*regionNode {
		var out []*regionNode
		if n.rl != nil {
			for _, nd := range n.rl.nested {
				if m := node(nd.key); m != nil {
					out = append(out, m)
				}
			}
			return out
		}
		for _, sym := range l.regionMembers[n.class] {
			out = append(out, node(regionKey{sym: sym}))
		}
		return out
	}
	index := 0
	var stack []*regionNode
	var visit func(n *regionNode)
	visit = func(n *regionNode) {
		n.index, n.low = index, index
		index++
		stack = append(stack, n)
		n.onStack = true
		for _, m := range succ(n) {
			if m.index < 0 {
				visit(m)
				n.low = min(n.low, m.low)
			} else if m.onStack {
				n.low = min(n.low, m.index)
			}
		}
		if n.low != n.index {
			return
		}
		var scc []*regionNode
		for {
			m := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			m.onStack = false
			scc = append(scc, m)
			if m == n {
				break
			}
		}
		l.sizeSCC(scc, succ)
	}
	for _, rl := range l.sortedLayouts() {
		if n := l.regionNodes[rl]; n == nil || n.index < 0 {
			visit(node(regionKey{sym: rl.sym}))
		}
	}
	// The classes only callers' slots and pending errors name.
	var keys []regionKey
	for _, rs := range l.regionSlots {
		keys = append(keys, rs.key)
	}
	for _, cands := range l.regionPending {
		for _, c := range cands {
			keys = append(keys, c.key)
		}
	}
	for _, k := range keys {
		if n := node(k); n != nil && n.index < 0 {
			visit(n)
		}
	}
}

// sizeSCC sizes the regions of one strongly connected component, whose
// successors outside it are sized.
func (l *Lowerer) sizeSCC(scc []*regionNode, succ func(*regionNode) []*regionNode) {
	in := map[*regionNode]bool{}
	for _, n := range scc {
		in[n] = true
	}
	cyclic := len(scc) > 1
	for _, m := range succ(scc[0]) {
		cyclic = cyclic || m == scc[0]
	}
	// own is n's size counting only regions outside the component.
	own := func(n *regionNode) (int64, int64) {
		if n.rl == nil {
			var size, align int64 = 0, 8
			for _, m := range succ(n) {
				if !in[m] {
					size, align = max(size, m.size), max(align, m.align)
				}
			}
			return size, align
		}
		rl := n.rl
		size, align := rl.fixed, rl.align
		for _, nd := range rl.nested {
			m := l.nodeOf(nd.key)
			if m == nil || in[m] {
				continue
			}
			size = (size+m.align-1)&^(m.align-1) + m.size
			align = max(align, m.align)
		}
		if len(rl.parts) == 0 && size == rl.fixed {
			return 0, 8 // nothing but the drop word, which it does not need
		}
		return (size + align - 1) &^ (align - 1), align
	}
	if !cyclic {
		n := scc[0]
		n.size, n.align = own(n)
		n.set = true
		return
	}
	var bad *regionNode
	for _, n := range scc {
		n.size, n.align = 0, 8
		if s, a := own(n); s > 0 {
			bad = n
			n.align = a
		}
	}
	for _, n := range scc {
		n.set = true
	}
	if bad == nil {
		return
	}
	// Report the first call, in source order, by which a function of the
	// component holds the component's regions.
	var at *regionNested
	var from *regionLayout
	for _, n := range scc {
		if n.rl == nil {
			continue
		}
		for _, nd := range n.rl.nested {
			if m := l.nodeOf(nd.key); m != nil && in[m] && (at == nil || nd.pos < at.pos) {
				at, from = nd, n.rl
			}
		}
	}
	if at != nil {
		l.regionCycle(from, at)
	}
}

// nodeOf returns the node sizeRegions made for k, or nil.
func (l *Lowerer) nodeOf(k regionKey) *regionNode {
	if rl := l.keyLayout(k); rl != nil {
		return l.regionNodes[rl]
	}
	if k.sym == nil && k.name == "" {
		return l.regionNodes[k.sig]
	}
	return nil
}

// keySize returns the size and alignment of the region k names.
func (l *Lowerer) keySize(k regionKey) (int64, int64) {
	if n := l.nodeOf(k); n != nil && n.set {
		return n.size, n.align
	}
	return 0, 8
}

// sortedLayouts returns the layouts in a fixed order, so that errors come
// out the same on every run.
func (l *Lowerer) sortedLayouts() []*regionLayout {
	var out []*regionLayout
	for _, rl := range l.regionLayouts {
		out = append(out, rl)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].sym.Name < out[j-1].sym.Name; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// regionCycle reports the region of rl holding itself through n.
func (l *Lowerer) regionCycle(rl *regionLayout, n *regionNested) {
	if l.regionCycles[n.pos] {
		return
	}
	l.regionCycles[n.pos] = true
	name := rl.sym.Name
	if fn := l.symFuncs[rl.sym]; fn != nil {
		name = fn.Name()
	}
	l.report(n.pos, &diag.Diagnostic{
		Code: diag.ClosureRegionCycle,
		Msg:  fmt.Sprintf("%s cannot return the closures of this call: the call reaches %s again, so its region would have no fixed size", name, name),
		Text: fmt.Sprintf("A function that returns a closure builds it in a region its caller provides, and the region holds the regions of the calls whose closures it returns in turn (spec section 4.5). %s returns closures that hold those of this call, and the call can reach %s again, directly, through other functions, or through a func value or an interface, so its region would have to hold itself.\nBuild the closures in a loop instead of by recursion, or keep their state in a value the caller owns, such as a struct with a method.", name, name),
	})
}

// regionParts returns the parts of rl's region, with the nested regions'
// own parts where they are known.
func (l *Lowerer) regionParts(rl *regionLayout, seen map[*regionLayout]bool) []ssa.Part {
	if seen[rl] {
		return nil
	}
	seen[rl] = true
	defer delete(seen, rl)
	if size, _ := l.keySize(regionKey{sym: rl.sym}); size == 0 {
		return nil
	}
	parts := append([]ssa.Part{{Off: 0, Size: 8, Align: 8, Leaves: []ssa.Leaf{{Off: 0, Type: ssa.TPtr}}}}, rl.parts...)
	for _, n := range rl.nested {
		size, align := l.keySize(n.key)
		p := ssa.Part{Off: n.off, Size: size, Align: align, Region: true}
		if n.key.sym != nil {
			if nl := l.regionLayouts[n.key.sym]; nl != nil {
				p.Parts = l.regionParts(nl, seen)
			}
		}
		parts = append(parts, p)
	}
	return parts
}

// ownFrame reports whether p, in the body of the function f
// analyses, runs in that function's own frame: not in a function literal
// nested in it or in the body of a range-over-func loop, which is a
// function of its own.
func (f *closureFlow) ownFrame(p token.Pos) bool {
	for _, r := range f.foreign {
		// Strictly inside: a literal itself, or a call of it, is made
		// where it is written.
		if p > r.Pos() && p < r.End() {
			return false
		}
	}
	return true
}

// findForeign lists the nested function literals and range-over-func loop
// bodies of the body.
func (f *closureFlow) findForeign() {
	ast.Inspect(f.body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			f.foreign = append(f.foreign, n)
			return false
		case *ast.RangeStmt:
			if _, ok := underlying(f.typeOf(n.X)).(*types2.Signature); ok {
				f.foreign = append(f.foreign, n.Body)
			}
		}
		return true
	})
}
