# Plutonium runtime: asynchronous preemption and interrupt masking in kernel
# mode. See preempt_amd64.s for how preemption works. Kernel-mode code
# outside //pu:fpu functions uses no vector registers (abi.md section 10.2),
# so only the general-purpose registers and RFLAGS are saved. A //pu:fpu
# region between x86.FPUBegin and x86.FPUEnd must run with preemption
# disabled (rt.DisablePreemption), since its vector state is not saved here.
#pu:build amd64 && kernel

	.text

	.globl "pluto/rt.asyncPreempt"
	.type "pluto/rt.asyncPreempt", @function
"pluto/rt.asyncPreempt":
	leaq	-136(%rsp), %rsp
	pushfq
	pushq	%rax
	pushq	%rbx
	pushq	%rcx
	pushq	%rdx
	pushq	%rsi
	pushq	%rdi
	pushq	%rbp
	pushq	%r8
	pushq	%r9
	pushq	%r10
	pushq	%r11
	pushq	%r12
	pushq	%r13
	pushq	%r14
	pushq	%r15
	movq	%rsp, %rbp
	andq	$-16, %rsp
	cld
	movq	"pluto/rt.preemptParkFn"(%rip), %rax
	call	*(%rax)
	movq	%rax, 128(%rbp)
	movq	%rbp, %rsp
	popq	%r15
	popq	%r14
	popq	%r13
	popq	%r12
	popq	%r11
	popq	%r10
	popq	%r9
	popq	%r8
	popq	%rbp
	popq	%rdi
	popq	%rsi
	popq	%rdx
	popq	%rcx
	popq	%rbx
	popq	%rax
	popfq
	ret	$128

	.globl "pluto/rt.asyncPreemptPC"
	.type "pluto/rt.asyncPreemptPC", @function
"pluto/rt.asyncPreemptPC":
	leaq	"pluto/rt.asyncPreempt"(%rip), %rax
	ret

# irqSave() uintptr: RFLAGS, then disable interrupts. rt.Lock uses it in
# kernel mode so that an interrupt handler on the same processor can take
# a runtime lock (a non-blocking channel operation) without deadlocking.
	.globl "pluto/rt.irqSave"
	.type "pluto/rt.irqSave", @function
"pluto/rt.irqSave":
	pushfq
	popq	%rax
	cli
	ret

# irqRestore(flags uintptr): restore the interrupt flag saved by irqSave.
	.globl "pluto/rt.irqRestore"
	.type "pluto/rt.irqRestore", @function
"pluto/rt.irqRestore":
	pushq	%rdi
	popfq
	ret

	.section .note.GNU-stack,"",@progbits
