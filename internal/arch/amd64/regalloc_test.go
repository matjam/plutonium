package amd64

import (
	"slices"
	"testing"
)

// chain builds a function whose blocks run b0 -> b1 -> ... -> b(n-1) but
// are laid out in reverse after the entry, defines a register in b0 and
// uses it in the last block.
func chain(n int) (*MFunc, Reg) {
	f := newMFunc("chain")
	r := f.newVReg(ClassGP)
	blocks := make([]*MBlock, n)
	for i := range blocks {
		blocks[i] = &MBlock{ID: i}
	}
	for i := 0; i+1 < n; i++ {
		blocks[i].Succs = []*MBlock{blocks[i+1]}
		blocks[i+1].Preds = []*MBlock{blocks[i]}
	}
	blocks[0].Insts = []*MInst{{Op: MMOV, Size: 8, Dst: RegOp(r), Src: ImmOp(1)}}
	blocks[n-1].Insts = []*MInst{{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: RegOp(r)}, {Op: MRET}}
	f.Blocks = append([]*MBlock{blocks[0]}, blocks[1:]...)
	slices.Reverse(f.Blocks[1:])
	return f, r
}

func TestPostorder(t *testing.T) {
	f, _ := chain(4) // layout b0 b3 b2 b1
	index := map[*MBlock]int{}
	for i, b := range f.Blocks {
		index[b] = i
	}
	got := postorder(f, index)
	want := []int{1, 2, 3, 0} // b3 b2 b1 b0
	if !slices.Equal(got, want) {
		t.Fatalf("postorder = %v, want %v", got, want)
	}
}

// TestLivenessLayout checks that a register defined at the top of a long
// chain is live out of every block before its use, whatever the layout.
func TestLivenessLayout(t *testing.T) {
	f, r := chain(200)
	live := liveness(f)
	x := f.regIndex(r)
	for i, b := range f.Blocks {
		want := b.ID != 199 // live out of all but the block that uses it
		if live[i].has(x) != want {
			t.Fatalf("block %d: live out %v, want %v", b.ID, live[i].has(x), want)
		}
	}
}
