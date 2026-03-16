#pu:build linux && amd64

# Slab allocator for the Plutonium runtime.
#
# Maintains power-of-2 size classes: 8, 16, 32, 64, 128, 256, 512, 1024, 2048
# Each size class has a free list (singly-linked list of free slots).
# When a free list is empty, allocates a 4096-byte page via mmap and carves
# it into slots.  Large allocations (> 2048 bytes) go directly to mmap.
# Deallocation pushes the slot back onto the free list.
#
# BSS layout of alloc.slabMeta (128 bytes, 8-byte aligned):
#   offset  0: free list head for class 0 (8-byte slots)
#   offset  8: free list head for class 1 (16-byte slots)
#   offset 16: free list head for class 2 (32-byte slots)
#   offset 24: free list head for class 3 (64-byte slots)
#   offset 32: free list head for class 4 (128-byte slots)
#   offset 40: free list head for class 5 (256-byte slots)
#   offset 48: free list head for class 6 (512-byte slots)
#   offset 56: free list head for class 7 (1024-byte slots)
#   offset 64: free list head for class 8 (2048-byte slots)
#   offset 72-127: padding / reserved

.bss
.globl alloc.slabMeta
.align 8
alloc.slabMeta:
    .zero 128

# ---- size class table (read-only) ----
.section .rodata
.align 8
alloc.slabSizes:
    .quad 8        # class 0
    .quad 16       # class 1
    .quad 32       # class 2
    .quad 64       # class 3
    .quad 128      # class 4
    .quad 256      # class 5
    .quad 512      # class 6
    .quad 1024     # class 7
    .quad 2048     # class 8

# --------------------------------------------------------------------------
# alloc.slabAlloc(size int64) *byte
#
# Allocates at least `size` bytes of zeroed memory.
# For size <= 2048, uses the slab free lists and zeroes the slot.
# For size > 2048, falls through to mmap directly (kernel zeroes pages).
#
# Calling convention (System V AMD64 ABI):
#   rdi = size
# Returns pointer in rax.
# --------------------------------------------------------------------------
.text
.globl alloc.slabAlloc
.type alloc.slabAlloc, @function
alloc.slabAlloc:
    pushq %rbp
    movq  %rsp, %rbp
    pushq %rbx
    pushq %r12
    pushq %r13
    pushq %r14

    movq  %rdi, %r12        # r12 = requested size

    # --- large allocation (> 2048): direct mmap (kernel zeroes) ---
    cmpq  $2048, %r12
    jle   .Lsmall

    # Round up to page boundary
    movq  %r12, %rdi
    addq  $4095, %rdi
    andq  $-4096, %rdi
    call  alloc.mmap
    jmp   .Ldone

.Lsmall:
    # --- determine size class ---
    # Minimum allocation is 8 bytes (class 0).
    # Classes: 0->8, 1->16, 2->32, 3->64, 4->128, 5->256, 6->512, 7->1024, 8->2048
    # class = max(0, bsr(max(size,8) - 1) - 2)

    movq  %r12, %rdi
    cmpq  $8, %rdi
    jge   .Lfind
    movq  $8, %rdi
.Lfind:
    # For sizes that are exact powers of 2, (size-1) gives the right bsr.
    # e.g. size=8: bsr(7)=2, class=2-2=0  -> slot=8  OK
    # size=16: bsr(15)=3, class=3-2=1     -> slot=16 OK
    # size=9: bsr(8)=3, class=3-2=1       -> slot=16 OK
    movq  %rdi, %rcx
    subq  $1, %rcx
    jz    .Lclass0            # size=1 -> class 0
    bsrq  %rcx, %rax
    subq  $2, %rax
    cmpq  $0, %rax
    jge   .Lclamp_hi
    xorq  %rax, %rax
.Lclamp_hi:
    cmpq  $8, %rax
    jle   .Lclass_set
    movq  $8, %rax
    jmp   .Lclass_set
.Lclass0:
    xorq  %rax, %rax
