# Plutonium runtime: x86-64 context switching and the atomic operations the
# scheduler and channels use (sched.p, chan.p). Package rt cannot import
# pluto/sync/atomic, so it has its own. Every function here is opaque to the
# compiler, so a call to one is a compiler barrier (memory-model.md 5.2).
#
# The saved context of a suspended task (abi.md section 12) is its
# stack pointer; below it on its stack lie the callee-saved registers and the
# address to resume at.
#pu:build amd64

	.cfi_sections .debug_frame
	.text

# swapcontext(save *uintptr, sp uintptr): push the callee-saved registers
# and RFLAGS, store the stack pointer in *save, load sp and pop what was
# saved there, then return into the code that saved it (or into goStart for
# a new context). Caller-saved registers, XMM included, are dead across a
# call under the internal ABI and need no saving. The direction flag is
# clear at every call, and Plutonium code does not change MXCSR or the x87
# control word. RFLAGS is saved so that, in a kernel, a task switched away
# from inside an interrupt handler (interrupts off) and one that parked
# with interrupts on each resume with their own interrupt flag.
	.globl "pluto/rt.swapcontext"
	.type "pluto/rt.swapcontext", @function
"pluto/rt.swapcontext":
	pushq	%rbp
	pushq	%rbx
	pushq	%r12
	pushq	%r13
	pushq	%r14
	pushq	%r15
	pushfq
	movq	%rsp, (%rdi)
	movq	%rsi, %rsp
	popfq
	popq	%r15
	popq	%r14
	popq	%r13
	popq	%r12
	popq	%rbx
	popq	%rbp
	ret

# goStart is where a new context begins (see newContext in sched.p): its
# first swapcontext pops RFLAGS, RBX, R12 and R13 from the initial frame and
# returns here with RSP 16-byte aligned. It calls the entry function, whose closure
# record is in R12, with RBX and R13 as its two arguments. The entry never
# returns. Debuggers stop unwinding here: the return address is undefined.
	.globl "pluto/rt.goStart"
	.type "pluto/rt.goStart", @function
"pluto/rt.goStart":
	.cfi_startproc
	.cfi_undefined rip
	movq	%rbx, %rdi
	movq	%r13, %rsi
	movq	%r12, %rax
	call	*(%rax)
	ud2
	.cfi_endproc

# goStartPC() uintptr: the address of goStart.
	.globl "pluto/rt.goStartPC"
	.type "pluto/rt.goStartPC", @function
"pluto/rt.goStartPC":
	leaq	"pluto/rt.goStart"(%rip), %rax
	ret

# getsp() uintptr: the caller's stack pointer.
	.globl "pluto/rt.getsp"
	.type "pluto/rt.getsp", @function
"pluto/rt.getsp":
	leaq	8(%rsp), %rax
	ret

# inc32(p *int32) and dec32(p *int32) change a counter that only the running
# task writes and only its own thread (or an interrupt or signal handler
# on it) reads, such as G.locks: no lock prefix, but a compiler barrier, so
# the change is in memory before the code it protects runs.
	.globl "pluto/rt.inc32"
	.type "pluto/rt.inc32", @function
"pluto/rt.inc32":
	incl	(%rdi)
	ret

	.globl "pluto/rt.dec32"
	.type "pluto/rt.dec32", @function
"pluto/rt.dec32":
	decl	(%rdi)
	ret

# load32(p *uint32) uint32
	.globl "pluto/rt.load32"
	.type "pluto/rt.load32", @function
"pluto/rt.load32":
	movl	(%rdi), %eax
	ret

# store32(p *uint32, v uint32): sequentially consistent.
	.globl "pluto/rt.store32"
	.type "pluto/rt.store32", @function
"pluto/rt.store32":
	xchgl	%esi, (%rdi)
	ret

# cas32(p *uint32, old, new uint32) bool
	.globl "pluto/rt.cas32"
	.type "pluto/rt.cas32", @function
"pluto/rt.cas32":
	movl	%esi, %eax
	lock cmpxchgl	%edx, (%rdi)
	sete	%al
	movzbl	%al, %eax
	ret

# xadd32(p *uint32, delta int32) uint32: adds delta and returns the new value.
	.globl "pluto/rt.xadd32"
	.type "pluto/rt.xadd32", @function
"pluto/rt.xadd32":
	movl	%esi, %eax
	lock xaddl	%eax, (%rdi)
	addl	%esi, %eax
	ret

# xchg32(p *uint32, v uint32) uint32: stores v and returns the old value.
	.globl "pluto/rt.xchg32"
	.type "pluto/rt.xchg32", @function
"pluto/rt.xchg32":
	movl	%esi, %eax
	xchgl	%eax, (%rdi)
	ret

# load64(p *uint64) uint64
	.globl "pluto/rt.load16"
	.type "pluto/rt.load16", @function
"pluto/rt.load16":
	movzwq	(%rdi), %rax
	ret

	.globl "pluto/rt.store16"
	.type "pluto/rt.store16", @function
"pluto/rt.store16":
	xchgw	%si, (%rdi)
	ret

	.globl "pluto/rt.load64"
	.type "pluto/rt.load64", @function
"pluto/rt.load64":
	movq	(%rdi), %rax
	ret

# store64(p *uint64, v uint64): sequentially consistent.
	.globl "pluto/rt.store64"
	.type "pluto/rt.store64", @function
"pluto/rt.store64":
	xchgq	%rsi, (%rdi)
	ret

# cas64(p *uint64, old, new uint64) bool
	.globl "pluto/rt.cas64"
	.type "pluto/rt.cas64", @function
"pluto/rt.cas64":
	movq	%rsi, %rax
	lock cmpxchgq	%rdx, (%rdi)
	sete	%al
	movzbl	%al, %eax
	ret

# xadd64(p *uint64, delta int64) uint64: adds delta and returns the new value.
	.globl "pluto/rt.xadd64"
	.type "pluto/rt.xadd64", @function
"pluto/rt.xadd64":
	movq	%rsi, %rax
	lock xaddq	%rax, (%rdi)
	addq	%rsi, %rax
	ret

# pause() is the spin-wait hint.
	.globl "pluto/rt.pause"
	.type "pluto/rt.pause", @function
"pluto/rt.pause":
	pause
	ret

	.section .note.GNU-stack,"",@progbits
