// A freestanding kernel that installs two interrupt handlers and enters
// them: int $0x80 (no error code) and a general protection fault (with an
// error code). Output goes to the first serial port.
package main

import (
	"pluto/arch/x86"
	"pluto/math"
	"pluto/rt"
	"pluto/sync/atomic"
	"unsafe"
)

// Implemented in boot_amd64.s.
func outb(port uint16, v uint8)
func lidt(p unsafe.Pointer)
func stackMod16() uint64
func softInt() uint64
func badSegment()

const com1 = 0x3F8

func serialWrite(p unsafe.Pointer, n int) {
	for i := 0; i < n; i++ {
		outb(com1, *(*uint8)(unsafe.Add(p, i)))
	}
}

// onPanic reports a panic over the serial port and leaves QEMU through the
// isa-debug-exit device with status 3.
//
//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(rt.Console{})
	for {
		outb(0xf4, 1)
		x86.Cli()
		x86.Hlt()
	}
}

// The interrupt descriptor table: one 16-byte gate for each vector, entered
// through an interrupt gate (x86.InterruptGate), which clears the interrupt
// flag on entry.
var idt [256]x86.Gate

// idtr is the 10-byte operand of lidt: limit, then base.
var idtr [10]byte

// What the handlers record is atomic: a handler is a party in the data race
// rules (language spec section 19.3), and may run on several processors.
var (
	softCount   atomic.Int64
	softAlign   atomic.Uint64
	softCS      atomic.Uint64
	faultCode   atomic.Uint64
	faultCount  atomic.Int64
	faultAlign  atomic.Uint64
	fpuInHandle atomic.Uint64 // the bits of a float64
)

//pu:interrupt
func soft(f *x86.InterruptFrame) {
	n := softCount.Add(1)
	softAlign.Store(stackMod16())
	softCS.Store(f.CS)
	// Calls and floats clobber caller-saved registers, XMM ones included.
	fpuInHandle.Store(math.Float64bits(float64(n) * 1.5))
	println("in handler: vector 0x80, cs", f.CS)
}

//pu:interrupt
func generalProtection(f *x86.InterruptFrame, code uint64) {
	faultCount.Add(1)
	faultCode.Store(code)
	faultAlign.Store(stackMod16())
	f.RIP += 2 // skip the faulting mov to DS
}

func main() int64 {
	rt.SetConsole(serialWrite)
	idt[0x80] = x86.InterruptGate(soft, 0x08, 0)
	idt[13] = x86.InterruptGate(generalProtection, 0x08, 0)
	*(*uint16)(unsafe.Pointer(&idtr[0])) = uint16(len(idt)*16 - 1)
	*(*uintptr)(unsafe.Pointer(&idtr[2])) = uintptr(unsafe.Pointer(&idt))
	lidt(unsafe.Pointer(&idtr))

	changed := softInt()
	println("int 0x80: count", softCount.Load(), "registers changed", changed, "stack mod 16", softAlign.Load(), "cs", softCS.Load())
	println("float in handler x2:", int(math.Float64frombits(fpuInHandle.Load())*2))
	badSegment()
	println("#GP: count", faultCount.Load(), "error code", faultCode.Load(), "stack mod 16", faultAlign.Load())
	return 0
}
