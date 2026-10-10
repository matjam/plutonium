# Atomic operations for package pluto/sync/atomic, x86-64.
#
# x86-64 is strongly ordered: plain loads are acquire loads and every locked
# instruction (and xchg with memory, which is implicitly locked) is a full
# barrier, so these sequences are sequentially consistent. Stores use xchg
# rather than mov so a store is not reordered with a later load.
#
# 32-bit results are returned sign-extended (int32) or zero-extended
# (uint32) in %rax; bool results are 0 or 1 in %rax.
#pu:build amd64

	.text

# LoadInt32(addr *int32) int32
	.globl "pluto/sync/atomic.LoadInt32"
	.type "pluto/sync/atomic.LoadInt32", @function
"pluto/sync/atomic.LoadInt32":
	movl	(%rdi), %eax
	movslq	%eax, %rax
	ret

# StoreInt32(addr *int32, val int32)
	.globl "pluto/sync/atomic.StoreInt32"
	.type "pluto/sync/atomic.StoreInt32", @function
"pluto/sync/atomic.StoreInt32":
	xchgl	%esi, (%rdi)
	ret

# AddInt32(addr *int32, delta int32) (new int32)
	.globl "pluto/sync/atomic.AddInt32"
	.type "pluto/sync/atomic.AddInt32", @function
"pluto/sync/atomic.AddInt32":
	movl	%esi, %eax
	lock xaddl	%eax, (%rdi)
	addl	%esi, %eax
	movslq	%eax, %rax
	ret

# SwapInt32(addr *int32, new int32) (old int32)
	.globl "pluto/sync/atomic.SwapInt32"
	.type "pluto/sync/atomic.SwapInt32", @function
"pluto/sync/atomic.SwapInt32":
	movl	%esi, %eax
	xchgl	%eax, (%rdi)
	movslq	%eax, %rax
	ret

# CompareAndSwapInt32(addr *int32, old, new int32) (swapped bool)
	.globl "pluto/sync/atomic.CompareAndSwapInt32"
	.type "pluto/sync/atomic.CompareAndSwapInt32", @function
"pluto/sync/atomic.CompareAndSwapInt32":
	movl	%esi, %eax
	lock cmpxchgl	%edx, (%rdi)
	sete	%al
	movzbl	%al, %eax
	ret

# AndInt32(addr *int32, mask int32) (old int32)
	.globl "pluto/sync/atomic.AndInt32"
	.type "pluto/sync/atomic.AndInt32", @function
"pluto/sync/atomic.AndInt32":
	movl	(%rdi), %eax
	1:
	movl	%eax, %ecx
	andl	%esi, %ecx
	lock cmpxchgl	%ecx, (%rdi)
	jne	1b
	movslq	%eax, %rax
	ret

# OrInt32(addr *int32, mask int32) (old int32)
	.globl "pluto/sync/atomic.OrInt32"
	.type "pluto/sync/atomic.OrInt32", @function
"pluto/sync/atomic.OrInt32":
	movl	(%rdi), %eax
	1:
	movl	%eax, %ecx
	orl	%esi, %ecx
	lock cmpxchgl	%ecx, (%rdi)
	jne	1b
	movslq	%eax, %rax
	ret

# LoadUint32(addr *uint32) uint32
	.globl "pluto/sync/atomic.LoadUint32"
	.type "pluto/sync/atomic.LoadUint32", @function
"pluto/sync/atomic.LoadUint32":
	movl	(%rdi), %eax
	movl	%eax, %eax
	ret

# StoreUint32(addr *uint32, val uint32)
	.globl "pluto/sync/atomic.StoreUint32"
	.type "pluto/sync/atomic.StoreUint32", @function
"pluto/sync/atomic.StoreUint32":
	xchgl	%esi, (%rdi)
	ret

# AddUint32(addr *uint32, delta uint32) (new uint32)
	.globl "pluto/sync/atomic.AddUint32"
	.type "pluto/sync/atomic.AddUint32", @function
