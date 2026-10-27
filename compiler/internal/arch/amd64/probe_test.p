// Ported from internal/arch/amd64/probe_test.go at the go-cutover tag.

package amd64

import (
	"pluto/testing"

	"compiler/compat"
	"compiler/pu"
)

// TestStackProbes checks which frames probe their stack, and how
// (language spec sections 19.1 and 17.5).
func TestStackProbes(t *testing.T) {
	type probeCase struct {
		drop      int64
		interrupt bool
		want      string
	}
	for _, tc := range []probeCase{
		{0, false, ""},
		{4088, false, ""}, // the first call's return address is at most a page below
		{4096, false, "test8 %rsp, -4096(%rsp)"},
		{4 * 4096, false, "test8 %rsp, -4096(%rsp)|test8 %rsp, -8192(%rsp)|test8 %rsp, -12288(%rsp)|test8 %rsp, -16384(%rsp)"},
		{2 << 20, false, "lea -2097152(%rsp), %r11; mov %rsp, %r10; 1: sub $4096, %r10; test %r10, (%r10); cmp %r11, %r10; ja 1b"},
		{5 * 4096, false, "lea -20480(%rsp), %r11; mov %rsp, %r10; 1: sub $4096, %r10; test %r10, (%r10); cmp %r11, %r10; ja 1b"},
		{5*4096 - 8, false, "test8 %rsp, -4096(%rsp)|test8 %rsp, -8192(%rsp)|test8 %rsp, -12288(%rsp)|test8 %rsp, -16384(%rsp)"},
		{6 * 4096, true, "test8 %rsp, -4096(%rsp)|test8 %rsp, -8192(%rsp)|test8 %rsp, -12288(%rsp)|test8 %rsp, -16384(%rsp)|test8 %rsp, -20480(%rsp)|test8 %rsp, -24576(%rsp)"},
	} {
		var got []string
		for _, in := range stackProbes(tc.drop, tc.interrupt) {
			got = compat.Append(got, in.String())
		}
		if g := compat.Join(got, "|"); g != tc.want {
			t.Errorf("drop %d, interrupt %v:\n got %s\nwant %s", tc.drop, tc.interrupt, g, tc.want)
		}
	}
}

// TestEncodeStackProbes checks the probes' encoding against GNU as.
func TestEncodeStackProbes(t *testing.T) {
	cases := []gasCase{
		{stackProbes(4096, false)[0], "test %rsp, -4096(%rsp)"},
		{newInst(MInst{Op: MSTACKPROBE, Imm: 20480}), "lea -20480(%rsp), %r11\n\t{load} mov %rsp, %r10\n1:\tsub $4096, %r10\n\ttest %r10, (%r10)\n\t{load} cmp %r11, %r10\n\tja 1b"},
		{newInst(MInst{Op: MSTACKPROBE, Imm: 64 << 20}), "lea -67108864(%rsp), %r11\n\t{load} mov %rsp, %r10\n1:\tsub $4096, %r10\n\ttest %r10, (%r10)\n\t{load} cmp %r11, %r10\n\tja 1b"},
	}
	checkAgainstGAS(t, cases)
}

// TestPrologueProbes checks that a prologue probes a large frame after
// its pushes and before it moves RSP, and that the unwind tables keep the
// CFA at the pushes until the frame is allocated.
func TestPrologueProbes(t *testing.T) {
	b0 := pu.New(MBlock{ID: 0})
	b0.Insts = insts(newInst(MInst{Op: MEPILOGUE}))
	code := framed(t, regs(RBX), 1<<20, false, b0)
	// push rbx (1); lea (8); mov (3); sub (7); test (3); cmp (3); ja (2);
	// sub rsp, 1 MiB (7); ...
	checkStates(t, code.Debug.CFI[:3], []cfa{
		{0, RSP, 8, nil},
		{1, RSP, 16, map[Reg]int64{RBX: -16}},
		{34, RSP, 16 + 1<<20, map[Reg]int64{RBX: -16}},
	})
	if code.Debug.PrologueEnd != 34 {
		t.Errorf("prologue ends at %d, want 34", code.Debug.PrologueEnd)
	}
}
