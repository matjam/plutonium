// Translated by go2pu from internal/opt/tailrec.go at the go-cutover tag; edited by hand since.

package opt

import (
	"compiler/compat"
	"compiler/internal/ssa"
)

// TailRec turns self-recursive calls in tail position into a loop.
//
// A return of f(args) jumps back to the top of the function with the
// parameters set to args. A return of f(args) op x, where op is an
// associative and commutative integer operation, does the same while
// folding x into an accumulator, and every other return then yields
// acc op result. So fib(n-1) + fib(n-2) keeps one call and loops on the
// other.
//
// The function's frame is reused, so functions with stack slots are left
// alone: a slot's address could be live in the frame being replaced.
func TailRec(f *ssa.Func) {
	if f.Sym == nil || len(f.Slots) > 0 || len(f.Results) > 1 || f.Pragmas&ssa.PragmaInterrupt != 0 {
		return
	}
	entry := f.Entry
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			if v.Op == ssa.OpClosurePtr {
				return
			}
		}
	}

	type site struct {
		b *ssa.Block
		tailSite
	}
	var sites []site
	accOp := ssa.OpInvalid
	var others []*ssa.Block
	for _, b := range f.Blocks {
		if b.Kind != ssa.BlockRet {
			continue
		}
		s, op, ok := matchTailCall(f, b)
		if !ok {
			others = compat.Append(others, b)
			continue
		}
		if op != ssa.OpInvalid {
			if accOp != ssa.OpInvalid && accOp != op {
				others = compat.Append(others, b)
				continue
			}
			accOp = op
		}
		sites = compat.Append(sites, site{b, s})
	}
	if len(sites) == 0 {
		return
	}

	// A new entry block takes over the arguments and initial memory; the
	// old entry becomes the loop header.
	// The backend expects the entry to be the first block.
	pre := f.NewBlock(ssa.BlockPlain)
	blocks := compat.Append(compat.Clone([]*ssa.Block{pre}), f.Blocks[:len(f.Blocks)-1]...)
	f.Blocks = blocks
	var params []*ssa.Value // indexed by argument number
	var initMem *ssa.Value
	keep := entry.Values[:0]
	for _, v := range entry.Values {
		switch v.Op {
		case ssa.OpArg:
			for int(v.AuxInt) >= len(params) {
				params = compat.Append(params, nil)
			}
			params[v.AuxInt] = v
		case ssa.OpInitMem:
			initMem = v
		default:
			keep = compat.Append(keep, v)
			continue
		}
		v.Block = pre
		pre.Values = compat.Append(pre.Values, v)
	}
	clear(entry.Values[len(keep):])
	entry.Values = keep
	f.Entry = pre
	pre.AddEdgeTo(entry)

	newPhi := func(t *ssa.Type, first *ssa.Value) *ssa.Value {
		var p *ssa.Value
		if len(entry.Values) > 0 {
			p = entry.NewValueBefore(entry.Values[0], ssa.OpPhi, t, first)
		} else {
			p = entry.NewValue(ssa.OpPhi, t, first)
		}
		return p
	}
	// Phis for every parameter and for memory; their other arguments come
	// from the tail sites in order.
	phiOf := map[*ssa.Value]*ssa.Value{}
	paramPhis := compat.Make[*ssa.Value](len(params))
	for i, a := range params {
		if a != nil {
			paramPhis[i] = newPhi(a.Type, a)
			phiOf[a] = paramPhis[i]
		}
	}
	var memPhi *ssa.Value
	if initMem != nil {
		memPhi = newPhi(ssa.TMem, initMem)
		phiOf[initMem] = memPhi
	}
	var accPhi *ssa.Value
	if accOp != ssa.OpInvalid {
		t := f.Results[0]
		accPhi = newPhi(t, f.ConstInt(t, identity(accOp)))
	}
	replaceUses(f, phiOf)

	for _, s := range sites {
		b, call := s.b, s.call
		args := compat.Append([]*ssa.Value(nil), call.Args...)
		var acc *ssa.Value
		if accPhi != nil {
			acc = accPhi
			if x := s.x; x != nil {
				if p, ok := phiOf[x]; ok {
					x = p // x is a parameter itself
				}
				acc = b.NewValue(accOp, accPhi.Type, accPhi, x)
			}
		}
		b.SetControl()
		b.Kind = ssa.BlockPlain
		b.AddEdgeTo(entry)
		for i, p := range paramPhis {
			if p != nil {
				p.AddArg(args[i])
			}
		}
		if memPhi != nil {
			memPhi.AddArg(args[len(args)-1])
		}
		if accPhi != nil {
			accPhi.AddArg(acc)
		}
		// The call and its results are now unused.
		for _, w := range b.Values {
			if w.Op == ssa.OpSelectN && w.Args[0] == call {
				w.Reset(ssa.OpUnreachableValue)
			}
		}
		call.Reset(ssa.OpUnreachableValue)
		call.Type = ssa.TVoid
	}
	if accPhi != nil {
		for _, b := range others {
			r := b.Controls[1]
			b.SetControl(b.Controls[0], b.NewValue(accOp, r.Type, accPhi, r))
		}
	}
	f.InvalidateCFG()
	ssa.DeadCode(f)
}