"pluto/sync/atomic.AddUint32":
	movl	%esi, %eax
	lock xaddl	%eax, (%rdi)
	addl	%esi, %eax
	movl	%eax, %eax
	ret

# SwapUint32(addr *uint32, new uint32) (old uint32)
	.globl "pluto/sync/atomic.SwapUint32"
	.type "pluto/sync/atomic.SwapUint32", @function
"pluto/sync/atomic.SwapUint32":
	movl	%esi, %eax
	xchgl	%eax, (%rdi)
	movl	%eax, %eax
	ret

# CompareAndSwapUint32(addr *uint32, old, new uint32) (swapped bool)
	.globl "pluto/sync/atomic.CompareAndSwapUint32"
	.type "pluto/sync/atomic.CompareAndSwapUint32", @function
"pluto/sync/atomic.CompareAndSwapUint32":
	movl	%esi, %eax
	lock cmpxchgl	%edx, (%rdi)
	sete	%al
	movzbl	%al, %eax
	ret

# AndUint32(addr *uint32, mask uint32) (old uint32)
	.globl "pluto/sync/atomic.AndUint32"
	.type "pluto/sync/atomic.AndUint32", @function
"pluto/sync/atomic.AndUint32":
	movl	(%rdi), %eax
	1:
	movl	%eax, %ecx
	andl	%esi, %ecx
	lock cmpxchgl	%ecx, (%rdi)
	jne	1b
	movl	%eax, %eax
	ret

# OrUint32(addr *uint32, mask uint32) (old uint32)
	.globl "pluto/sync/atomic.OrUint32"
	.type "pluto/sync/atomic.OrUint32", @function
"pluto/sync/atomic.OrUint32":
	movl	(%rdi), %eax
	1:
	movl	%eax, %ecx
	orl	%esi, %ecx
	lock cmpxchgl	%ecx, (%rdi)
	jne	1b
	movl	%eax, %eax
	ret

# Load{Int64,Uint64,Uintptr,Pointer}(addr *T) T
	.globl "pluto/sync/atomic.LoadInt64"
	.type "pluto/sync/atomic.LoadInt64", @function
	.globl "pluto/sync/atomic.LoadUint64"
	.type "pluto/sync/atomic.LoadUint64", @function
	.globl "pluto/sync/atomic.LoadUintptr"
	.type "pluto/sync/atomic.LoadUintptr", @function
	.globl "pluto/sync/atomic.LoadPointer"
	.type "pluto/sync/atomic.LoadPointer", @function
"pluto/sync/atomic.LoadInt64":
"pluto/sync/atomic.LoadUint64":
"pluto/sync/atomic.LoadUintptr":
"pluto/sync/atomic.LoadPointer":
	movq	(%rdi), %rax
	ret

# Store{Int64,Uint64,Uintptr,Pointer}(addr *T, val T)
	.globl "pluto/sync/atomic.StoreInt64"
	.type "pluto/sync/atomic.StoreInt64", @function
	.globl "pluto/sync/atomic.StoreUint64"
	.type "pluto/sync/atomic.StoreUint64", @function
	.globl "pluto/sync/atomic.StoreUintptr"
	.type "pluto/sync/atomic.StoreUintptr", @function
	.globl "pluto/sync/atomic.StorePointer"
	.type "pluto/sync/atomic.StorePointer", @function
"pluto/sync/atomic.StoreInt64":
"pluto/sync/atomic.StoreUint64":
"pluto/sync/atomic.StoreUintptr":
"pluto/sync/atomic.StorePointer":
	xchgq	%rsi, (%rdi)
	ret

# Add{Int64,Uint64,Uintptr}(addr *T, delta T) (new T)
	.globl "pluto/sync/atomic.AddInt64"
	.type "pluto/sync/atomic.AddInt64", @function
	.globl "pluto/sync/atomic.AddUint64"
	.type "pluto/sync/atomic.AddUint64", @function
	.globl "pluto/sync/atomic.AddUintptr"
	.type "pluto/sync/atomic.AddUintptr", @function