.Lclass_set:
    movq  %rax, %r13         # r13 = class index

    # rbx = slot size
    leaq  alloc.slabSizes(%rip), %rcx
    movq  (%rcx, %r13, 8), %rbx

    # r14 = pointer to slabMeta array
    leaq  alloc.slabMeta(%rip), %r14

    # --- check free list ---
    movq  (%r14, %r13, 8), %rax   # rax = free list head
    testq %rax, %rax
    jnz   .Lpop

    # --- carve a new page ---
    movq  $4096, %rdi
    call  alloc.mmap         # rax = page base
    movq  %rax, %rcx         # rcx = page base (saved)

    # nSlots = 4096 / slotSize
    # Save page pointer first — div clobbers rax and rdx.
    pushq %rcx               # save page base
    movq  $4096, %rax
    xorq  %rdx, %rdx
    divq  %rbx               # rax = nSlots
    movq  %rax, %rdi         # rdi = nSlots
    popq  %rcx               # restore page base

    # Build the free list by linking slots from last to first.
    # After this, the first slot is the new head.
    xorq  %rax, %rax         # rax = "next" pointer (NULL for last slot)

    # rsi = pointer to last slot = page + (nSlots - 1) * slotSize
    movq  %rdi, %rsi
    subq  $1, %rsi
    imulq %rbx, %rsi
    addq  %rcx, %rsi         # rsi = last slot address

.Lcarve_loop:
    movq  %rax, (%rsi)       # slot->next = previous head
    movq  %rsi, %rax         # new head = this slot
    subq  %rbx, %rsi         # move to previous slot
    cmpq  %rcx, %rsi         # compare with page base
    jge   .Lcarve_loop

    # rax = head of free list (first slot in page)
    movq  %rax, (%r14, %r13, 8)   # store as free list head

.Lpop:
    # Pop the head of the free list
    movq  (%r14, %r13, 8), %rax   # rax = head
    movq  (%rax), %rcx             # rcx = head->next
    movq  %rcx, (%r14, %r13, 8)   # update head

    # --- zero the slot before returning ---
    # rax = pointer to slot, rbx = slot size
    # We zero the full slot size (>= requested size).
    # Save rax (the return pointer) and use rep stosb.
    movq  %rax, %r12        # save return pointer in r12
    movq  %rax, %rdi        # rdi = destination for stosb
    movq  %rbx, %rcx        # rcx = count (slot size)
    xorb  %al, %al          # al = 0
    rep   stosb              # zero the slot
    movq  %r12, %rax        # restore return pointer

.Ldone:
    popq  %r14
    popq  %r13
    popq  %r12
    popq  %rbx
    popq  %rbp
    ret


# --------------------------------------------------------------------------
# alloc.slabFree(ptr *byte, size int64)
#
# Returns a previously allocated block to the slab free list.
# For blocks > 2048 bytes, calls munmap directly.
#
# Calling convention:
#   rdi = ptr
#   rsi = size
# --------------------------------------------------------------------------
.globl alloc.slabFree
.type alloc.slabFree, @function
alloc.slabFree:
    pushq %rbp
    movq  %rsp, %rbp

    # --- large allocation: direct munmap ---
    cmpq  $2048, %rsi
    jle   .Lfree_small

    # Round up to page boundary
    addq  $4095, %rsi
    andq  $-4096, %rsi
    call  alloc.munmap
    jmp   .Lfree_done

.Lfree_small:
    # Determine size class (same logic as slabAlloc)
    movq  %rsi, %rcx         # rcx = size
    cmpq  $8, %rcx
    jge   .Lfree_find
    movq  $8, %rcx
.Lfree_find:
    subq  $1, %rcx
    jz    .Lfree_class0
    bsrq  %rcx, %rax
    subq  $2, %rax
    cmpq  $0, %rax
    jge   .Lfree_clamp
    xorq  %rax, %rax
.Lfree_clamp:
    cmpq  $8, %rax
    jle   .Lfree_classok
    movq  $8, %rax
    jmp   .Lfree_classok
.Lfree_class0:
    xorq  %rax, %rax
.Lfree_classok:
    # rax = class index, rdi = ptr to free
    # Push ptr onto the free list for this class
    leaq  alloc.slabMeta(%rip), %rcx
    movq  (%rcx, %rax, 8), %rsi   # rsi = old head
    movq  %rsi, (%rdi)             # ptr->next = old head
    movq  %rdi, (%rcx, %rax, 8)   # slabMeta[class] = ptr

.Lfree_done:
    popq  %rbp
    ret


# --------------------------------------------------------------------------
# alloc.memzero(ptr *byte, size int64)
#
# Zeroes `size` bytes starting at `ptr`.
# Uses rep stosb for simplicity.
#
# Calling convention:
#   rdi = ptr
#   rsi = size (byte count)
# --------------------------------------------------------------------------
.globl alloc.memzero
.type alloc.memzero, @function
alloc.memzero:
    movq  %rsi, %rcx        # rcx = count
    xorb  %al, %al          # al = 0
    rep   stosb              # *rdi++ = al, rcx times
    ret
