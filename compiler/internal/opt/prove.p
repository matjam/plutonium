// Translated by go2pu from internal/opt/prove.go at the go-cutover tag; edited by hand since.

package opt

import (
	"compiler/compat"
	"compiler/internal/ssa"
	"pluto/bits"
	"pluto/math"
)

// Prove folds conditional branches whose outcome follows from the branches
// that dominate them and from the ranges values can take. Its main target
// is bounds checks: in
//
//	for i := 0; i < len(s); i++ { s[i] }
//
// the check i <u len(s) is implied by i < len(s) and i >= 0, the second of
// which holds because i is an induction variable that starts at 0, only
// grows, and cannot overflow while the loop condition holds.
//
// Only 64-bit integer comparisons are considered, and the comparison of a
// pointer's bits with zero that lowering emits as a nil check: a pointer
// is not nil after an access at a small offset from it has completed,
// since that access would have faulted (language spec section 17.5).
// SimplifyCFG removes the folded branches.
func Prove(f *ssa.Func) {
	p := &prover{
		f:      f,
		dom:    f.Dom(),
		ranges: map[*ssa.Value]rng{},
		busy:   map[*ssa.Value]bool{},
	}
	p.walk(f.Entry)
	ssa.CopyElim(f)
}

// rng is an inclusive range of signed 64-bit values.
type rng struct{ lo, hi int64 }

var fullRange = rng{math.MinInt64, math.MaxInt64}

func (r rng) union(s rng) rng { return rng{min(r.lo, s.lo), max(r.hi, s.hi)} }

func (r rng) nonneg() bool { return r.lo >= 0 }

// rel is the relation a fact asserts between its two values.
type rel uint8

const (
	relLt  rel = iota // x < y, signed
	relLe             // x <= y, signed
	relLtu            // x < y, unsigned
	relLeu            // x <= y, unsigned
	relEq
	relNe
)

type fact struct {
	x, y *ssa.Value
	r    rel
}

type prover struct {
	f      *ssa.Func
	dom    *ssa.DomTree
	ranges map[*ssa.Value]rng // static ranges, independent of facts
	busy   map[*ssa.Value]bool
	facts  []fact
	nonNil []*ssa.Value // pointers known not to be nil
}

// nilGuardSize is the size of the unmapped region at address zero: an
// access at a smaller offset from a nil pointer faults (lower.nilGuardSize).
const nilGuardSize = 4096

// walk visits the dominator tree below b, carrying the facts that hold on
// entry to each block.
func (p *prover) walk(b *ssa.Block) {
	n, nn := len(p.facts), len(p.nonNil)
	if len(b.Preds) == 1 {
		e := b.Preds[0]
		if pb := e.B; pb.Kind == ssa.BlockIf && pb.Succs[0].B != pb.Succs[1].B {
			p.assume(pb.Controls[0], e.I == 0)
		}
	}
	for _, v := range b.Values {
		p.rewrite(v)
	}
	// Every access in b completes before b's branch and before the blocks
	// b dominates run.
	for _, v := range b.Values {
		p.accessed(v)
	}
	if b.Kind == ssa.BlockIf && b.Controls[0].Op != ssa.OpConst {
		if val, ok := p.decide(b.Controls[0]); ok {
			b.SetControl(p.f.ConstBool(val))
		}
	}
	for _, c := range p.dom.Children(b) {
		p.walk(c)
	}
	p.facts = p.facts[:n]
	p.nonNil = p.nonNil[:nn]
}

// accessed records the pointers that memory access v shows not to be nil:
// the base of an address at an offset below nilGuardSize.
func (p *prover) accessed(v *ssa.Value) {
	var addrs []*ssa.Value
	switch v.Op {
	case ssa.OpLoad, ssa.OpStore:
		addrs = v.Args[:1]
	case ssa.OpZero:
		if v.AuxInt > 0 {
			addrs = v.Args[:1]
		}
	case ssa.OpMove:
		if v.AuxInt > 0 {
			addrs = v.Args[:2]
		}
	}
	for _, a := range addrs {
		off := int64(0)
		for a.Op == ssa.OpOffPtr && off >= 0 && off < nilGuardSize {
			off += a.AuxInt
			a = a.Args[0]
		}
		if off >= 0 && off < nilGuardSize {
			p.nonNil = compat.Append(p.nonNil, a)
		}
	}
}

