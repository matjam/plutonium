package amd64

import (
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
)

// framed builds a function whose body is blocks, gives it the frame
// described by the arguments, and encodes it.
func framed(t *testing.T, saved []Reg, frameSize int64, fp bool, blocks ...*MBlock) *Code {
	t.Helper()
	f := &MFunc{Blocks: blocks, usedSaved: saved, frameSize: frameSize, frameFP: fp, maxAlign: 32, Pos: 100}
	insertFrame(f)
	code, err := encodeFunc(f, blocks)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

type cfa struct {
	off    int
	reg    Reg
	cfaOff int64
	saved  map[Reg]int64
}

func checkStates(t *testing.T, got []FrameState, want []cfa) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d frame states, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Off != w.off || g.CFAReg != w.reg || g.CFAOff != w.cfaOff {
			t.Errorf("state %d: at %d CFA=%s%+d, want at %d CFA=%s%+d", i, g.Off, g.CFAReg, g.CFAOff, w.off, w.reg, w.cfaOff)
		}
		if len(g.Saved) != len(w.saved) {
			t.Errorf("state %d: saved %+v, want %v", i, g.Saved, w.saved)
			continue
		}
		for _, s := range g.Saved {
			if off, ok := w.saved[s.Reg]; !ok || off != s.Off {
				t.Errorf("state %d: %s saved at CFA%+d, want %v", i, s.Reg, s.Off, w.saved)
			}
		}
	}
}

// TestFrameStatesNoFramePointer follows the prologue and two epilogues of
// a frame without a frame pointer.
func TestFrameStatesNoFramePointer(t *testing.T) {
	sym := &ssa.Sym{Name: "callee"}
	b0 := &MBlock{ID: 0}
	b1 := &MBlock{ID: 1}
	b0.Insts = []*MInst{
		{Op: MCALL, Sym: sym, Pos: 101},
		{Op: MJCC, Cond: CondE, Target: b1, Pos: 102},
		{Op: MEPILOGUE, Pos: 103},
	}
	b1.Insts = []*MInst{{Op: MCALL, Sym: sym, Pos: 104}, {Op: MEPILOGUE, Pos: 105}}
	code := framed(t, []Reg{RBX, R12}, 24, false, b0, b1)
	d := code.Debug

	// push rbx (1 byte); push r12 (2); sub rsp, 24 (4); call (5); je (2);
	// add rsp, 24 (4); pop r12 (2); pop rbx (1); ret (1); call (5); ...
	saves := map[Reg]int64{RBX: -16, R12: -24}
	checkStates(t, d.CFI, []cfa{
		{0, RSP, 8, nil},
		{1, RSP, 16, map[Reg]int64{RBX: -16}},
		{3, RSP, 24, saves},
		{7, RSP, 48, saves},
		{18, RSP, 24, saves},
		{20, RSP, 16, map[Reg]int64{RBX: -16}},
		{21, RSP, 8, nil},
		{22, RSP, 48, saves},
		{31, RSP, 24, saves},
		{33, RSP, 16, map[Reg]int64{RBX: -16}},
		{34, RSP, 8, nil},
	})
	if d.PrologueEnd != 7 {
		t.Errorf("prologue ends at %d, want 7", d.PrologueEnd)
	}
	if len(d.Saved) != 2 || d.FrameSize != 24 || d.FP {
		t.Errorf("frame %+v", d)
	}
	wantLines := []LineEntry{{0, 100, 0}, {7, 101, 0}, {12, 102, 0}, {14, 103, 0}, {22, 104, 0}, {27, 105, 0}}
	if len(d.Lines) != len(wantLines) {
		t.Fatalf("lines %+v, want %+v", d.Lines, wantLines)
	}
	for i, w := range wantLines {
		if d.Lines[i] != w {
			t.Errorf("line %d: %+v, want %+v", i, d.Lines[i], w)
		}
	}
}

// TestFrameStatesFramePointer follows a frame that keeps RBP as a frame
// pointer because its stack is realigned.
func TestFrameStatesFramePointer(t *testing.T) {
	b0 := &MBlock{ID: 0}
	b0.Insts = []*MInst{{Op: MCALL, Sym: &ssa.Sym{Name: "callee"}}, {Op: MEPILOGUE}}
	code := framed(t, []Reg{RBX, RBP}, 64, true, b0)
	d := code.Debug

	// push rbp (1); mov rbp, rsp (3); push rbx (1); and rsp, -32 (4);
	// sub rsp, 64 (4); call (5); lea rsp, [rbp-8] (4); pop rbx (1);
	// pop rbp (1); ret.
	checkStates(t, d.CFI, []cfa{
		{0, RSP, 8, nil},
		{1, RSP, 16, map[Reg]int64{RBP: -16}},
		{4, RBP, 16, map[Reg]int64{RBP: -16}},
		{5, RBP, 16, map[Reg]int64{RBP: -16, RBX: -24}},
		{23, RBP, 16, map[Reg]int64{RBP: -16}},
		{24, RSP, 8, nil},
	})
	if d.PrologueEnd != 13 || !d.FP || len(d.Saved) != 1 || d.Saved[0] != RBX {
		t.Errorf("frame %+v", d)
	}
}

// TestLineTableAfterRelaxation checks that positions follow the code when
// a branch before them is shortened, and that instructions without a
// position take the next one's.
func TestLineTableAfterRelaxation(t *testing.T) {
	b0 := &MBlock{ID: 0}
	b1 := &MBlock{ID: 1}
	b0.Insts = []*MInst{
		{Op: MJCC, Cond: CondE, Target: b1, Pos: 10},
		{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: RegOp(RBX)}, // no position
		{Op: MMOV, Size: 8, Dst: RegOp(RCX), Src: RegOp(RDX), Pos: 20},
	}
	b1.Insts = []*MInst{{Op: MRET, Pos: 30}}
	code, err := encodeFunc(&MFunc{}, []*MBlock{b0, b1})
	if err != nil {
		t.Fatal(err)
	}
	// je (2 bytes once relaxed); mov (3); mov (3); ret.
	want := []LineEntry{{0, 10, 0}, {2, 20, 0}, {8, 30, 0}}
	got := code.Debug.Lines
	if len(got) != len(want) {
		t.Fatalf("lines %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDWARFReg(t *testing.T) {
	for r, want := range map[Reg]int{RAX: 0, RDX: 1, RCX: 2, RBX: 3, RSI: 4, RDI: 5, RBP: 6, RSP: 7, R8: 8, R15: 15} {
		if got := DWARFReg(r); got != want {
			t.Errorf("DWARFReg(%s) = %d, want %d", r, got, want)
		}
	}
}
