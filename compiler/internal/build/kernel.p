// Translated by go2pu from internal/build/kernel.go at the go-cutover tag; edited by hand since.

package build

import (
	"compiler/internal/backend"
)

// perCPUSelf is the symbol of the word at offset 0 of every per-CPU block.
const perCPUSelf = backend.PerCPUSelf

// perCPUTask is the current-task slot of kernel builds, at offset 8 of
// every per-CPU block (abi.md section 12.3).
const perCPUTask = backend.PerCPUTask

// KernelLinkerScript is the linker script for --mode kernel builds that do
// not pass --ldscript (abi.md section 10.3). It links the kernel in the
// top 2 GiB of the address space and loads it at 1 MiB physical:
//
//   - .boot (input sections .multiboot, then .boot and .boot.*) is linked
//     and loaded at physical 0x100000, for boot code that runs before
//     paging maps the kernel high. A multiboot header goes in .multiboot.
//   - .text, .rodata, .data and .bss follow at KERNEL_VMA plus their
//     physical address, so virtual = physical + 0xFFFFFFFF80000000.
//   - .percpu is linked at address 0 and loaded after .data, so each
//     per-CPU variable's address is its offset in a per-CPU block. The
//     kernel copies __percpu_size bytes from __percpu_image (the image's
//     virtual address) into each processor's block.
//
// The script defines __kernel_phys_start, __kernel_load_end (the physical
// end of the loaded image) and __kernel_phys_end (after .bss), __bss_start,
// __bss_end, __percpu_image, __percpu_load (its physical address) and
// __percpu_size. The program provides _start.
const KernelLinkerScript = `/* Plutonium default kernel linker script (abi.md section 10.3). */
ENTRY(_start)

KERNEL_VMA  = 0xFFFFFFFF80000000;
KERNEL_PHYS = 0x100000;

SECTIONS
{
	. = KERNEL_PHYS;
	__kernel_phys_start = .;

	.boot : {
		KEEP(*(.multiboot))
		*(.boot .boot.*)
	}

	. = ALIGN(4096) + KERNEL_VMA;
	.text : AT(ADDR(.text) - KERNEL_VMA) {
		*(.text .text.*)
	}

	. = ALIGN(4096);
	.rodata : AT(ADDR(.rodata) - KERNEL_VMA) {
		*(.rodata .rodata.*)
	}

	. = ALIGN(4096);
	.data : AT(ADDR(.data) - KERNEL_VMA) {
		*(.data .data.*)
	}

	. = ALIGN(64);
	__percpu_image = .;
	__percpu_load = . - KERNEL_VMA;
	.percpu 0 : AT(__percpu_load) {
		*(.percpu .percpu.*)
	}
	__percpu_size = SIZEOF(.percpu);
	. = __percpu_image + __percpu_size;
	__kernel_load_end = . - KERNEL_VMA;

	. = ALIGN(4096);
	.bss (NOLOAD) : AT(ADDR(.bss) - KERNEL_VMA) {
		__bss_start = .;
		*(.bss .bss.* COMMON)
		__bss_end = .;
	}
	__kernel_phys_end = . - KERNEL_VMA;

	/DISCARD/ : {
		*(.note.GNU-stack)
		*(.comment)
		*(.eh_frame)
	}
}
`
