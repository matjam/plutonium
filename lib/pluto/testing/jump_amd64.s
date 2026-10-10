# Non-local exit for package pluto/testing, x86-64.
#
# FailNow and SkipNow must leave the test function at once. Plutonium has no
# unwinder, so the test runner records its own frame with setjmp before
# calling the test, and FailNow returns to that point with longjmp. Only
# the callee-saved registers, the stack pointer and the return address are
# kept: the runner reads nothing else after the jump. Deferred calls of the
# abandoned frames do not run.
#pu:build amd64

	.text

# setjmp(b *jmpbuf) int: records the caller's frame in b and returns 0;
# returns again with 1 when longjmp(b) is called.
	.globl "pluto/testing.setjmp"
	.type "pluto/testing.setjmp", @function
"pluto/testing.setjmp":
	movq	%rbx, 0(%rdi)
	movq	%rbp, 8(%rdi)
	movq	%r12, 16(%rdi)
	movq	%r13, 24(%rdi)
	movq	%r14, 32(%rdi)
	movq	%r15, 40(%rdi)
	leaq	8(%rsp), %rdx
	movq	%rdx, 48(%rdi)
	movq	(%rsp), %rdx
	movq	%rdx, 56(%rdi)
	xorl	%eax, %eax
	ret

# longjmp(b *jmpbuf): resumes the frame recorded in b, making its setjmp
# return 1. It does not return.
	.globl "pluto/testing.longjmp"
	.type "pluto/testing.longjmp", @function
"pluto/testing.longjmp":
	movq	0(%rdi), %rbx
	movq	8(%rdi), %rbp
	movq	16(%rdi), %r12
	movq	24(%rdi), %r13
	movq	32(%rdi), %r14
	movq	40(%rdi), %r15
	movq	48(%rdi), %rsp
	movl	$1, %eax
	jmp	*56(%rdi)

	.section .note.GNU-stack,"",@progbits
