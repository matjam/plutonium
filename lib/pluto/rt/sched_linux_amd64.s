# Plutonium runtime: worker threads and thread-local storage for the hosted
# scheduler (sched_linux.p). The FS base of every thread the runtime runs
# points at its worker (an m): %fs:0 holds the m's own address and %fs:8 the
# puroutine it is running (abi.md section 12.3).
#pu:build linux && amd64

	.cfi_sections .debug_frame
	.text

# getm() *m: the current thread's worker.
	.globl "pluto/rt.getm"
	.type "pluto/rt.getm", @function
"pluto/rt.getm":
	movq	%fs:0, %rax
	ret

# getg() *G: the puroutine the current thread runs, or nil on its
# scheduling context.
	.globl "pluto/rt.getg"
	.type "pluto/rt.getg", @function
"pluto/rt.getg":
	movq	%fs:8, %rax
	ret

# cloneThread(flags, stack uintptr, mp unsafe.Pointer, fn unsafe.Pointer) int:
# starts a thread with clone(2) on the stack whose top is stack, with its FS
# base set to mp (CLONE_SETTLS). The new thread calls the closure record fn
# with mp as its argument; fn never returns. Returns the new thread's id, or
# a negated errno.
	.globl "pluto/rt.cloneThread"
	.type "pluto/rt.cloneThread", @function
"pluto/rt.cloneThread":
	# The child pops mp and fn from its new stack.
	subq	$16, %rsi
	movq	%rdx, 0(%rsi)
	movq	%rcx, 8(%rsi)
	movq	%rdx, %r8		# tls
	xorl	%edx, %edx		# parent_tid
	xorl	%r10d, %r10d		# child_tid
	movl	$56, %eax		# SYS_clone
	syscall
	testq	%rax, %rax
	jnz	1f
	# Child: RSP is the new stack, 16-byte aligned once mp and fn are off it.
	.cfi_startproc
	.cfi_undefined rip
	popq	%rdi
	popq	%rax
	xorl	%ebp, %ebp
	call	*(%rax)
	ud2
	.cfi_endproc
1:
	ret

	.section .note.GNU-stack,"",@progbits
