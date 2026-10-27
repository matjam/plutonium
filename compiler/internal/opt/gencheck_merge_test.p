// Ported from internal/opt/gencheck_merge_test.go at the go-cutover tag.

package opt

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/pu"
)

// genTest builds a checked function by hand for the placement tests.
type genTest struct {
	p *ssa.Program
	f *ssa.Func
	b *ssa.Block   // the block values go into
	m *ssa.Value   // the current memory state
	x []*ssa.Value // the loaded values, summed into the result
}

// newGenTest returns a function of n pointer parameters that returns an i64.
// A function named "quiet" with a body that frees nothing is in the
// program, for calls that cannot free.
func newGenTest(n int) *genTest {
	p := ssa.NewProgram()
	quiet := ssa.NewFunc("quiet")
	quiet.Sym = p.Sym(quiet.Name, ssa.SymFunc)
	qm := quiet.Entry.NewValue(ssa.OpInitMem, ssa.TMem)
	quiet.Entry.Kind = ssa.BlockRet
	quiet.Entry.SetControl(qm)
	p.Funcs = compat.Append(p.Funcs, quiet)

	f := ssa.NewFunc("merge")
	for range n {
		f.Params = compat.Append(f.Params, ssa.TPtr)
	}
	f.Results = compat.Append[*ssa.Type](nil, ssa.TI64)
	f.Pragmas = ssa.PragmaGenCheck
	f.GenLive = p.Sym("pluto/rt.GenRegisteredLive", ssa.SymFunc)
	f.GenRange = p.Sym("pluto/rt.GenRegisteredRangeLive", ssa.SymFunc)
	f.GenPanic = p.Sym("pluto/rt.panicStale", ssa.SymFunc)
	f.GenRangePanic = p.Sym("pluto/rt.panicStaleRange", ssa.SymFunc)
	f.Sym = p.Sym(f.Name, ssa.SymFunc)
	p.Funcs = compat.Append(p.Funcs, f)
	t := pu.New(genTest{p: p, f: f, b: f.Entry})
	t.m = t.b.NewValue(ssa.OpInitMem, ssa.TMem)
	return t
}

func (t *genTest) arg(i int) *ssa.Value { return t.b.NewValueI(ssa.OpArg, ssa.TPtr, int64(i)) }

// at returns p plus off.
func (t *genTest) at(p *ssa.Value, off int64) *ssa.Value {
	if off == 0 {
		return p
	}
	return t.b.NewValueI(ssa.OpOffPtr, ssa.TPtr, off, p)
}

// load loads a value of type typ from p plus off.
func (t *genTest) load(typ *ssa.Type, p *ssa.Value, off int64) {
	v := t.b.NewValue(ssa.OpLoad, typ, t.at(p, off), t.m)
	if typ != ssa.TI64 {
		v = t.b.NewValue(ssa.OpZeroExt, ssa.TI64, v)
	}
	t.x = compat.Append(t.x, v)
}

// store stores an i64 to p plus off.
func (t *genTest) store(p *ssa.Value, off int64) {
	t.m = t.b.NewValueA(ssa.OpStore, ssa.TMem, ssa.TI64, t.at(p, off), t.f.ConstInt(ssa.TI64, 1), t.m)
}

// call calls name with p.
func (t *genTest) call(name string, p *ssa.Value) {
	c := t.b.NewValueA(ssa.OpCall, memTuple(), t.p.Sym(name, ssa.SymFunc), p, t.m)
	c.Call = pu.New(ssa.CallInfo{ABI: ssa.ABIInternal})
	t.m = t.b.NewValueI(ssa.OpSelectN, ssa.TMem, 0, c)
}

// volatile reads a static variable with a volatile load.
func (t *genTest) volatile() {
	addr := t.b.NewValueA(ssa.OpSymAddr, ssa.TPtr, t.p.Sym("flag", ssa.SymData))
	v := t.b.NewValueI(ssa.OpLoad, ssa.TI64, ssa.MemVolatile, addr, t.m)
	t.x = compat.Append(t.x, v)
}