// decideNil decides a nil check, x != 0 or x == 0 for the bits x of a
// pointer known not to be nil.
func (p *prover) decideNil(c *ssa.Value) (val, ok bool) {
	if (c.Op != ssa.OpEq && c.Op != ssa.OpNe) || len(c.Args) != 2 {
		return false, false
	}
	x, y := c.Args[0], c.Args[1]
	if x.Op == ssa.OpConst {
		x, y = y, x
	}
	if y.Op != ssa.OpConst || y.AuxInt != 0 || x.Op != ssa.OpBitcast || !x.Args[0].Type.IsPtr() {
		return false, false
	}
	for _, q := range p.nonNil {
		if q == x.Args[0] {
			return c.Op == ssa.OpNe, true
		}
	}
	return false, false
}

// rewrite simplifies v using what is known where it is defined: a value
// with a single possible value becomes that constant, and a signed division
// by 2^k whose dividend is non-negative or has its low k bits clear drops
// its rounding fix-up.
func (p *prover) rewrite(v *ssa.Value) {
	if !word(v) || v.Op == ssa.OpConst {
		return
	}
	if r := p.rangeOf(v); r.lo == r.hi {
		if v.Op == ssa.OpPhi {
			// Phis stay at the head of their block; copyelim removes this.
			v.CopyOf(p.f.ConstInt(v.Type, r.lo))
			return
		}
		v.Reset(ssa.OpConst)
		v.AuxInt = r.lo
		return
	}
	// x/2^k expands to (x + (x>>63 >>> (64-k))) >> k.
	if v.Op != ssa.OpShrA || v.Args[0].Op != ssa.OpAdd || v.Args[1].Op != ssa.OpConst {
		return
	}
	k := v.Args[1].AuxInt
	add := v.Args[0]
	for i, bias := range add.Args {
		x := add.Args[1-i]
		if bias.Op != ssa.OpShrL || bias.Args[1].Op != ssa.OpConst || bias.Args[1].AuxInt != 64-k {
			continue
		}
		sign := bias.Args[0]
		if sign.Op != ssa.OpShrA || sign.Args[0] != x || sign.Args[1].Op != ssa.OpConst || sign.Args[1].AuxInt != 63 {
			continue
		}
		if p.rangeOf(x).nonneg() || p.lowBitsClear(x, k) {
			v.SetArg(0, x)
			return
		}
	}
}

// lowBitsClear reports whether the facts say x&m == 0 for an m covering
// the low k bits.
func (p *prover) lowBitsClear(x *ssa.Value, k int64) bool {
	if k <= 0 || k >= 64 {
		return false
	}
	low := int64(1)<<k - 1
	for _, ft := range p.facts {
		if ft.r != relEq {
			continue
		}
		and, zero := ft.x, ft.y
		if and.Op == ssa.OpConst {
			and, zero = zero, and
		}
		if zero.Op != ssa.OpConst || zero.AuxInt != 0 || and.Op != ssa.OpAnd {
			continue
		}
		for i, m := range and.Args {
			if and.Args[1-i] == x && m.Op == ssa.OpConst && m.AuxInt&low == low {
				return true
			}
		}
	}
	return false
}

// word reports whether v is a 64-bit integer, the only kind Prove reasons
// about.
func word(v *ssa.Value) bool { return v.Type.Kind == ssa.KInt && v.Type.Size == 8 }

// condFact returns the fact that comparison c having the given outcome
// establishes.
func condFact(c *ssa.Value, outcome bool) (fact, bool) {
	for c.Op == ssa.OpNot {
		c = c.Args[0]
		outcome = !outcome
	}
	if len(c.Args) != 2 || !word(c.Args[0]) || !word(c.Args[1]) {
		return fact{}, false
	}
	x, y := c.Args[0], c.Args[1]
	switch c.Op {
	case ssa.OpLess:
		if outcome {
			return fact{x, y, relLt}, true
		}
		return fact{y, x, relLe}, true
	case ssa.OpLeq:
		if outcome {
			return fact{x, y, relLe}, true
		}
		return fact{y, x, relLt}, true
	case ssa.OpLessu:
		if outcome {
			return fact{x, y, relLtu}, true
		}
		return fact{y, x, relLeu}, true
	case ssa.OpLequ:
		if outcome {
			return fact{x, y, relLeu}, true
		}
		return fact{y, x, relLtu}, true
	case ssa.OpEq:
		if outcome {
			return fact{x, y, relEq}, true
		}
		return fact{x, y, relNe}, true
	case ssa.OpNe:
		if outcome {
			return fact{x, y, relNe}, true
		}
		return fact{x, y, relEq}, true
	}
	return fact{}, false
}

