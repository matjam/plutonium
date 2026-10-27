// Ported from internal/opt/gencheck_range_test.go at the go-cutover tag.

package opt

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/ssa"
)

// countArg adds an i64 parameter to t's function and returns it.
func (t *genTest) countArg() *ssa.Value {
	t.f.Params = compat.Append(t.f.Params, ssa.TI64)
	return t.f.Entry.NewValueI(ssa.OpArg, ssa.TI64, int64(len(t.f.Params)-1))
}

// countLoop builds for i := 0; i < n; i++ { body(i) }: body builds from
// the loop's body block, in t.b, and the loads it adds to t.x are summed
// across iterations. t continues after the loop.
func (t *genTest) countLoop(n *ssa.Value, body func(i *ssa.Value)) {
	zero := t.f.ConstInt(ssa.TI64, 0)
	one := t.f.ConstInt(ssa.TI64, 1)
	h := t.f.NewBlock(ssa.BlockIf)
	t.b.AddEdgeTo(h)
	i := h.NewValue(ssa.OpPhi, ssa.TI64, zero)
	m := h.NewValue(ssa.OpPhi, ssa.TMem, t.m)
	sum := h.NewValue(ssa.OpPhi, ssa.TI64, zero)
	h.SetControl(h.NewValue(ssa.OpLess, ssa.TBool, i, n))
	bodyB := t.f.NewBlock(ssa.BlockPlain)
	exit := t.f.NewBlock(ssa.BlockPlain)
	h.AddEdgeTo(bodyB)
	h.AddEdgeTo(exit)
	outer := t.x
	t.b, t.m, t.x = bodyB, m, nil
	body(i)
	s := sum
	for _, v := range t.x {
		s = t.b.NewValue(ssa.OpAdd, ssa.TI64, s, v)
	}
	next := t.b.NewValue(ssa.OpAdd, ssa.TI64, i, one)
	t.b.AddEdgeTo(h)
	i.AddArg(next)
	m.AddArg(t.m)
	sum.AddArg(s)
	t.b, t.m, t.x = exit, m, compat.Append(outer, sum)
}

// elem returns the address of element i of the array at p, of 1<<shift
// bytes each, plus off.
func (t *genTest) elem(p, i *ssa.Value, shift, off int64) *ssa.Value {
	idx := i
	if shift > 0 {
		idx = t.b.NewValue(ssa.OpShl, ssa.TI64, i, t.f.ConstInt(ssa.TI64, shift))
	}
	return t.at(t.b.NewValue(ssa.OpAddPtr, ssa.TPtr, p, idx), off)
}

// loadAt loads a value of type typ from address a.
func (t *genTest) loadAt(typ *ssa.Type, a *ssa.Value) {
	v := t.b.NewValue(ssa.OpLoad, typ, a, t.m)
	if typ != ssa.TI64 {
		v = t.b.NewValue(ssa.OpZeroExt, ssa.TI64, v)
	}
	t.x = compat.Append(t.x, v)
}

// checkDepths returns, for each check GenCheck placed, the depth of the
// innermost loop holding it (0: outside every loop).
func (t *genTest) checkDepths() []int {
	ln := t.f.Loops()
	var depths []int
	for _, b := range t.f.Blocks {
		for _, v := range b.Values {
			if v.Op == ssa.OpCall && (v.Aux == t.f.GenLive || v.Aux == t.f.GenRange) {
				depths = compat.Append(depths, ln.Depth(b))
			}
		}
	}
	return depths
}

type genRangeCase struct {
	name  string
	build func(t *genTest)
	want  []int // the loop depth of each check, in block order
}

