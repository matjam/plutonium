//pu:build amd64

// Package x86 gives access to the x86-64 instructions a kernel needs.
//
// The compiler expands every call to a function of this package inline,
// to the instruction or short sequence the function names (language spec
// section 12.4, abi.md section 10.5). Each expansion is a compiler
// barrier: no load or store is moved across it, and it is never removed,
// even when its result is unused. The functions also have assembly
// bodies (x86_amd64.s), used when one is called through a func value.
//
// Privileged instructions fault outside ring 0; only Pause, Cpuid, Rdtsc
// and Rdtscp are useful in a hosted program.
package x86

// A DescriptorTablePointer is the operand of Lgdt and Lidt: the table's
// limit (its size in bytes minus one) and its linear address.
type DescriptorTablePointer packed [10]byte {
	limit uint16
	base  uint64
}

// NewDescriptorTablePointer returns a pointer to a table of size bytes at
// base.
func NewDescriptorTablePointer(base uintptr, size int) DescriptorTablePointer {
	return DescriptorTablePointer{limit: uint16(size - 1), base: uint64(base)}
}

// InterruptFrame is what the CPU pushes when it delivers an interrupt or
// exception, lowest address first. A //pu:interrupt handler receives a
// pointer to it; changes to it take effect when the handler returns
// (abi.md section 10.4).
type InterruptFrame struct {
	RIP, CS, RFLAGS, RSP, SS uint64
}

// An FPUState holds the x87, MMX and SSE state that FXSAVE stores.
type FPUState aligned[[512]byte, 16]

// Cli disables maskable interrupts.
func Cli()

// Sti enables maskable interrupts.
func Sti()

// StiHlt enables interrupts and halts with no gap between the two, so an
// interrupt that arrives after Sti wakes the Hlt.
func StiHlt()

// Hlt halts the processor until the next interrupt.
func Hlt()

// Pause hints that the processor is in a spin-wait loop.
func Pause()

// SaveFlagsCli returns RFLAGS and then disables interrupts.
func SaveFlagsCli() uint64

// RestoreFlags restores RFLAGS saved by SaveFlagsCli, re-enabling
// interrupts if they were enabled.
func RestoreFlags(f uint64)

// Inb reads a byte from an I/O port.
func Inb(port uint16) uint8

// Inw reads 16 bits from an I/O port.
func Inw(port uint16) uint16

// Inl reads 32 bits from an I/O port.
func Inl(port uint16) uint32

// Outb writes a byte to an I/O port.
func Outb(port uint16, v uint8)

// Outw writes 16 bits to an I/O port.
func Outw(port uint16, v uint16)

// Outl writes 32 bits to an I/O port.
func Outl(port uint16, v uint32)

// Rdmsr reads a model-specific register.
func Rdmsr(msr uint32) uint64

// Wrmsr writes a model-specific register.
func Wrmsr(msr uint32, v uint64)

// ReadCR0 reads control register CR0.
func ReadCR0() uint64

// ReadCR2 reads CR2, the faulting address of the last page fault.
func ReadCR2() uint64

// ReadCR3 reads CR3, the page table root.
func ReadCR3() uint64

// ReadCR4 reads control register CR4.
func ReadCR4() uint64

// WriteCR0 writes control register CR0.
func WriteCR0(v uint64)

// WriteCR3 writes CR3, switching page tables (and flushing the TLB).
func WriteCR3(v uint64)

// WriteCR4 writes control register CR4.
func WriteCR4(v uint64)

// Invlpg invalidates the TLB entry for the page holding addr.
func Invlpg(addr uintptr)

// Lgdt loads the global descriptor table register.
//
//pu:noescape
func Lgdt(p *DescriptorTablePointer)

// Lidt loads the interrupt descriptor table register.
//
//pu:noescape
func Lidt(p *DescriptorTablePointer)

// Ltr loads the task register with a TSS selector.
func Ltr(sel uint16)

// Cpuid executes CPUID for leaf and subleaf.
func Cpuid(leaf, subleaf uint32) (eax, ebx, ecx, edx uint32)