func (p *prover) assume(c *ssa.Value, outcome bool) {
	if ft, ok := condFact(c, outcome); ok {
		p.facts = compat.Append(p.facts, ft)
	}
}

// refine narrows r, the range of v, by fact ft.
func (p *prover) refine(r rng, v *ssa.Value, ft fact) rng {
	switch v {
	case ft.x:
		y := p.staticRange(ft.y)
		switch ft.r {
		case relLt:
			if y.hi == math.MinInt64 {
				return r // impossible fact; leave it alone
			}
			r.hi = min(r.hi, y.hi-1)
		case relLe:
			r.hi = min(r.hi, y.hi)
		case relLtu:
			// x <u y with 0 <= y <= y.hi puts x in [0, y.hi-1].
			if y.nonneg() && y.hi > 0 {
				r.lo = max(r.lo, 0)
				r.hi = min(r.hi, y.hi-1)
			}
		case relLeu:
			if y.nonneg() {
				r.lo = max(r.lo, 0)
				r.hi = min(r.hi, y.hi)
			}
		case relEq:
			r.lo, r.hi = max(r.lo, y.lo), min(r.hi, y.hi)
		case relNe:
			if y.lo == y.hi {
				if r.lo == y.lo && r.lo < math.MaxInt64 {
					r.lo++
				} else if r.hi == y.lo && r.hi > math.MinInt64 {
					r.hi--
				}
			}
		}
	case ft.y:
		x := p.staticRange(ft.x)
		switch ft.r {
		case relLt:
			if x.lo < math.MaxInt64 {
				r.lo = max(r.lo, x.lo+1)
			}
		case relLe:
			r.lo = max(r.lo, x.lo)
		case relEq:
			r.lo, r.hi = max(r.lo, x.lo), min(r.hi, x.hi)
		case relNe:
			return p.refine(r, v, fact{ft.y, ft.x, relNe})
		}
	}
	return r
}

// rangeOf returns the range of v given the current facts.
func (p *prover) rangeOf(v *ssa.Value) rng {
	r := p.staticRange(v)
	for _, ft := range p.facts {
		if ft.x == v || ft.y == v {
			r = p.refine(r, v, ft)
		}
	}
	return r
}

// has reports whether the current facts include x r y.
func (p *prover) has(x, y *ssa.Value, r rel) bool {
	for _, ft := range p.facts {
		if ft.x == x && ft.y == y && ft.r == r {
			return true
		}
		if ft.r == r && (r == relEq || r == relNe) && ft.x == y && ft.y == x {
			return true
		}
	}
	return false
}

// decide works out the value of comparison c, if the facts and ranges fix it.
func (p *prover) decide(c *ssa.Value) (val, ok bool) {
	if c.Op == ssa.OpNot {
		val, ok = p.decide(c.Args[0])
		return !val, ok
	}
	if val, ok = p.decideNil(c); ok {
		return val, ok
	}
	if len(c.Args) != 2 || !word(c.Args[0]) || !word(c.Args[1]) {
		return false, false
	}
	x, y := c.Args[0], c.Args[1]
	switch c.Op {
	case ssa.OpLess:
		return p.less(x, y, false)
	case ssa.OpLeq:
		// x <= y is !(y < x).
		val, ok = p.less(y, x, false)
		return !val, ok
	case ssa.OpLessu:
		return p.less(x, y, true)
	case ssa.OpLequ:
		val, ok = p.less(y, x, true)
		return !val, ok
	case ssa.OpEq:
		return p.equal(x, y)
	case ssa.OpNe:
		val, ok = p.equal(x, y)
		return !val, ok
	}
	return false, false
}