func TestGenCheckRange(t *testing.T) {
	for _, tc := range []genRangeCase{
		{"words", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(i *ssa.Value) { t.loadAt(ssa.TI64, t.elem(p, i, 3, 0)) })
		}, []int{0}},
		{"bytes", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(i *ssa.Value) { t.loadAt(ssa.TI8, t.elem(p, i, 0, 0)) })
		}, []int{0}},
		{"both words of 16-byte elements", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(i *ssa.Value) {
				e := t.elem(p, i, 4, 0)
				t.loadAt(ssa.TI64, t.at(e, 8))
				t.loadAt(ssa.TI64, e)
			})
		}, []int{0}},
		{"store", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(i *ssa.Value) {
				t.m = t.b.NewValueA(ssa.OpStore, ssa.TMem, ssa.TI64, t.elem(p, i, 3, 0), i, t.m)
			})
		}, []int{0}},
		{"nested loops", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(j *ssa.Value) {
				t.countLoop(n, func(i *ssa.Value) { t.loadAt(ssa.TI64, t.elem(p, i, 3, 0)) })
			})
		}, []int{1}},

		// Must not hoist.
		{"call in the loop", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(i *ssa.Value) {
				t.loadAt(ssa.TI64, t.elem(p, i, 3, 0))
				t.call("quiet", p)
			})
		}, []int{1}},
		{"free in the loop", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(i *ssa.Value) {
				t.call("pluto/rt.GenAfterCustomFree", p)
				t.loadAt(ssa.TI64, t.elem(p, i, 3, 0))
			})
		}, []int{1}},
		{"volatile access in the loop", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(i *ssa.Value) {
				t.loadAt(ssa.TI64, t.elem(p, i, 3, 0))
				t.volatile()
			})
		}, []int{1}},
		{"one word of 16-byte elements", func(t *genTest) {
			// The loop does not touch the other words.
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(i *ssa.Value) { t.loadAt(ssa.TI64, t.elem(p, i, 4, 0)) })
		}, []int{1}},
		{"base reassigned in the loop", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			q := t.arg(1)
			t.countLoop(n, func(i *ssa.Value) {
				h := t.b.Preds[0].B
				base := h.NewValueBefore(h.Values[0], ssa.OpPhi, ssa.TPtr, p)
				t.loadAt(ssa.TI64, t.elem(base, i, 3, 0))
				base.AddArg(q)
			})
		}, []int{1}},
		{"count changed in the loop", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			// i < m, where m changes as the loop goes round.
			var m *ssa.Value
			t.countLoop(n, func(i *ssa.Value) {
				h := t.b.Preds[0].B
				m = h.NewValueBefore(h.Values[0], ssa.OpPhi, ssa.TI64, n)
				h.Controls[0].SetArg(1, m)
				t.loadAt(ssa.TI64, t.elem(p, i, 3, 0))
				m.AddArg(t.b.NewValue(ssa.OpSub, ssa.TI64, m, t.f.ConstInt(ssa.TI64, 1)))
			})
		}, []int{1}},
		{"index not the count", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(i *ssa.Value) {
				j := t.b.NewValue(ssa.OpAdd, ssa.TI64, i, t.f.ConstInt(ssa.TI64, 1))
				t.loadAt(ssa.TI64, t.elem(p, j, 3, 0))
			})
		}, []int{1}},
		{"break", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(i *ssa.Value) {
				t.loadAt(ssa.TI64, t.elem(p, i, 3, 0))
				// if x[i] == 0 { break }
				x := t.x[len(t.x)-1]
				t.b.Kind = ssa.BlockIf
				t.b.SetControl(t.b.NewValue(ssa.OpEq, ssa.TBool, x, t.f.ConstInt(ssa.TI64, 0)))
				out := t.f.NewBlock(ssa.BlockRet)
				out.SetControl(t.m, x)
				rest := t.f.NewBlock(ssa.BlockPlain)
				t.b.AddEdgeTo(out)
				t.b.AddEdgeTo(rest)
				t.b = rest
			})
		}, []int{1}},
		{"continue", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			t.countLoop(n, func(i *ssa.Value) {
				// if i&1 == 0 { continue }; sum += x[i]
				odd := t.b.NewValue(ssa.OpAnd, ssa.TI64, i, t.f.ConstInt(ssa.TI64, 1))
				t.b.Kind = ssa.BlockIf
				t.b.SetControl(t.b.NewValue(ssa.OpNe, ssa.TBool, odd, t.f.ConstInt(ssa.TI64, 0)))
				use := t.f.NewBlock(ssa.BlockPlain)
				latch := t.f.NewBlock(ssa.BlockPlain)
				t.b.AddEdgeTo(use)
				t.b.AddEdgeTo(latch)
				t.b = use
				t.loadAt(ssa.TI64, t.elem(p, i, 3, 0))
				x := t.x[0]
				t.b.AddEdgeTo(latch)
				t.b = latch
				t.x = compat.Append[*ssa.Value](nil, latch.NewValue(ssa.OpPhi, ssa.TI64, t.f.ConstInt(ssa.TI64, 0), x))
			})
		}, []int{1}},
		{"access in the header", func(t *genTest) {
			p, n := t.arg(0), t.countArg()
			var h *ssa.Block
			t.countLoop(n, func(i *ssa.Value) {
				h = t.b.Preds[0].B
				a := h.NewValue(ssa.OpAddPtr, ssa.TPtr, p, h.NewValue(ssa.OpShl, ssa.TI64, i, t.f.ConstInt(ssa.TI64, 3)))
				t.x = compat.Append(t.x, h.NewValue(ssa.OpLoad, ssa.TI64, a, h.Values[1]))
			})
		}, []int{1}},
	} {
		// The case's want is copied to the heap: it is formatted in the
		// subtest, and the case lives in this frame.
		want, build := compat.Clone(tc.want), tc.build
		t.Run(tc.name, func(t *testing.T) {
			g := newGenTest(2)
			build(g)
			g.done(t)
			got := g.checkDepths()
			if len(got) != len(want) {
				t.Fatalf("checks at loop depths %v, want %v:\n%s", got, want, g.f.String())
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("checks at loop depths %v, want %v:\n%s", got, want, g.f.String())
				}
			}
		})
	}
}