// jump ends the current block with a jump to a new one, and continues
// there.
func (t *genTest) jump() {
	n := t.f.NewBlock(ssa.BlockPlain)
	t.b.AddEdgeTo(n)
	t.b = n
}

// done ends the function, returning the sum of the loaded values, and
// places the checks.
func (t *genTest) done(tb *testing.T) {
	tb.Helper()
	sum := t.f.ConstInt(ssa.TI64, 0)
	for _, v := range t.x {
		sum = t.b.NewValue(ssa.OpAdd, ssa.TI64, sum, v)
	}
	t.b.Kind = ssa.BlockRet
	t.b.SetControl(t.m, sum)
	GenFrees(t.p)
	GenCheck(t.f)
	if err := t.f.Verify(); err != nil {
		tb.Fatal(err)
	}
}

// checks returns the checks GenCheck placed, as their address operands'
// offsets from their roots and their byte lengths.
func (t *genTest) checks() [][2]int64 {
	var out [][2]int64
	for _, b := range t.f.Blocks {
		for _, v := range b.Values {
			if v.Op != ssa.OpCall || v.Aux != t.f.GenLive && v.Aux != t.f.GenRange {
				continue
			}
			_, off := genOffsetRoot(v.Args[0])
			n := int64(1)
			if v.Aux == t.f.GenRange {
				n = v.Args[1].AuxInt
			}
			out = compat.Append(out, [2]int64{off, n})
		}
	}
	return out
}

type genMergeCase struct {
	name  string
	build func(t *genTest)
	want  [][2]int64 // the checks, as offset and length, in any order
}

func TestGenCheckMerge(t *testing.T) {
	for _, tc := range []genMergeCase{
		{"two fields", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 8)
			t.load(ssa.TI64, p, 0)
		}, [][2]int64{{0, 16}}},
		{"three fields out of order", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 16)
			t.load(ssa.TI64, p, 0)
			t.load(ssa.TI64, p, 8)
		}, [][2]int64{{0, 24}}},
		{"overlapping extents", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.load(ssa.TI8, p, 4)
			t.load(ssa.TI32, p, 6)
		}, [][2]int64{{0, 10}}},
		{"stores", func(t *genTest) {
			p := t.arg(0)
			t.store(p, 8)
			t.store(p, 0)
		}, [][2]int64{{0, 16}}},
		{"interior extent of a checked one", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.load(ssa.TI64, p, 8)
			t.jump()
			t.load(ssa.TI32, p, 4)
			t.load(ssa.TI64, p, 8)
		}, [][2]int64{{0, 16}}},
		{"extent covered by two checks", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.call("quiet", p)
			t.load(ssa.TI64, p, 8)
			t.jump()
			t.load(ssa.TI64, p, 4)
		}, [][2]int64{{0, 8}, {8, 8}}},
		{"same extents after a call that cannot free", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.load(ssa.TI64, p, 8)
			t.call("quiet", p)
			t.load(ssa.TI64, p, 8)
			t.load(ssa.TI64, p, 0)
		}, [][2]int64{{0, 16}}},

		// Must not merge.
		{"free between", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.call("pluto/rt.GenAfterCustomFree", p)
			t.load(ssa.TI64, p, 8)
		}, [][2]int64{{0, 8}, {8, 8}}},
		{"free between, then the same field", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.load(ssa.TI64, p, 8)
			t.call("pluto/rt.GenAfterCustomFree", p)
			t.load(ssa.TI64, p, 0)
		}, [][2]int64{{0, 16}, {0, 8}}},
		{"reset between", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.call("pluto/rt.GenSetEpoch", p)
			t.load(ssa.TI64, p, 8)
		}, [][2]int64{{0, 8}, {8, 8}}},
		{"unknown function between", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.call("external.unknown", p)
			t.load(ssa.TI64, p, 8)
		}, [][2]int64{{0, 8}, {8, 8}}},
		{"call that cannot free between", func(t *genTest) {
			// The call may not return: the second access may never run.
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.call("quiet", p)
			t.load(ssa.TI64, p, 8)
		}, [][2]int64{{0, 8}, {8, 8}}},
		{"volatile access between", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.volatile()
			t.load(ssa.TI64, p, 8)
		}, [][2]int64{{0, 8}, {8, 8}}},
		{"gap between", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.load(ssa.TI64, p, 16)
		}, [][2]int64{{0, 8}, {16, 8}}},
		{"different bases", func(t *genTest) {
			p, q := t.arg(0), t.arg(1)
			t.load(ssa.TI64, p, 0)
			t.load(ssa.TI64, q, 8)
		}, [][2]int64{{0, 8}, {8, 8}}},
		{"different blocks", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.jump()
			t.load(ssa.TI64, p, 8)
		}, [][2]int64{{0, 8}, {8, 8}}},
		{"partly covered", func(t *genTest) {
			p := t.arg(0)
			t.load(ssa.TI64, p, 0)
			t.jump()
			t.load(ssa.TI64, p, 4)
		}, [][2]int64{{0, 8}, {4, 8}}},
	} {
		// The case's want is copied to the heap: it is formatted in the
		// subtest, and the case lives in this frame.
		want, build := compat.Clone(tc.want), tc.build
		t.Run(tc.name, func(t *testing.T) {
			g := newGenTest(2)
			build(g)
			g.done(t)
			got := g.checks()
			if !sameChecks(got, want) {
				t.Fatalf("checks %v, want %v:\n%s", got, want, g.f.String())
			}
		})
	}
}

