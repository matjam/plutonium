# Plutonium runtime: x86-64 atomic operations for the panic path (panic.p).
#pu:build amd64

	.text

# casUint64(addr *uint64, old, new uint64) bool: a sequentially consistent
# compare-and-swap.
	.globl "pluto/rt.casUint64"
	.type "pluto/rt.casUint64", @function
"pluto/rt.casUint64":
	movq	%rsi, %rax
	lock cmpxchgq	%rdx, (%rdi)
	sete	%al
	movzbl	%al, %eax
	ret

# loadUint64(addr *uint64) uint64: an atomic load (x86-64 loads are
# acquire loads).
	.globl "pluto/rt.loadUint64"
	.type "pluto/rt.loadUint64", @function
"pluto/rt.loadUint64":
	movq	(%rdi), %rax
	ret

# callDeferRunner(runner, rec uintptr): calls the defer record runner at
# code address runner, a func(rec unsafe.Pointer), with rec. A tail jump:
# the runner returns to callDeferRunner's caller.
	.globl "pluto/rt.callDeferRunner"
	.type "pluto/rt.callDeferRunner", @function
"pluto/rt.callDeferRunner":
	movq	%rdi, %rax
	movq	%rsi, %rdi
	jmp	*%rax

	.section .note.GNU-stack,"",@progbits
