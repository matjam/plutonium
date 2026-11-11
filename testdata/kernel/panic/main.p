// A kernel-mode kernel that panics, to test its //pu:panic handler under
// QEMU. It boots with testdata/kernel/smoke's boot code, which the test
// copies next to it, and the default kernel linker script. The test also
// writes scenario.p, which sets the scenario constant: how it panics.
//
//   - explicit: panic(v) in main.main.
//   - bounds: an index out of range.
//   - fault: a page fault inside an interrupt handler, which the page
//     fault handler turns into a panic with rt.PanicFault.
//   - nested: the panic handler itself panics, which goes to the
//     runtime's last resort: interrupts off and halt, saying nothing.
//   - print: an interrupt raised by the int instruction, which masking
//     does not hold off, arrives in the middle of a print statement. The
//     handler's print finds its own processor holding the console lock and
//     drops its output (spec section 13.12).
//
// The handler prints over the serial port and leaves QEMU through the
// isa-debug-exit device with status 3.
package main

import (
	"pluto/arch/x86"
	"pluto/rt"
	"pluto/sync/atomic"
	"unsafe"
)

// Implemented in boot_amd64.s.
func percpuImage() uintptr
func percpuSize() uintptr

// softInt raises interrupt 0x80 (int_amd64.s).
func softInt()

const com1 = 0x3F8

// serial writes to the first serial port.
type serial struct{}

func (serial) Write(b []byte) (int, error) {
	for _, c := range b {
		for x86.Inb(com1+5)&0x20 == 0 {
			x86.Pause()
		}
		x86.Outb(com1, c)
	}
	return len(b), nil
}

func serialWrite(p unsafe.Pointer, n int) {
	serial{}.Write(unsafe.Slice((*byte)(trapInPrint(p)), n))
}

//pu:panic
func onPanic(p *rt.Panic) {
	println("kernel panic on cpu", p.CPU)
	p.Print(serial{})
	if p.Fault.Kind == rt.MemoryFault {
		println("fault at the bad address:", p.Fault.Addr == badAddr)
	}
	if scenario == "nested" {
		var xs []int
		println(xs[2])
	}
	for {
		x86.Outl(0xf4, 3)
		x86.Cli()
		x86.Hlt()
	}
}

// badAddr is just past the first GiB, the memory the boot code maps.
const badAddr = 0x40000000

//pu:interrupt
func soft(f *x86.InterruptFrame) {
	*(*int)(unsafe.Pointer(uintptr(badAddr))) = 1
}

//pu:interrupt
func pageFault(f *x86.InterruptFrame, code uint64) {
	rt.PanicFault(rt.MemoryFault, uintptr(x86.ReadCR2()), uintptr(f.RIP), uintptr(f.RSP), 0)
}

// The interrupt descriptor table: one 16-byte gate for each vector, entered
// through an interrupt gate (x86.InterruptGate), which clears the interrupt
// flag on entry.
var idt [256]x86.Gate

var idtr x86.DescriptorTablePointer

// block is processor 2's per-CPU block: a copy of the .percpu image whose
// first word is its own address.
var block [64]uint64

//pu:noinline
func get(xs []int, i int) int { return xs[i] }

func main() int64 {
	rt.SetConsole(serialWrite)
	b := unsafe.Pointer(&block)
	image := percpuImage()
	for k := uintptr(0); k < percpuSize(); k++ {
		*(*byte)(unsafe.Add(b, k)) = *(*byte)(unsafe.Pointer(image + k))
	}
	block[0] = uint64(uintptr(b))
	x86.WriteGSBase(uintptr(b))
	rt.SetCPU(2)

	idt[0x80] = x86.InterruptGate(soft, 0x08, 0)
	idt[14] = x86.InterruptGate(pageFault, 0x08, 0)
	idtr = x86.NewDescriptorTablePointer(uintptr(unsafe.Pointer(&idt)), len(idt)*16)
	x86.Lidt(&idtr)

	println("scenario", scenario)
	switch scenario {
	case "explicit", "nested":
		panic("kernel " + scenario)
	case "bounds":
		println(get([]int{1, 2, 3}, 5))
	case "fault":
		softInt()
	case "print":
		idt[0x80] = x86.InterruptGate(printTrap, 0x08, 0)
		armed.Store(true)
		println("outer statement", 7)
		println("handler ran", printTraps.Load())
		panic("kernel print")
	}
	return 0
}

// armed says the next console write raises the print scenario's
// interrupt. It is set once printTrap is in the table, so earlier prints do
// not raise it, and cleared by the write that raises it. The handler's own
// print comes through trapInPrint too, so it is atomic.
var armed atomic.Bool

// printTraps counts the runs of printTrap; it is atomic because the
// handler is a party in the data race rules.
var printTraps atomic.Int64

// trapInPrint raises interrupt 0x80 at the first console write after the
// print scenario arms it: in the middle of a print statement, which holds the
// console lock.
func trapInPrint(p unsafe.Pointer) unsafe.Pointer {
	if armed.CompareAndSwap(true, false) {
		softInt()
	}
	return p
}

//pu:interrupt
func printTrap(f *x86.InterruptFrame) {
	printTraps.Add(1)
	println("handler output", 1)
}