// less decides x < y (unsigned if u).
func (p *prover) less(x, y *ssa.Value, u bool) (val, ok bool) {
	if x == y {
		return false, true
	}
	rx, ry := p.rangeOf(x), p.rangeOf(y)
	// Unsigned order is signed order when both are non-negative.
	signed := !u || rx.nonneg() && ry.nonneg()
	if signed {
		if rx.hi < ry.lo {
			return true, true
		}
		if rx.lo >= ry.hi {
			return false, true
		}
	}
	switch {
	case p.has(x, y, relEq):
		return false, true
	case !u && p.has(x, y, relLt), u && p.has(x, y, relLtu):
		return true, true
	case !u && (p.has(y, x, relLe) || p.has(y, x, relLt)),
		u && (p.has(y, x, relLeu) || p.has(y, x, relLtu)):
		return false, true
	}
	if u && rx.nonneg() {
		// 0 <= x < y (signed) makes y positive, so x <u y too.
		if p.has(x, y, relLt) {
			return true, true
		}
		if ry.nonneg() && (p.has(y, x, relLe) || p.has(y, x, relLt)) {
			return false, true
		}
	}
	return false, false
}

// equal decides x == y.
func (p *prover) equal(x, y *ssa.Value) (val, ok bool) {
	if x == y || p.has(x, y, relEq) {
		return true, true
	}
	if p.has(x, y, relNe) || p.has(x, y, relLt) || p.has(y, x, relLt) {
		return false, true
	}
	rx, ry := p.rangeOf(x), p.rangeOf(y)
	if rx.hi < ry.lo || ry.hi < rx.lo {
		return false, true
	}
	if rx.lo == rx.hi && ry.lo == ry.hi {
		return rx.lo == ry.lo, true
	}
	return false, false
}

// staticRange returns the range of v that holds wherever v is defined.
func (p *prover) staticRange(v *ssa.Value) rng {
	if !word(v) {
		return fullRange
	}
	if r, ok := p.ranges[v]; ok {
		return r
	}
	if p.busy[v] {
		return fullRange
	}
	p.busy[v] = true
	r := p.computeRange(v)
	delete(p.busy, v)
	p.ranges[v] = r
	return r
}

func (p *prover) computeRange(v *ssa.Value) rng {
	arg := func(i int) rng { return p.staticRange(v.Args[i]) }
	switch v.Op {
	case ssa.OpConst:
		return rng{v.AuxInt, v.AuxInt}
	case ssa.OpZeroExt:
		a := v.Args[0].Type
		if a.IsBool() {
			return rng{0, 1}
		}
		if a.Size < 8 {
			return rng{0, 1<<(8*a.Size) - 1}
		}
	case ssa.OpSignExt:
		if s := v.Args[0].Type.Size; s < 8 {
			return rng{-1 << (8*s - 1), 1<<(8*s-1) - 1}
		}
	case ssa.OpAnd:
		a, b := arg(0), arg(1)
		switch {
		case a.nonneg() && b.nonneg():
			return rng{0, min(a.hi, b.hi)}
		case a.nonneg():
			return rng{0, a.hi}
		case b.nonneg():
			return rng{0, b.hi}
		}
	case ssa.OpShrL:
		if k := v.Args[1]; k.Op == ssa.OpConst && k.AuxInt >= 1 && k.AuxInt < 64 {
			if a := arg(0); a.nonneg() {
				return rng{a.lo >> k.AuxInt, a.hi >> k.AuxInt}
			}
			return rng{0, int64(uint64(math.MaxUint64) >> uint64(k.AuxInt))}
		}
	case ssa.OpShrA:
		if k := v.Args[1]; k.Op == ssa.OpConst && k.AuxInt >= 0 && k.AuxInt < 64 {
			a := arg(0)
			return rng{a.lo >> k.AuxInt, a.hi >> k.AuxInt}
		}
	case ssa.OpModu:
		if c := v.Args[1]; c.Op == ssa.OpConst && c.AuxInt > 0 {
			return rng{0, c.AuxInt - 1}
		}
	case ssa.OpMod:
		if c := v.Args[1]; c.Op == ssa.OpConst && c.AuxInt != 0 && c.AuxInt != math.MinInt64 {
			m := c.AuxInt
			if m < 0 {
				m = -m
			}
			if arg(0).nonneg() {
				return rng{0, m - 1}
			}
			return rng{-(m - 1), m - 1}
		}
	case ssa.OpAdd:
		return addRange(arg(0), arg(1))
	case ssa.OpSub:
		b := arg(1)
		if b.lo == math.MinInt64 {
			return fullRange
		}
		return addRange(arg(0), rng{-b.hi, -b.lo})
	case ssa.OpMul:
		a, b := arg(0), arg(1)
		if a.nonneg() && b.nonneg() {
			if hi, lo := bits.Mul64(uint64(a.hi), uint64(b.hi)); hi == 0 && lo <= math.MaxInt64 {
				return rng{a.lo * b.lo, int64(lo)}
			}
		}
	case ssa.OpSelect:
		return arg(1).union(arg(2))
	case ssa.OpPhi:
		return p.phiRange(v)
	}
	return fullRange
}

