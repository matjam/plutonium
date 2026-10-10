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

# runOnStack(top uintptr): switch to the stack whose highest address is top
# and run the program there; mainOnStack never returns. RBP is cleared so
# that nothing walks past the new stack's first frame.
	.globl "pluto/rt.runOnStack"
	.type "pluto/rt.runOnStack", @function
"pluto/rt.runOnStack":
	movq	%rdi, %rsp
	andq	$-16, %rsp
	xorl	%ebp, %ebp
	call	"pluto/rt.mainOnStack"
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

# faultResume is where a fault that runs deferred calls resumes once the
# fault handler has returned (sigpanic): on the faulting task's stack,
# 16-byte aligned below the red zone, it raises the panic sigpanic
# described.
	.type "pluto/rt.faultResume", @function
"pluto/rt.faultResume":
	call	"pluto/rt.raiseFault"
	ud2

# faultResumePC() uintptr: the address of faultResume.
	.globl "pluto/rt.faultResumePC"
	.type "pluto/rt.faultResumePC", @function
"pluto/rt.faultResumePC":
	leaq	"pluto/rt.faultResume"(%rip), %rax
	ret

# sigreturn is the signal restorer the kernel requires (SA_RESTORER): the
# fault handler returns to it when the panic resumes on the task's stack
# (faultResume), as the preemption signal's handler does.
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
