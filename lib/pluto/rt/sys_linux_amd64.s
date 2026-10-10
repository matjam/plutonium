# Plutonium runtime: Linux x86-64 entry point and system calls.
#pu:build linux && amd64

	.text

# _start: the kernel enters here with %rsp pointing at argc.
	.globl _start
	.type _start, @function
_start:
	xorl	%ebp, %ebp
	movq	%rsp, %rdi
	andq	$-16, %rsp
	call	"pluto/rt.start"
	ud2

# syscall3(num, a1, a2, a3 uintptr) uintptr
	.globl "pluto/rt.syscall3"
	.type "pluto/rt.syscall3", @function
"pluto/rt.syscall3":
	movq	%rdi, %rax
	movq	%rsi, %rdi
	movq	%rdx, %rsi
	movq	%rcx, %rdx
	syscall
	ret

# syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) uintptr
# a6 arrives on the stack (the seventh integer argument).
	.globl "pluto/rt.syscall6"
	.type "pluto/rt.syscall6", @function
"pluto/rt.syscall6":
	movq	%rdi, %rax
	movq	%rsi, %rdi
	movq	%rdx, %rsi
	movq	%rcx, %rdx
	movq	%r8, %r10
	movq	%r9, %r8
	movq	8(%rsp), %r9
	syscall
	ret

# sigtramp is the SIGSEGV handler. The kernel enters it as a call with
# (signo, siginfo, ucontext) in %rdi, %rsi, %rdx, so it passes straight on
# to sigfault.
	.type sigtramp, @function
sigtramp:
	jmp	"pluto/rt.sigfault"

# sigreturn is the signal restorer: a handler that returns lands here.
	.type sigreturn, @function
sigreturn:
	movl	$15, %eax	# rt_sigreturn
	syscall
	ud2

# sigtrampAddr() uintptr
	.globl "pluto/rt.sigtrampAddr"
	.type "pluto/rt.sigtrampAddr", @function
"pluto/rt.sigtrampAddr":
	leaq	sigtramp(%rip), %rax
	ret

# sigreturnAddr() uintptr
	.globl "pluto/rt.sigreturnAddr"
	.type "pluto/rt.sigreturnAddr", @function
"pluto/rt.sigreturnAddr":
	leaq	sigreturn(%rip), %rax
	ret

	.section .note.GNU-stack,"",@progbits
