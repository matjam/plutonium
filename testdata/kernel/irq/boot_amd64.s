# A minimal multiboot kernel: QEMU loads it as a flat binary at 1 MiB (the
# header's address fields say where), enters it in 32-bit protected mode,
# and this code switches to long mode and calls pluto.main. The helpers below
# give the Plutonium code port I/O, lidt and the interrupts to test.

	.set MB_MAGIC, 0x1BADB002
	.set MB_FLAGS, 0x00010000		# load addresses are in the header

	.section .multiboot, "a"
	.align 4
mb_header:
	.long MB_MAGIC
	.long MB_FLAGS
	.long -(MB_MAGIC + MB_FLAGS)
	.long mb_header
	.long __load_start
	.long __load_end
	.long __bss_end
	.long start32

	.section .boot, "ax"
	.code32
start32:
	cli
	movl	$boot_stack_top, %esp

	# Identity-map the first GiB with 2 MiB pages.
	movl	$pdpt, %eax
	orl	$3, %eax
	movl	%eax, pml4
	movl	$pd, %eax
	orl	$3, %eax
	movl	%eax, pdpt
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

	# PAE, and SSE enabled (OSFXSR, OSXMMEXCPT).
	movl	%cr4, %eax
	orl	$(1 << 5 | 1 << 9 | 1 << 10), %eax
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
	call	pluto.main

	# Leave QEMU through the isa-debug-exit device: status (v << 1) | 1.
	movw	$0xf4, %dx
	outl	%eax, %dx
2:	cli
	hlt
	jmp	2b

	.text

# outb(port uint16, v uint8)
	.globl "main.outb"
"main.outb":
	movw	%di, %dx
	movb	%sil, %al
	outb	%al, %dx
	ret

# lidt(p unsafe.Pointer)
	.globl "main.lidt"
"main.lidt":
	lidt	(%rdi)
	ret

# stackMod16() uint64: RSP modulo 16 at the call, which is 0 in code that
# keeps the ABI's alignment.
	.globl "main.stackMod16"
"main.stackMod16":
	leaq	8(%rsp), %rax
	andq	$15, %rax
	ret

# softInt() uint64 loads a known value into every register except RSP, sets
# the direction flag, executes int $0x80 and reports how many registers
# changed: 0 when the handler preserved them all.
	.globl "main.softInt"
"main.softInt":
	pushq	%rbx
	pushq	%rbp
	pushq	%r12
	pushq	%r13
	pushq	%r14
	pushq	%r15
	movq	%rsp, saved_rsp(%rip)
	movq	$0x0F0F, %rax
	movq	%rax, %xmm0
	movq	%rax, %xmm7
	movq	%rax, %xmm15
	movq	$0x11, %rax
	movq	$0x22, %rcx
	movq	$0x33, %rdx
	movq	$0x44, %rbx
	movq	$0x55, %rbp
	movq	$0x66, %rsi
	movq	$0x77, %rdi
	movq	$0x88, %r8
	movq	$0x99, %r9
	movq	$0xAA, %r10
	movq	$0xBB, %r11
	movq	$0xCC, %r12
	movq	$0xDD, %r13
	movq	$0xEE, %r14
	movq	$0xFF, %r15
	std
	int	$0x80
	pushfq
	popq	saved_flags(%rip)
	cld
	cmpq	$0x11, %rax
	jne	bad
	cmpq	$0x22, %rcx
	jne	bad
	cmpq	$0x33, %rdx
	jne	bad
	cmpq	$0x44, %rbx
	jne	bad
	cmpq	$0x55, %rbp
	jne	bad
	cmpq	$0x66, %rsi
	jne	bad
	cmpq	$0x77, %rdi
	jne	bad
	cmpq	$0x88, %r8
	jne	bad
	cmpq	$0x99, %r9
	jne	bad
	cmpq	$0xAA, %r10
	jne	bad
	cmpq	$0xBB, %r11
	jne	bad
	cmpq	$0xCC, %r12
	jne	bad
	cmpq	$0xDD, %r13
	jne	bad
	cmpq	$0xEE, %r14
	jne	bad
	cmpq	$0xFF, %r15
	jne	bad
	cmpq	saved_rsp(%rip), %rsp
	jne	bad
	movq	%xmm0, %rcx
	cmpq	$0x0F0F, %rcx
	jne	bad
	movq	%xmm7, %rcx
	cmpq	$0x0F0F, %rcx
	jne	bad
	movq	%xmm15, %rcx
	cmpq	$0x0F0F, %rcx
	jne	bad
	# The direction flag set before the interrupt is restored by iretq.
	testq	$0x400, saved_flags(%rip)
	jz	bad
	xorl	%eax, %eax
	jmp	done
bad:
	movl	$1, %eax
done:
	popq	%r15
	popq	%r14
	popq	%r13
	popq	%r12
	popq	%rbp
	popq	%rbx
	ret

# badSegment() loads a selector past the end of the GDT into DS, which
# raises #GP with the selector as its error code. The handler skips the
# 2-byte faulting instruction, so this returns normally.
	.globl "main.badSegment"
"main.badSegment":
	movl	$0x18, %ecx
	.byte	0x8E, 0xD9		# mov %ecx, %ds
	ret

	.section .rodata
	.align 8
gdt:
	.quad	0
	.quad	0x00AF9A000000FFFF	# 64-bit code, selector 0x08
	.quad	0x00CF92000000FFFF	# data, selector 0x10
gdt_end:
gdt_ptr:
	.word	gdt_end - gdt - 1
	.quad	gdt

	.bss
	.align 4096
pml4:	.skip 4096
pdpt:	.skip 4096
pd:	.skip 4096
	.align 16
	.skip 16384
boot_stack_top:
saved_rsp:	.skip 8
saved_flags:	.skip 8

	.section .note.GNU-stack,"",@progbits
