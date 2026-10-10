# Plutonium runtime: calling the vDSO's clock_gettime (timer_linux.p).
#pu:build linux && amd64

	.text

# vdsoCall2(fn, a1, a2 uintptr) uintptr: calls the C function fn(a1, a2).
# The internal ABI passes the same registers and preserves the same ones
# as the C ABI, so only the stack needs aligning: the vDSO is compiled
# for a 16-byte aligned stack at each call.
	.globl "pluto/rt.vdsoCall2"
	.type "pluto/rt.vdsoCall2", @function
"pluto/rt.vdsoCall2":
	pushq	%rbp
	movq	%rsp, %rbp
	andq	$-16, %rsp
	movq	%rdi, %rax
	movq	%rsi, %rdi
	movq	%rdx, %rsi
	call	*%rax
	movq	%rbp, %rsp
	popq	%rbp
	ret

	.section .note.GNU-stack,"",@progbits
