// Ported from internal/build/interrupt_test.go at the go-cutover tag.

package tests

import (
	"slices"
	"strings"
	"testing"
)

// TestInterruptListing checks the code generated for //pu:interrupt
// handlers: register saves in the prologue, cld, the mirror-image restores
// and iretq in place of ret, and the error code dropped before iretq.
func TestInterruptListing(t *testing.T) {
	t.Parallel()
	const src = `package main

import (
	"pluto/arch/x86"
	"pluto/sync/atomic"
)

// A handler is a party in the data race rules, so what it shares is atomic.
var lastCode atomic.Uint64

//pu:noinline
func work(n uint64) uint64 { return n*3 + 1 }

//pu:interrupt
func leaf(f *x86.InterruptFrame) { f.RIP++ }

//pu:interrupt
func caller(f *x86.InterruptFrame, code uint64) {
	lastCode.Store(work(code))
	f.RIP += 2
}

//pu:interrupt
func floats(f *x86.InterruptFrame) { f.RIP = uint64(float64(f.RIP)*1.5 + 2) }

var idt [3]x86.Gate

func main() {
	idt[0] = x86.InterruptGate(leaf, 0x08, 0)
	idt[1] = x86.InterruptGate(caller, 0x08, 0)
	idt[2] = x86.InterruptGate(floats, 0x08, 0)
	println(len(idt))
}
`
	lines := func(asm string) []string {
		var out []string
		for _, l := range strings.Split(asm, "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasSuffix(l, ":") {
				out = append(out, l)
			}
		}
		return out
	}
	pushes := func(ls []string) []string {
		var regs []string
		for _, l := range ls {
			if r, ok := strings.CutPrefix(l, "push8 %"); ok {
				regs = append(regs, r)
			}
		}
		return regs
	}
	pops := func(ls []string) []string {
		var regs []string
		for _, l := range ls {
			if r, ok := strings.CutPrefix(l, "pop8 %"); ok {
				regs = append([]string{r}, regs...)
			}
		}
		return regs
	}

	// A leaf saves only what it changes and saves no XMM register.
	leaf := lines(listFuncUnchecked(t, src, "main.leaf"))
	if got := pushes(leaf); len(got) == 0 || len(got) > 2 {
		t.Errorf("leaf saves %v, want only the registers it uses\n%s", got, strings.Join(leaf, "\n"))
	}
	if strings.Contains(strings.Join(leaf, "\n"), "movups") {
		t.Errorf("leaf saves XMM registers it never touches\n%s", strings.Join(leaf, "\n"))
	}
	if leaf[len(leaf)-1] != "iretq" || strings.Contains(strings.Join(leaf, "\n"), "ret0") {
		t.Errorf("leaf does not end in iretq\n%s", strings.Join(leaf, "\n"))
	}

	// A handler that calls saves every caller-saved register, all sixteen
	// XMM registers, and drops the error code before iretq.
	caller := lines(listFuncUnchecked(t, src, "main.caller"))
	text := strings.Join(caller, "\n")
	saved := pushes(caller)
	for _, r := range []string{"rax", "rcx", "rdx", "rsi", "rdi", "r8", "r9", "r10", "r11"} {
		if !slices.Contains(saved, r) {
			t.Errorf("caller does not save %%%s (saves %v)\n%s", r, saved, text)
		}
	}
	if got := pops(caller); !slices.Equal(got, saved) {
		t.Errorf("caller restores %v, saved %v\n%s", got, saved, text)
	}
	if strings.Count(text, "movups %xmm") != 16 || strings.Count(text, ", %xmm") < 16 {
		t.Errorf("caller does not save and restore XMM0-XMM15\n%s", text)
	}
	if !strings.Contains(text, "cld") {
		t.Errorf("caller does not clear the direction flag\n%s", text)
	}
	n := len(caller)
	if n < 2 || caller[n-1] != "iretq" || caller[n-2] != "add8 $8, %rsp" {
		t.Errorf("caller must drop the error code and iretq\n%s", text)
	}

	// Float code in the handler itself saves the XMM registers it uses.
	floats := strings.Join(lines(listFuncUnchecked(t, src, "main.floats")), "\n")
	if !strings.Contains(floats, "movups %xmm0,") {
		t.Errorf("floats does not save the XMM registers it changes\n%s", floats)
	}
}
