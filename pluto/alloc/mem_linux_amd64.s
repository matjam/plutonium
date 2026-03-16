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
# ABI-compliant wrapper around alloc.slabAlloc that saves and restores
# callee-saved registers.  The v0.1 Plutonium compiler does not emit
# callee-save code, so any caller relying on rbx/r12-r15 surviving a
# call would see corrupted state.  This wrapper provides the ABI
# guarantee at the package boundary.
#
# Calling convention: rdi = size.  Returns pointer in rax.
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
    # rdi already holds size
    call  alloc.slabAlloc
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
# Calling convention: rdi = ptr, rsi = size.
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
    # rdi and rsi already hold ptr and size
    call  alloc.slabFree
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
