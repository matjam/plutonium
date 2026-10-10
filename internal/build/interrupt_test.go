package build_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestInterruptListing checks the code generated for //pu:interrupt
// handlers: register saves in the prologue, cld, the mirror-image restores
// and iretq in place of ret, and the error code dropped before iretq.
func TestInterruptListing(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

import "pluto/arch/x86"

var ticks, lastCode uint64
var scale float64

//pu:noinline
func work(n uint64) uint64 { return n*3 + 1 }

//pu:interrupt
func leaf(f *x86.InterruptFrame) { ticks++ }

//pu:interrupt
func caller(f *x86.InterruptFrame, code uint64) {
	lastCode = work(code)
	f.RIP += 2
}

//pu:interrupt
func floats(f *x86.InterruptFrame) { scale = scale*1.5 + float64(f.RIP) }

func main() {
	println(x86.HandlerAddr(leaf) != 0, x86.HandlerAddr(caller) != 0, x86.HandlerAddr(floats) != 0)
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
	leaf := lines(listFunc(t, src, "main.leaf"))
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
	caller := lines(listFunc(t, src, "main.caller"))
	text := strings.Join(caller, "\n")
	saved := pushes(caller)
	for _, r := range []string{"rax", "rcx", "rdx", "rsi", "rdi", "r8", "r9", "r10", "r11"} {
		if !contains(saved, r) {
			t.Errorf("caller does not save %%%s (saves %v)\n%s", r, saved, text)
		}
	}
	if got := pops(caller); !equal(got, saved) {
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
	floats := strings.Join(lines(listFunc(t, src, "main.floats")), "\n")
	if !strings.Contains(floats, "movups %xmm0,") {
		t.Errorf("floats does not save the XMM registers it changes\n%s", floats)
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