func sameChecks(a, b [][2]int64) bool {
	if len(a) != len(b) {
		return false
	}
	left := compat.Clone(b)
	for _, x := range a {
		found := false
		for i, y := range left {
			if x == y {
				left = compat.Append(left[:i], left[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// A shared check goes before the first access it covers, and every access
// still uses its own address without the tag.
func TestGenCheckMergePlacement(t *testing.T) {
	g := newGenTest(1)
	p := g.arg(0)
	g.load(ssa.TI64, p, 16)
	first := g.x[0]
	g.load(ssa.TI64, p, 8)
	g.load(ssa.TI64, p, 0)
	g.done(t)
	if got := g.checks(); !sameChecks(got, [][2]int64{{8, 16}, {0, 8}}) && !sameChecks(got, [][2]int64{{0, 24}}) {
		t.Fatalf("checks %v:\n%s", got, g.f.String())
	}
	// The check's block jumps to the block of the first access.
	var check *ssa.Block
	for _, b := range g.f.Blocks {
		for _, v := range b.Values {
			if v.Op == ssa.OpCall && v.Aux == g.f.GenRange {
				check = b
			}
		}
	}
	if check == nil || check.Succs[0].B != first.Block {
		t.Fatalf("the check does not go before the first access:\n%s", g.f.String())
	}
	offs := map[int64]bool{}
	for _, b := range g.f.Blocks {
		for _, v := range b.Values {
			if v.Op != ssa.OpLoad {
				continue
			}
			root, off := genOffsetRoot(v.Args[0])
			if !isUntagPattern(root) {
				t.Fatalf("%s uses a tagged address:\n%s", v.String(), g.f.String())
			}
			offs[off] = true
		}
	}
	if len(offs) != 3 || !offs[0] || !offs[8] || !offs[16] {
		t.Fatalf("accesses lost their offsets %v:\n%s", offs, g.f.String())
	}
}

// genLoop builds a loop over n iterations whose body is body, in a
// function of one pointer parameter p, and places the checks. It returns
// the blocks of the loop.
func genLoop(tb *testing.T, body func(t *genTest, p *ssa.Value)) (*genTest, map[*ssa.Block]bool) {
	g := newGenTest(1)
	p := g.arg(0)
	n := g.f.ConstInt(ssa.TI64, 10)
	zero := g.f.ConstInt(ssa.TI64, 0)
	one := g.f.ConstInt(ssa.TI64, 1)
	entry := g.b
	header := g.f.NewBlock(ssa.BlockIf)
	bodyB := g.f.NewBlock(ssa.BlockPlain)
	exit := g.f.NewBlock(ssa.BlockRet)
	entry.AddEdgeTo(header)
	i := header.NewValue(ssa.OpPhi, ssa.TI64, zero)
	mphi := header.NewValue(ssa.OpPhi, ssa.TMem, g.m)
	sphi := header.NewValue(ssa.OpPhi, ssa.TI64, zero)
	header.SetControl(header.NewValue(ssa.OpLess, ssa.TBool, i, n))
	header.AddEdgeTo(bodyB)
	header.AddEdgeTo(exit)
	g.b, g.m, g.x = bodyB, mphi, nil
	body(g, p)
	sum := sphi
	for _, v := range g.x {
		sum = g.b.NewValue(ssa.OpAdd, ssa.TI64, sum, v)
	}
	next := g.b.NewValue(ssa.OpAdd, ssa.TI64, i, one)
	g.b.AddEdgeTo(header)
	i.AddArg(next)
	mphi.AddArg(g.m)
	sphi.AddArg(sum)
	exit.SetControl(mphi, sphi)
	GenFrees(g.p)
	GenCheck(g.f)
	if err := g.f.Verify(); err != nil {
		tb.Fatal(err)
	}
	loop := map[*ssa.Block]bool{}
	for _, l := range g.f.Loops().Loops {
		for _, b := range l.Blocks {
			loop[b] = true
		}
	}
	if !loop[header] {
		tb.Fatalf("no loop:\n%s", g.f.String())
	}
	return g, loop
}

func TestGenCheckMergeLoops(t *testing.T) {
	// No barrier: the fields' shared check goes before the loop.
	g, loop := genLoop(t, func(t *genTest, p *ssa.Value) {
		t.load(ssa.TI64, p, 0)
		t.load(ssa.TI64, p, 8)
	})
	if got := g.checks(); !sameChecks(got, [][2]int64{{0, 16}}) {
		t.Fatalf("invariant fields: checks %v, want one of 16 bytes:\n%s", got, g.f.String())
	}
	for _, b := range g.f.Blocks {
		for _, v := range b.Values {
			if v.Op == ssa.OpCall && v.Aux == g.f.GenRange && loop[b] {
				t.Fatalf("invariant fields: check left in the loop:\n%s", g.f.String())
			}
		}
	}

	// A free in the loop between the fields: each is checked in every
	// iteration, on its own.
	g, loop = genLoop(t, func(t *genTest, p *ssa.Value) {
		t.load(ssa.TI64, p, 0)
		t.call("pluto/rt.GenAfterCustomFree", p)
		t.load(ssa.TI64, p, 8)
	})
	if got := g.checks(); !sameChecks(got, [][2]int64{{0, 8}, {8, 8}}) {
		t.Fatalf("free in the loop: checks %v:\n%s", got, g.f.String())
	}
	for _, b := range g.f.Blocks {
		for _, v := range b.Values {
			if v.Op == ssa.OpCall && (v.Aux == g.f.GenRange || v.Aux == g.f.GenLive) && !loop[b] {
				t.Fatalf("free in the loop: check moved out of the loop:\n%s", g.f.String())
			}
		}
	}

	// A free in the loop after both fields: they share one check, in the
	// loop.
	g, loop = genLoop(t, func(t *genTest, p *ssa.Value) {
		t.load(ssa.TI64, p, 8)
		t.load(ssa.TI64, p, 0)
		t.call("pluto/rt.GenAfterCustomFree", p)
	})
	if got := g.checks(); !sameChecks(got, [][2]int64{{0, 16}}) {
		t.Fatalf("free after the fields: checks %v:\n%s", got, g.f.String())
	}
	for _, b := range g.f.Blocks {
		for _, v := range b.Values {
			if v.Op == ssa.OpCall && v.Aux == g.f.GenRange && !loop[b] {
				t.Fatalf("free after the fields: check moved out of the loop:\n%s", g.f.String())
			}
		}
	}
}