// The range check before a loop covers exactly the bytes the loop
// touches, and runs only when the loop runs an iteration.
func TestGenCheckRangeExtent(t *testing.T) {
	g := newGenTest(1)
	p, n := g.arg(0), g.countArg()
	// Words at 4 + 4i: each overlaps the next.
	g.countLoop(n, func(i *ssa.Value) { g.loadAt(ssa.TI64, g.elem(p, i, 2, 4)) })
	g.done(t)
	var call *ssa.Value
	for _, b := range g.f.Blocks {
		for _, v := range b.Values {
			if v.Op == ssa.OpCall && v.Aux == g.f.GenRange {
				call = v
			}
		}
	}
	if call == nil {
		t.Fatalf("no range check:\n%s", g.f.String())
	}
	// The first byte: p + 0*4 + 4.
	start := call.Args[0]
	if start.Op != ssa.OpOffPtr || start.AuxInt != 4 || start.Args[0].Op != ssa.OpAddPtr || start.Args[0].Args[0] != p {
		t.Fatalf("range starts at %s:\n%s", start.LongString(), g.f.String())
	}
	// The length: (n-0)*4 + 8 - 4, through the last word's 8 bytes.
	length := call.Args[1]
	if length.Op != ssa.OpAdd || !constEq(length.Args[1], 4) || length.Args[0].Op != ssa.OpMul ||
		!constEq(length.Args[0].Args[1], 4) || length.Args[0].Args[0].Op != ssa.OpSub || length.Args[0].Args[0].Args[0] != n {
		t.Fatalf("range length %s:\n%s", length.LongString(), g.f.String())
	}
	// The block that branches to the check tests the loop's entry: 0 < n.
	pred := call.Block.Preds[0].B
	ctl := pred.Controls[0]
	if ctl.Op != ssa.OpAnd || ctl.Args[0].Op != ssa.OpLess || ctl.Args[0].Args[1] != n {
		t.Fatalf("range check not guarded by the loop's entry test:\n%s", g.f.String())
	}
}
