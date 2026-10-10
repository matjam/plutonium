# Plutonium runtime: x86-64 support for stack traces (trace.p).
#pu:build amd64

	.text

# callerFrame(out *[3]uintptr): the caller's PC and stack pointer as they
# will be once this returns, and its RBP.
	.globl "pluto/rt.callerFrame"
	.type "pluto/rt.callerFrame", @function
"pluto/rt.callerFrame":
	movq	(%rsp), %rax
	movq	%rax, 0(%rdi)
	leaq	8(%rsp), %rax
	movq	%rax, 8(%rdi)
	movq	%rbp, 16(%rdi)
	ret

# traceTable() unsafe.Pointer: the address of the compiler's trace table.
	.globl "pluto/rt.traceTable"
	.type "pluto/rt.traceTable", @function
"pluto/rt.traceTable":
	leaq	pluto.tracetab(%rip), %rax
	ret

	.section .note.GNU-stack,"",@progbits
