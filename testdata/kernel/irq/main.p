// A freestanding kernel that installs two interrupt handlers and enters
// them: int $0x80 (no error code) and a general protection fault (with an
// error code). Output goes to the first serial port.
package main

import (
	"pluto/arch/x86"
	"pluto/rt"
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

// An IDT gate is 16 bytes: the handler address split around the selector
// and type fields.
var idt [256][2]uint64

// idtr is the 10-byte operand of lidt: limit, then base.
var idtr [10]byte

func setGate(vec int, h uintptr) {
	addr := uint64(h)
	idt[vec][0] = addr&0xFFFF | 0x08<<16 | 0x8E<<40 | (addr>>16&0xFFFF)<<48
	idt[vec][1] = addr >> 32
}

var (
	softCount   int
	softAlign   uint64
	softCS      uint64
	faultCode   uint64
	faultCount  int
	faultAlign  uint64
	fpuInHandle float64
)

//pu:interrupt
func soft(f *x86.InterruptFrame) {
	softCount++
	softAlign = stackMod16()
	softCS = f.CS
	// Calls and floats clobber caller-saved registers, XMM ones included.
	fpuInHandle = float64(softCount) * 1.5
	println("in handler: vector 0x80, cs", f.CS)
}

//pu:interrupt
func generalProtection(f *x86.InterruptFrame, code uint64) {
	faultCount++
	faultCode = code
	faultAlign = stackMod16()
	f.RIP += 2 // skip the faulting mov to DS
}

func main() int64 {
	rt.SetConsole(serialWrite)
	setGate(0x80, x86.HandlerAddr(soft))
	setGate(13, x86.HandlerAddr(generalProtection))
	*(*uint16)(unsafe.Pointer(&idtr[0])) = uint16(len(idt)*16 - 1)
	*(*uintptr)(unsafe.Pointer(&idtr[2])) = uintptr(unsafe.Pointer(&idt))
	lidt(unsafe.Pointer(&idtr))

	changed := softInt()
	println("int 0x80: count", softCount, "registers changed", changed, "stack mod 16", softAlign, "cs", softCS)
	println("float in handler x2:", int(fpuInHandle*2))
	badSegment()
	println("#GP: count", faultCount, "error code", faultCode, "stack mod 16", faultAlign)
	return 0
}
