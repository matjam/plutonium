// Ported from internal/build/gate_test.go at the go-cutover tag.

package tests

import (
	"testing"
)

// TestInterruptGates checks the 16-byte table entries that x86.InterruptGate
// and x86.TrapGate build (language spec section 20.8, abi.md section 10.5):
// the handler's address split around the selector, the interrupt stack table
// index in the low three bits, and the attribute byte 0x8E for an interrupt
// gate and 0x8F for a trap gate. A handler that is not entered through an
// interrupt gate or a trap gate is not installed this way.
func TestInterruptGates(t *testing.T) {
	t.Parallel()
	const src = `package main

import (
	"pluto/arch/x86"
	"unsafe"
)

//pu:interrupt
func irq(f *x86.InterruptFrame) {}

//pu:interrupt trap
func trap(f *x86.InterruptFrame, code uint64) {}

func bytesOf(g *x86.Gate) [16]byte { return *(*[16]byte)(unsafe.Pointer(g)) }

func offset(b [16]byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[6])<<16 | uint64(b[7])<<24 |
		uint64(b[8])<<32 | uint64(b[9])<<40 | uint64(b[10])<<48 | uint64(b[11])<<56
}

var table [4]x86.Gate

func main() {
	table[0] = x86.InterruptGate(irq, 0x08, 1)
	table[1] = x86.TrapGate(trap, 0x10, 9)
	table[2] = x86.InterruptGate(irq, 0x08, 1)
	a, b, c := bytesOf(&table[0]), bytesOf(&table[1]), bytesOf(&table[2])
	println(unsafe.Sizeof(table[0]), len(table))
	// selector, ist, attribute, and the reserved bytes
	println(uint(a[2])|uint(a[3])<<8, a[4], a[5], a[12]|a[13]|a[14]|a[15])
	println(uint(b[2])|uint(b[3])<<8, b[4], b[5], b[12]|b[13]|b[14]|b[15])
	// the address is the handler's: the same for the same handler, and not zero
	println(offset(a) != 0, offset(b) != 0, offset(a) == offset(c), offset(a) != offset(b), a == c, a == b)
	// the entry of an unused vector is empty
	z := bytesOf(&table[3])
	println(z == [16]byte{})
}
`
	const want = "16 4\n8 1 142 0\n16 1 143 0\ntrue true true true true false\ntrue\n"
	for _, level := range []int{0, 2} {
		got, status := compileAndRun(t, []byte(src), level)
		if status != 0 || got != want {
			t.Errorf("-O%d: status %d, output %q, want %q", level, status, got, want)
		}
	}
}