// Rdtsc reads the time-stamp counter.
func Rdtsc() uint64

// Rdtscp reads the time-stamp counter and IA32_TSC_AUX.
func Rdtscp() (tsc uint64, aux uint32)

// Swapgs exchanges the GS base with IA32_KERNEL_GS_BASE.
func Swapgs()

// GSBaseMSR is IA32_GS_BASE.
const GSBaseMSR = 0xC0000101

// KernelGSBaseMSR is IA32_KERNEL_GS_BASE, which Swapgs exchanges with the
// GS base.
const KernelGSBaseMSR = 0xC0000102

// ReadGSBase reads the GS base (rdmsr of IA32_GS_BASE).
func ReadGSBase() uintptr

// WriteGSBase sets the GS base (wrmsr of IA32_GS_BASE).
func WriteGSBase(v uintptr)

// GSLoad64 reads 64 bits at offset off from the GS base.
func GSLoad64(off uintptr) uint64

// GSLoad32 reads 32 bits at offset off from the GS base.
func GSLoad32(off uintptr) uint32

// GSStore64 writes 64 bits at offset off from the GS base.
func GSStore64(off uintptr, v uint64)

// GSStore32 writes 32 bits at offset off from the GS base.
func GSStore32(off uintptr, v uint32)

// FPUBegin saves the FPU and SSE state into area and enables the FPU
// (clts; fxsave64), so that //pu:fpu functions may run in kernel mode.
// The state they start from is the interrupted context's, including its
// MXCSR and x87 control word.
//
//pu:noescape
func FPUBegin(area *FPUState)

// FPUEnd restores the state FPUBegin saved (fxrstor64).
//
//pu:noescape
func FPUEnd(area *FPUState)

// A Gate is one 16-byte entry of an interrupt descriptor table. The
// constructors InterruptGate and TrapGate are the way to make one that enters
// a handler (language spec section 20.8); a table is an array of Gate and
// Lidt loads its address. A Gate has no exported fields: putting a handler's
// address into the table as a raw word is unsafe code (section 20.1).
type Gate struct {
	offsetLow  uint16
	selector   uint16
	ist        uint8 // the interrupt stack table index, in the low three bits
	attr       uint8 // present, privilege level 0, and the gate type
	offsetMid  uint16
	offsetHigh uint32
	_          uint32
}

// InterruptGate returns the table entry that enters the //pu:interrupt
// function h through an interrupt gate: the processor clears the interrupt
// flag on entry, so the handler's body runs with maskable interrupts
// disabled. selector is the code segment selector the handler runs in and ist
// the interrupt stack table index (0 for none, or 1 to 7).
//
// The compiler expands the call and puts h's address in the entry. h must be
// the name of a //pu:interrupt function; a func value, an indirect use, a
// handler of the other form and any other argument are compile errors.
func InterruptGate(h any, selector uint16, ist uint8) Gate {
	panic("x86.InterruptGate must be called with a //pu:interrupt function")
}

// TrapGate returns the table entry that enters the //pu:interrupt trap
// function h through a trap gate: the processor leaves the interrupt flag as
// it was, so the handler can be entered again while it runs, and its body is
// not a region with interrupts disabled. The arguments are those of
// InterruptGate; h must be the name of a //pu:interrupt trap function.
func TrapGate(h any, selector uint16, ist uint8) Gate {
	panic("x86.TrapGate must be called with a //pu:interrupt trap function")
}

// gateAt builds the entry for the handler at addr. The compiler calls it
// from its expansion of InterruptGate (attr 0x8E: present, privilege level 0,
// interrupt gate) and TrapGate (attr 0x8F: a trap gate).
//
//pu:inline
func gateAt(addr uintptr, selector uint16, ist uint8, attr uint8) Gate {
	return Gate{
		offsetLow:  uint16(addr),
		selector:   selector,
		ist:        ist & 7,
		attr:       attr,
		offsetMid:  uint16(addr >> 16),
		offsetHigh: uint32(uint64(addr) >> 32),
	}
}
