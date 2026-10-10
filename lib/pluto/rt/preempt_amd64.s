# Plutonium runtime: asynchronous preemption outside kernel mode.
#
# rt.Preempt, called from a signal handler (hosted) or a timer interrupt
# handler, records the interrupted instruction's address in the puroutine
# and makes the interrupted code resume here instead, with every register as
# it was. This saves the whole register state on the puroutine's own stack,
# yields, and on resumption restores it and returns to the interrupted
# instruction. Every instruction is a safe point: nothing needs to know
# where the puroutine stopped.
#
# Layout below the interrupted RSP: 128 bytes of red zone, which C code
# compiled for hosted builds may use and which is left alone; the resume
# address; RFLAGS and the 15 general-purpose registers; then, 16-byte
# aligned, the 512-byte FXSAVE area holding the x87, MXCSR and XMM state.
#pu:build amd64 && !kernel

	.text

	.globl "pluto/rt.asyncPreempt"
	.type "pluto/rt.asyncPreempt", @function
"pluto/rt.asyncPreempt":
	leaq	-136(%rsp), %rsp	# lea leaves the flags alone
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
	movq	%rsp, %rbp		# the resume slot is at 128(%rbp)
	andq	$-16, %rsp
	subq	$512, %rsp
	fxsave64	(%rsp)
	cld
	movq	"pluto/rt.preemptParkFn"(%rip), %rax
	call	*(%rax)			# preemptPark() uintptr: yields, returns the resume address
	movq	%rax, 128(%rbp)
	fxrstor64	(%rsp)
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
	ret	$128			# pop the resume address, then skip the red zone

# asyncPreemptPC() uintptr: the address of asyncPreempt.
	.globl "pluto/rt.asyncPreemptPC"
	.type "pluto/rt.asyncPreemptPC", @function
"pluto/rt.asyncPreemptPC":
	leaq	"pluto/rt.asyncPreempt"(%rip), %rax
	ret

	.section .note.GNU-stack,"",@progbits
