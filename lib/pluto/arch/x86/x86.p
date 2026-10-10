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
func Lgdt(p *DescriptorTablePointer)

// Lidt loads the interrupt descriptor table register.
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
func FPUBegin(area *FPUState)

// FPUEnd restores the state FPUBegin saved (fxrstor64).
func FPUEnd(area *FPUState)

// HandlerAddr returns the entry address of the //pu:interrupt function h,
// for an IDT gate. The compiler expands it; h must name a handler, and
// anything else is a compile error. Through a func value it panics.
func HandlerAddr(h any) uintptr {
	panic("x86.HandlerAddr must be called with a //pu:interrupt function")
}
