package opt

import (
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
)

// branchOnPhi builds
//
//	b0: if x < 0 goto b1 else b2
//	b1: goto b3
//	b2: goto b3
//	b3: p = phi(true, false); q = phi(10, 20); if p goto b4 else b5
//	b4: (join == nil: return 1, q) or goto join
//	b5: (join == nil: return 2, q) or goto join
//	join: return 3, q
//
// and returns it with b3.
func branchOnPhi(t *testing.T, withJoin bool) (*ssa.Func, *ssa.Block) {
	t.Helper()
	f := ssa.NewFunc("branch")
	f.Params = []*ssa.Type{ssa.TI64}
	f.Results = []*ssa.Type{ssa.TI64, ssa.TI64}
	b0 := f.Entry
	mem := b0.NewValue(ssa.OpInitMem, ssa.TMem)
	x := b0.NewValueI(ssa.OpArg, ssa.TI64, 0)
	b1, b2, b3, b4, b5 := f.NewBlock(ssa.BlockPlain), f.NewBlock(ssa.BlockPlain), f.NewBlock(ssa.BlockIf), f.NewBlock(ssa.BlockRet), f.NewBlock(ssa.BlockRet)
	b0.Kind = ssa.BlockIf
	b0.SetControl(b0.NewValue(ssa.OpLess, ssa.TBool, x, f.ConstInt(ssa.TI64, 0)))
	b0.AddEdgeTo(b1)
	b0.AddEdgeTo(b2)
	b1.AddEdgeTo(b3)
	b2.AddEdgeTo(b3)
	p := b3.NewValue(ssa.OpPhi, ssa.TBool, f.ConstBool(true), f.ConstBool(false))
	q := b3.NewValue(ssa.OpPhi, ssa.TI64, f.ConstInt(ssa.TI64, 10), f.ConstInt(ssa.TI64, 20))
	b3.SetControl(p)
	b3.AddEdgeTo(b4)
	b3.AddEdgeTo(b5)
	if withJoin {
		join := f.NewBlock(ssa.BlockRet)
		b4.Kind, b5.Kind = ssa.BlockPlain, ssa.BlockPlain
		b4.AddEdgeTo(join)
		b5.AddEdgeTo(join)
		join.SetControl(mem, f.ConstInt(ssa.TI64, 3), q)
	} else {
		b4.SetControl(mem, f.ConstInt(ssa.TI64, 1), q)
		b5.SetControl(mem, f.ConstInt(ssa.TI64, 2), q)
	}
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	return f, b3
}

func hasBlock(f *ssa.Func, b *ssa.Block) bool {
	for _, x := range f.Blocks {
		if x == b {
			return true
		}
	}
	return false
}

// TestThreadPhiBranch checks that each predecessor of a block branching on
// a phi of constants jumps straight to its successor, the other phis'
// values following along.
func TestThreadPhiBranch(t *testing.T) {
	f, b3 := branchOnPhi(t, false)
	SimplifyCFG(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	if hasBlock(f, b3) {
		t.Errorf("branch on the phi still there:\n%s", f)
	}
	// Each return now returns the constant of its own path.
	for _, b := range f.Blocks {
		if b.Kind != ssa.BlockRet {
			continue
		}
		code, q := b.Controls[1], b.Controls[2]
		if q.Op != ssa.OpConst || q.AuxInt != 10*code.AuxInt {
			t.Errorf("return %d returns q = %s:\n%s", code.AuxInt, q.LongString(), f)
		}
	}
}

// TestThreadPhiBranchJoin checks that a phi used past the join of the
// branch's successors keeps the branch: threading would need a new phi at
// the join.
func TestThreadPhiBranchJoin(t *testing.T) {
	f, b3 := branchOnPhi(t, true)
	SimplifyCFG(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	if !hasBlock(f, b3) {
		t.Errorf("branch threaded although its phi is used past the join:\n%s", f)
	}
}

func TestThreadPhiBranchLocalUses(t *testing.T) {
	f, b := branchOnPhi(t, false)
	// Only the branch's own control uses either phi. The second phi is
	// unused; the first has exactly the terminator's one use.
	f.Results = []*ssa.Type{ssa.TI64}
	for _, e := range b.Succs {
		r := e.B
		r.SetControl(r.Controls[0], r.Controls[1])
	}
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	SimplifyCFG(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	if hasBlock(f, b) {
		t.Fatalf("local control use prevented threading:\n%s", f)
	}
}

func TestThreadPhiBranchPhiEdgeUses(t *testing.T) {
	f, b := branchOnPhi(t, true)
	join := b.Succs[0].B.Succs[0].B
	q := join.Controls[2]
	joined := join.NewValue(ssa.OpPhi, ssa.TI64, q, q)
	join.SetControl(join.Controls[0], join.Controls[1], joined)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	SimplifyCFG(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	if hasBlock(f, b) {
		t.Fatalf("successor phi edge uses prevented threading:\n%s", f)
	}
}

func TestThreadPhiBranchRepeatedUses(t *testing.T) {
	f, b := branchOnPhi(t, false)
	for _, e := range b.Succs {
		r := e.B
		q := r.Controls[2]
		sum := r.NewValue(ssa.OpAdd, ssa.TI64, q, q)
		r.SetControl(r.Controls[0], r.Controls[1], sum)
	}
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	SimplifyCFG(f)
	if err := f.Verify(); err != nil {
		t.Fatal(err)
	}
	if hasBlock(f, b) {
		t.Fatalf("repeated uses on the successor paths prevented threading:\n%s", f)
	}
	for _, r := range f.Blocks {
		if r.Kind != ssa.BlockRet {
			continue
		}
		code, sum := r.Controls[1], r.Controls[2]
		for _, a := range sum.Args {
			if a.Op != ssa.OpConst || a.AuxInt != 10*code.AuxInt {
				t.Fatalf("threaded repeated use has wrong value:\n%s", f)
			}
		}
	}
}
