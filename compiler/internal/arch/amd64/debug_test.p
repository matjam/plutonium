// Ported from internal/arch/amd64/debug_test.go at the go-cutover tag.

package amd64

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/ssa"
	"compiler/pu"
)

// framed builds a function whose body is blocks, gives it the frame
// described by the arguments, and encodes it.
func framed(t *testing.T, saved []Reg, frameSize int64, fp bool, blocks ...*MBlock) *Code {
	t.Helper()
	f := pu.New(MFunc{Blocks: compat.Clone(blocks), usedSaved: compat.Clone(saved), frameSize: frameSize, frameFP: fp, maxAlign: 32, Pos: 100})
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
		t.Fatalf("%d frame states, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Off != w.off || g.CFAReg != w.reg || g.CFAOff != w.cfaOff {
			t.Errorf("state %d: at %d CFA=%s+%d, want at %d CFA=%s+%d", i, g.Off, g.CFAReg.String(), g.CFAOff, w.off, w.reg.String(), w.cfaOff)
		}
		if len(g.Saved) != len(w.saved) {
			t.Errorf("state %d: %d saved registers, want %d", i, len(g.Saved), len(w.saved))
			continue
		}
		for _, s := range g.Saved {
			if off, ok := w.saved[s.Reg]; !ok || off != s.Off {
				t.Errorf("state %d: %s saved at CFA+%d, want %d", i, s.Reg.String(), s.Off, off)
			}
		}
	}
}

func checkLines(t *testing.T, got, want []LineEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d line entries, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("line %d: at %d pos %d inl %d, want at %d pos %d inl %d", i, got[i].Off, int(got[i].Pos), got[i].Inl, w.Off, int(w.Pos), w.Inl)
		}
	}
}

// TestFrameStatesNoFramePointer follows the prologue and two epilogues of
// a frame without a frame pointer.
func TestFrameStatesNoFramePointer(t *testing.T) {
	sym := pu.New(ssa.Sym{Name: "callee"})
	b0 := pu.New(MBlock{ID: 0})
	b1 := pu.New(MBlock{ID: 1})
	b0.Insts = insts(
		newInst(MInst{Op: MCALL, Sym: sym, Pos: 101}),
		newInst(MInst{Op: MJCC, Cond: CondE, Target: b1, Pos: 102}),
		newInst(MInst{Op: MEPILOGUE, Pos: 103}),
	)
	b1.Insts = insts(newInst(MInst{Op: MCALL, Sym: sym, Pos: 104}), newInst(MInst{Op: MEPILOGUE, Pos: 105}))
	code := framed(t, regs(RBX, R12), 24, false, b0, b1)
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
		t.Errorf("frame size %d, %d saved registers, FP %v", d.FrameSize, len(d.Saved), d.FP)
	}
	checkLines(t, d.Lines, []LineEntry{{0, 100, 0}, {7, 101, 0}, {12, 102, 0}, {14, 103, 0}, {22, 104, 0}, {27, 105, 0}})
}

// TestFrameStatesFramePointer follows a frame that keeps RBP as a frame
// pointer because its stack is realigned.
func TestFrameStatesFramePointer(t *testing.T) {
	b0 := pu.New(MBlock{ID: 0})
	b0.Insts = insts(newInst(MInst{Op: MCALL, Sym: pu.New(ssa.Sym{Name: "callee"})}), newInst(MInst{Op: MEPILOGUE}))
	code := framed(t, regs(RBX, RBP), 64, true, b0)
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
		t.Errorf("prologue ends at %d, FP %v, %d saved registers", d.PrologueEnd, d.FP, len(d.Saved))
	}
}

// TestFrameStatesInterrupt follows an interrupt handler whose CPU frame
// has an error code: the interrupted RIP is 16 bytes above RSP on entry,
// and the epilogue drops the error code before iretq.
func TestFrameStatesInterrupt(t *testing.T) {
	b0 := pu.New(MBlock{ID: 0})
	b0.Insts = insts(newInst(MInst{Op: MCALL, Sym: pu.New(ssa.Sym{Name: "callee"})}), newInst(MInst{Op: MEPILOGUE}))
	f := pu.New(MFunc{Blocks: blocksOf(b0), usedSaved: regs(RAX, RCX), frameSize: 8, interrupt: true, errCode: true})
	insertFrame(f)
	code, err := encodeFunc(f, f.Blocks)
	if err != nil {
		t.Fatal(err)
	}
	// push rax (1); push rcx (1); sub rsp, 8 (4); cld (1); call (5);
	// add rsp, 8 (4); pop rcx (1); pop rax (1); add rsp, 8 (4); iretq.
	saves := map[Reg]int64{RAX: -24, RCX: -32}
	checkStates(t, code.Debug.CFI, []cfa{
		{0, RSP, 16, nil},
		{1, RSP, 24, map[Reg]int64{RAX: -24}},
		{2, RSP, 32, saves},
		{6, RSP, 40, saves},
		{16, RSP, 32, saves},
		{17, RSP, 24, map[Reg]int64{RAX: -24}},
		{18, RSP, 16, nil},
		{22, RSP, 8, nil},
	})
	if code.Debug.PrologueEnd != 7 || !code.Debug.Interrupt {
		t.Errorf("prologue ends at %d, interrupt %v", code.Debug.PrologueEnd, code.Debug.Interrupt)
	}
}

// TestLineTableAfterRelaxation checks that positions follow the code when
// a branch before them is shortened, and that instructions without a
// position take the next one's.
func TestLineTableAfterRelaxation(t *testing.T) {
	b0 := pu.New(MBlock{ID: 0})
	b1 := pu.New(MBlock{ID: 1})
	b0.Insts = insts(
		newInst(MInst{Op: MJCC, Cond: CondE, Target: b1, Pos: 10}),
		newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(RAX), Src: RegOp(RBX)}), // no position
		newInst(MInst{Op: MMOV, Size: 8, Dst: RegOp(RCX), Src: RegOp(RDX), Pos: 20}),
	)
	b1.Insts = insts(newInst(MInst{Op: MRET, Pos: 30}))
	code, err := encodeFunc(pu.New(MFunc{}), blocksOf(b0, b1))
	if err != nil {
		t.Fatal(err)
	}
	// je (2 bytes once relaxed); mov (3); mov (3); ret.
	checkLines(t, code.Debug.Lines, []LineEntry{{0, 10, 0}, {2, 20, 0}, {8, 30, 0}})
}

func TestDWARFReg(t *testing.T) {
	type regCase struct {
		r    Reg
		want int
	}
	for _, c := range []regCase{{RAX, 0}, {RDX, 1}, {RCX, 2}, {RBX, 3}, {RSI, 4}, {RDI, 5}, {RBP, 6}, {RSP, 7}, {R8, 8}, {R15, 15}} {
		if got := DWARFReg(c.r); got != c.want {
			t.Errorf("DWARFReg(%s) = %d, want %d", c.r.String(), got, c.want)
		}
	}
}
