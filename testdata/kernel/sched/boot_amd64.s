# Boot code for the scheduler test kernel, the same as testdata/kernel/smoke:
# linked with pluto's default kernel linker script (internal/build/kernel.go),
# .boot runs at its physical address, 1 MiB, and the rest of the kernel is
# linked in the top 2 GiB. QEMU loads the flat image through the multiboot
# header and enters _start in 32-bit protected mode. This code maps the
# first GiB both at 0 and at 0xFFFFFFFF80000000, enters long mode with SSE
# left disabled (kernel-mode code uses none), and calls pluto.main in the
# high mapping.

	.set MB_MAGIC, 0x1BADB002
	.set MB_FLAGS, 0x00010000		# load addresses are in the header

	.section .multiboot, "a"
	.align 4
mb_header:
	.long MB_MAGIC
	.long MB_FLAGS
	.long -(MB_MAGIC + MB_FLAGS)
	.long mb_header
	.long __kernel_phys_start
	.long __kernel_load_end
	.long __kernel_phys_end
	.long _start

	.section .boot, "ax"
	.code32
	.globl _start
_start:
	cli
	movl	$boot_stack_top, %esp

	# PML4[0] and PML4[511] -> a PDPT each; PDPT_low[0] and
	# PDPT_high[510] -> one page directory of 2 MiB pages for the first GiB.
	movl	$pdpt_low, %eax
	orl	$3, %eax
	movl	%eax, pml4
	movl	$pdpt_high, %eax
	orl	$3, %eax
	movl	%eax, pml4 + 511*8
	movl	$pd, %eax
	orl	$3, %eax
	movl	%eax, pdpt_low
	movl	%eax, pdpt_high + 510*8
	xorl	%ecx, %ecx
1:	movl	%ecx, %eax
	shll	$21, %eax
	orl	$0x83, %eax
	movl	%eax, pd(,%ecx,8)
	incl	%ecx
	cmpl	$512, %ecx
	jne	1b
	movl	$pml4, %eax
	movl	%eax, %cr3

	# PAE only: CR4.OSFXSR stays clear, so SSE instructions raise #UD.
	movl	%cr4, %eax
	orl	$(1 << 5), %eax
	movl	%eax, %cr4

	# Long mode.
	movl	$0xC0000080, %ecx
	rdmsr
	orl	$(1 << 8), %eax
	wrmsr

	# Paging and protection on; no FPU emulation, monitor coprocessor.
	movl	%cr0, %eax
	andl	$~(1 << 2), %eax
	orl	$(1 << 31 | 1 << 1 | 1), %eax
	movl	%eax, %cr0

	lgdt	gdt_ptr
	ljmp	$0x08, $start64

	.code64
start64:
	movw	$0x10, %ax
	movw	%ax, %ds
	movw	%ax, %es
	movw	%ax, %ss
	movw	%ax, %fs
	movw	%ax, %gs
	movq	$boot_stack_top, %rsp
	cld
	movabsq	$pluto.main, %rax
	call	*%rax

	# Leave QEMU through the isa-debug-exit device: status (v << 1) | 1.
	movw	$0xf4, %dx
	outl	%eax, %dx
2:	cli
	hlt
	jmp	2b

	.section .boot.data, "a"
	.align 8
gdt:
	.quad	0
	.quad	0x00AF9A000000FFFF	# 64-bit code, selector 0x08
	.quad	0x00CF92000000FFFF	# data, selector 0x10
gdt_end:
gdt_ptr:
	.word	gdt_end - gdt - 1
	.quad	gdt

	.section .boot.bss, "aw", @nobits
	.align 4096
pml4:		.skip 4096
pdpt_low:	.skip 4096
pdpt_high:	.skip 4096
pd:		.skip 4096
	.skip 16384
boot_stack_top:

	.text

# percpuImage() uintptr: the virtual address of the .percpu image.
	.globl "main.percpuImage"
"main.percpuImage":
	movabsq	$__percpu_image, %rax
	ret

# percpuSize() uintptr: the size of a per-CPU block.
	.globl "main.percpuSize"
"main.percpuSize":
	movabsq	$__percpu_size, %rax
	ret

	.section .note.GNU-stack,"",@progbits