"pluto/sync/atomic.AddInt64":
"pluto/sync/atomic.AddUint64":
"pluto/sync/atomic.AddUintptr":
	movq	%rsi, %rax
	lock xaddq	%rax, (%rdi)
	addq	%rsi, %rax
	ret

# Swap{Int64,Uint64,Uintptr,Pointer}(addr *T, new T) (old T)
	.globl "pluto/sync/atomic.SwapInt64"
	.type "pluto/sync/atomic.SwapInt64", @function
	.globl "pluto/sync/atomic.SwapUint64"
	.type "pluto/sync/atomic.SwapUint64", @function
	.globl "pluto/sync/atomic.SwapUintptr"
	.type "pluto/sync/atomic.SwapUintptr", @function
	.globl "pluto/sync/atomic.SwapPointer"
	.type "pluto/sync/atomic.SwapPointer", @function
"pluto/sync/atomic.SwapInt64":
"pluto/sync/atomic.SwapUint64":
"pluto/sync/atomic.SwapUintptr":
"pluto/sync/atomic.SwapPointer":
	movq	%rsi, %rax
	xchgq	%rax, (%rdi)
	ret

# CompareAndSwap{Int64,Uint64,Uintptr,Pointer}(addr *T, old, new T) (swapped bool)
	.globl "pluto/sync/atomic.CompareAndSwapInt64"
	.type "pluto/sync/atomic.CompareAndSwapInt64", @function
	.globl "pluto/sync/atomic.CompareAndSwapUint64"
	.type "pluto/sync/atomic.CompareAndSwapUint64", @function
	.globl "pluto/sync/atomic.CompareAndSwapUintptr"
	.type "pluto/sync/atomic.CompareAndSwapUintptr", @function
	.globl "pluto/sync/atomic.CompareAndSwapPointer"
	.type "pluto/sync/atomic.CompareAndSwapPointer", @function
"pluto/sync/atomic.CompareAndSwapInt64":
"pluto/sync/atomic.CompareAndSwapUint64":
"pluto/sync/atomic.CompareAndSwapUintptr":
"pluto/sync/atomic.CompareAndSwapPointer":
	movq	%rsi, %rax
	lock cmpxchgq	%rdx, (%rdi)
	sete	%al
	movzbl	%al, %eax
	ret

# And{Int64,Uint64,Uintptr}(addr *T, mask T) (old T)
	.globl "pluto/sync/atomic.AndInt64"
	.type "pluto/sync/atomic.AndInt64", @function
	.globl "pluto/sync/atomic.AndUint64"
	.type "pluto/sync/atomic.AndUint64", @function
	.globl "pluto/sync/atomic.AndUintptr"
	.type "pluto/sync/atomic.AndUintptr", @function
"pluto/sync/atomic.AndInt64":
"pluto/sync/atomic.AndUint64":
"pluto/sync/atomic.AndUintptr":
	movq	(%rdi), %rax
	1:
	movq	%rax, %rcx
	andq	%rsi, %rcx
	lock cmpxchgq	%rcx, (%rdi)
	jne	1b
	ret

# Or{Int64,Uint64,Uintptr}(addr *T, mask T) (old T)
	.globl "pluto/sync/atomic.OrInt64"
	.type "pluto/sync/atomic.OrInt64", @function
	.globl "pluto/sync/atomic.OrUint64"
	.type "pluto/sync/atomic.OrUint64", @function
	.globl "pluto/sync/atomic.OrUintptr"
	.type "pluto/sync/atomic.OrUintptr", @function
"pluto/sync/atomic.OrInt64":
"pluto/sync/atomic.OrUint64":
"pluto/sync/atomic.OrUintptr":
	movq	(%rdi), %rax
	1:
	movq	%rax, %rcx
	orq	%rsi, %rcx
	lock cmpxchgq	%rcx, (%rdi)
	jne	1b
	ret

# Fence()
	.globl "pluto/sync/atomic.Fence"
	.type "pluto/sync/atomic.Fence", @function
"pluto/sync/atomic.Fence":
	mfence
	ret

	.section .note.GNU-stack,"",@progbits
