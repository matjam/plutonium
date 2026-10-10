# Plutonium runtime: x86-64 support for string reference counts (str.p).
#pu:build amd64

	.text

# xadd64(p *int64, delta int64) int64: atomically adds delta to *p and
# returns the new value. The locked instruction is a full barrier, which
# gives the release and acquire ordering memory-model.md section 3.7 asks
# of the count.
	.globl "pluto/rt.xadd64"
	.type "pluto/rt.xadd64", @function
"pluto/rt.xadd64":
	movq	%rsi, %rax
	lock xaddq	%rax, (%rdi)
	addq	%rsi, %rax
	ret

	.section .note.GNU-stack,"",@progbits
