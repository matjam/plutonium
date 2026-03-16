#pu:build linux && amd64

# Minimal memory access primitives and BSS storage for the slab allocator.
#
# These are the only operations that cannot be expressed in Plutonium:
# reading/writing raw pointer values at arbitrary addresses, fast
# bulk zeroing via rep stosb, and BSS storage for slab metadata.

# ---- BSS: slab metadata (9 free list head pointers) ----
.bss
.globl alloc.slabMeta
.align 8
alloc.slabMeta:
    .zero 128

# ---- text section ----
.text

# --------------------------------------------------------------------------
# alloc.newRaw(size int64) *byte
#
# ABI-compliant wrapper that allocates size+8 bytes via slabAlloc,
# writes refcount=1 into the first 8 bytes, and returns ptr+8
# (the user-visible pointer past the refcount header).
#
# Memory layout:
#   [refcount: 8 bytes][user data: size bytes]
#                       ^
#                       returned pointer
#
# Calling convention: rdi = size.  Returns user pointer in rax.
# --------------------------------------------------------------------------
.globl alloc.newRaw
.type alloc.newRaw, @function
alloc.newRaw:
    pushq %rbp
    movq  %rsp, %rbp
    pushq %rbx
    pushq %r12
    pushq %r13
    pushq %r14
    pushq %r15
    addq  $8, %rdi        # rdi = size + 8 (include refcount header)
    call  alloc.slabAlloc
    # rax = raw pointer; set refcount to 1
    movq  $1, (%rax)       # *rax = 1 (refcount)
    addq  $8, %rax         # return user pointer (past header)
    popq  %r15
    popq  %r14
    popq  %r13
    popq  %r12
    popq  %rbx
    popq  %rbp
    ret

# --------------------------------------------------------------------------
# alloc.freeRaw(ptr *byte, size int64)
#
# ABI-compliant wrapper around alloc.slabFree.
# Adjusts ptr back by 8 and size up by 8 to account for refcount header.
# Calling convention: rdi = ptr (user pointer), rsi = size (user size).
# --------------------------------------------------------------------------
.globl alloc.freeRaw
.type alloc.freeRaw, @function
alloc.freeRaw:
    pushq %rbp
    movq  %rsp, %rbp
    pushq %rbx
    pushq %r12
    pushq %r13
    pushq %r14
    pushq %r15
    subq  $8, %rdi        # rdi = ptr - 8 (real allocation start)
    addq  $8, %rsi        # rsi = size + 8 (include refcount header)
    call  alloc.slabFree
    popq  %r15
    popq  %r14
    popq  %r13
    popq  %r12
    popq  %rbx
    popq  %rbp
    ret

# --------------------------------------------------------------------------
# alloc.incRef(ptr *byte)
#
# Increments the refcount at ptr-8.
# ABI-compliant wrapper that saves/restores callee-saved registers.
# Calling convention: rdi = user pointer.
# --------------------------------------------------------------------------
.globl alloc.incRef
.type alloc.incRef, @function
alloc.incRef:
    movq -8(%rdi), %rax   # rax = refcount at ptr-8
    addq $1, %rax          # rax++
    movq %rax, -8(%rdi)    # store back
    ret

# --------------------------------------------------------------------------
# alloc.decRef(ptr *byte, size int64)
#
# Decrements the refcount at ptr-8. If it reaches zero, frees the
# allocation (at ptr-8, size+8) via slabFree. Otherwise, just stores
# the decremented count.
# Calling convention: rdi = user pointer, rsi = user data size.
# --------------------------------------------------------------------------
.globl alloc.decRef
.type alloc.decRef, @function
alloc.decRef:
    pushq %rbp
    movq  %rsp, %rbp
    pushq %rbx
    pushq %r12
    pushq %r13
    pushq %r14
    pushq %r15
    movq  -8(%rdi), %rax   # rax = refcount at ptr-8
    subq  $1, %rax          # rax--
    jnz   .Ldecref_nofree   # if non-zero, skip free
    # refcount hit zero — free the allocation
    subq  $8, %rdi          # rdi = ptr - 8 (real allocation start)
    addq  $8, %rsi          # rsi = size + 8 (include refcount header)
    call  alloc.slabFree
    jmp   .Ldecref_done
.Ldecref_nofree:
    movq  %rax, -8(%rdi)    # store decremented refcount
.Ldecref_done:
    popq  %r15
    popq  %r14
    popq  %r13
    popq  %r12
    popq  %rbx
    popq  %rbp
    ret

# --------------------------------------------------------------------------
# alloc.slabMetaAddr() *byte
#
# Returns the address of the slab metadata BSS area.
# Calling convention: returns pointer in rax.
# --------------------------------------------------------------------------
.globl alloc.slabMetaAddr
.type alloc.slabMetaAddr, @function
alloc.slabMetaAddr:
    leaq alloc.slabMeta(%rip), %rax
    ret

# --------------------------------------------------------------------------
# alloc.readPtr(addr *byte) *byte
#
# Reads an 8-byte pointer value stored at address addr.
# Calling convention: rdi = addr. Returns pointer in rax.
# --------------------------------------------------------------------------
.globl alloc.readPtr
.type alloc.readPtr, @function
alloc.readPtr:
    movq (%rdi), %rax
    ret

# --------------------------------------------------------------------------
# alloc.writePtr(addr *byte, val *byte)
#
# Writes an 8-byte pointer value val at address addr.
# Calling convention: rdi = addr, rsi = val.
# --------------------------------------------------------------------------
.globl alloc.writePtr
.type alloc.writePtr, @function
alloc.writePtr:
    movq %rsi, (%rdi)
    ret

# --------------------------------------------------------------------------
# alloc.memzero(ptr *byte, size int64)
#
# Zeroes size bytes starting at ptr. Uses rep stosb.
# Calling convention: rdi = ptr, rsi = size.
# --------------------------------------------------------------------------
.globl alloc.memzero
.type alloc.memzero, @function
alloc.memzero:
    movq %rsi, %rcx
    xorb %al, %al
    rep stosb
    ret