// tailSite is a matched return of a self-call.
type tailSite struct {
	call *ssa.Value
	x    *ssa.Value // accumulated operand, or nil for a plain tail call
}

// matchTailCall reports whether return block b returns a call of f itself,
// possibly combined with another value by an accumulating operation op.
func matchTailCall(f *ssa.Func, b *ssa.Block) (tailSite, ssa.Op, bool) {
	mem := b.Controls[0]
	if mem.Op != ssa.OpSelectN || mem.AuxInt != 0 || mem.Uses != 1 {
		return tailSite{}, 0, false
	}
	call := mem.Args[0]
	if call.Op != ssa.OpCall || call.Block != b || call.Aux != f.Sym || call.Call != nil && call.Call.NoReturn {
		return tailSite{}, 0, false
	}
	isResult := func(v *ssa.Value) bool {
		return v.Op == ssa.OpSelectN && v.AuxInt == 1 && v.Args[0] == call && v.Uses == 1
	}
	if len(b.Controls) == 1 {
		return tailSite{call: call}, ssa.OpInvalid, true
	}
	r := b.Controls[1]
	if isResult(r) {
		return tailSite{call: call}, ssa.OpInvalid, true
	}
	switch r.Op {
	case ssa.OpAdd, ssa.OpMul, ssa.OpAnd, ssa.OpOr, ssa.OpXor:
	default:
		return tailSite{}, 0, false
	}
	if r.Uses != 1 || !r.Type.IsInteger() {
		return tailSite{}, 0, false
	}
	switch {
	case isResult(r.Args[0]) && !isResult(r.Args[1]):
		return tailSite{call: call, x: r.Args[1]}, r.Op, true
	case isResult(r.Args[1]) && !isResult(r.Args[0]):
		return tailSite{call: call, x: r.Args[0]}, r.Op, true
	}
	return tailSite{}, 0, false
}

// identity returns the identity element of accumulating operation op.
func identity(op ssa.Op) int64 {
	switch op {
	case ssa.OpMul:
		return 1
	case ssa.OpAnd:
		return -1
	}
	return 0
}

// replaceUses rewrites every use of a key of m, in values and controls, to
// its value, except the uses by the replacement values themselves.
func replaceUses(f *ssa.Func, m map[*ssa.Value]*ssa.Value) {
	isNew := map[*ssa.Value]bool{}
	for _, n := range m {
		isNew[n] = true
	}
	for _, b := range f.Blocks {
		for _, v := range b.Values {
			if isNew[v] {
				continue
			}
			for i, a := range v.Args {
				if n, ok := m[a]; ok {
					v.SetArg(i, n)
				}
			}
		}
		for i, c := range b.Controls {
			if n, ok := m[c]; ok {
				cs := compat.Append([]*ssa.Value(nil), b.Controls...)
				cs[i] = n
				b.SetControl(cs...)
			}
		}
	}
}