// addRange is the range of a+b, or the full range if the sum can overflow.
func addRange(a, b rng) rng {
	lo, ok1 := addNoOverflow(a.lo, b.lo)
	hi, ok2 := addNoOverflow(a.hi, b.hi)
	if !ok1 || !ok2 {
		return fullRange
	}
	return rng{lo, hi}
}

func addNoOverflow(a, b int64) (int64, bool) {
	s := a + b
	if (s > a) != (b > 0) {
		return 0, false
	}
	return s, true
}

// phiRange computes the range of a phi. Besides merging its incoming
// ranges, it recognises induction variables: a loop header phi whose
// back-edge values are the phi plus a non-negative (or non-positive) step,
// in a loop whose header test bounds the phi on the path to every step.
func (p *prover) phiRange(v *ssa.Value) rng {
	h := v.Block
	init := rng{math.MaxInt64, math.MinInt64} // empty
	var steps []*ssa.Value
	for i, a := range v.Args {
		pred := h.Preds[i].B
		if p.dom.Dominates(h, pred) {
			if a != v {
				steps = compat.Append(steps, a)
			}
			continue
		}
		init = init.union(p.staticRange(a))
	}
	if init.lo > init.hi {
		return fullRange // only back edges: unreachable or malformed
	}
	if len(steps) == 0 {
		return init
	}
	// Each step must be v + s for some s whose sign is known.
	var sMin, sMax int64
	for _, a := range steps {
		s, ok := p.stepOf(a, v)
		if !ok {
			return fullRange
		}
		sMin, sMax = min(sMin, s.lo), max(sMax, s.hi)
	}
	if sMin < 0 && sMax > 0 {
		return fullRange
	}
	// The header must test v, and the branch into the loop body must
	// dominate every step, so that the test's fact holds where v steps.
	if h.Kind != ssa.BlockIf {
		return fullRange
	}
	body := -1
	for k, e := range h.Succs {
		if len(e.B.Preds) != 1 {
			continue
		}
		all := true
		for _, a := range steps {
			if !p.dom.Dominates(e.B, a.Block) {
				all = false
				break
			}
		}
		if all {
			body = k
		}
	}
	if body < 0 {
		return fullRange
	}
	ft, ok := condFact(h.Controls[0], body == 0)
	if !ok || ft.x != v && ft.y != v {
		return fullRange
	}
	at := p.refine(fullRange, v, ft) // v's range where it steps
	if sMax > 0 {
		hi, ok := addNoOverflow(at.hi, sMax)
		if !ok {
			return fullRange
		}
		return rng{init.lo, max(init.hi, hi)}
	}
	lo, ok := addNoOverflow(at.lo, sMin)
	if !ok {
		return fullRange
	}
	return rng{min(init.lo, lo), init.hi}
}

// stepOf returns the range of s when a is v+s (or v-s, as -s).
func (p *prover) stepOf(a, v *ssa.Value) (rng, bool) {
	switch a.Op {
	case ssa.OpAdd:
		if a.Args[0] == v {
			return p.staticRange(a.Args[1]), true
		}
		if a.Args[1] == v {
			return p.staticRange(a.Args[0]), true
		}
	case ssa.OpSub:
		if a.Args[0] == v {
			s := p.staticRange(a.Args[1])
			if s.lo == math.MinInt64 {
				return rng{}, false
			}
			return rng{-s.hi, -s.lo}, true
		}
	}
	return rng{}, false
}
