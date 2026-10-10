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

# sigreturn is the signal restorer the kernel requires (SA_RESTORER). The
# fault handler never returns, so it is never reached in practice.
	.type "pluto/rt.sigreturn", @function
"pluto/rt.sigreturn":
	movl	$15, %eax
	syscall
	ud2

# signalPCs(out *[2]uintptr): the addresses of the fault handler and of the
# restorer, for rt_sigaction.
	.globl "pluto/rt.signalPCs"
	.type "pluto/rt.signalPCs", @function
"pluto/rt.signalPCs":
	leaq	"pluto/rt.sigpanic"(%rip), %rax
	movq	%rax, 0(%rdi)
	leaq	"pluto/rt.sigreturn"(%rip), %rax
	movq	%rax, 8(%rdi)
	ret

	.section .note.GNU-stack,"",@progbits
