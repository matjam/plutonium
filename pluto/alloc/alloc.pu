//pu:build linux

package alloc

import "unsafe"

// Slab allocator for the Plutonium runtime.
//
// Maintains power-of-2 size classes: 8, 16, 32, 64, 128, 256, 512, 1024, 2048.
// Each size class has a free list (singly-linked list of free slots).
// When a free list is empty, allocates a 4096-byte page via mmap and carves
// it into slots.  Large allocations (> 2048 bytes) go directly to mmap.
// Deallocation pushes the slot back onto the free list.
//
// Free list head pointers are stored in a 128-byte BSS area (alloc.slabMeta)
// defined in mem_linux_amd64.s, accessed via the slabMetaAddr() helper.
//
// BSS layout of alloc.slabMeta (128 bytes, 8-byte aligned):
//   offset  0: free list head for class 0 (8-byte slots)
//   offset  8: free list head for class 1 (16-byte slots)
//   offset 16: free list head for class 2 (32-byte slots)
//   offset 24: free list head for class 3 (64-byte slots)
//   offset 32: free list head for class 4 (128-byte slots)
//   offset 40: free list head for class 5 (256-byte slots)
//   offset 48: free list head for class 6 (512-byte slots)
//   offset 56: free list head for class 7 (1024-byte slots)
//   offset 64: free list head for class 8 (2048-byte slots)
//
// NOTE: slabAlloc and slabFree are written as monolithic functions that
// only call assembly primitives (no Plutonium-to-Plutonium sub-calls).
// The v0.1 compiler does not emit callee-save code, so deep call chains
// between compiled functions can corrupt registers.  Keeping the logic
// flat avoids this issue.

// --- Assembly primitives (alloc_linux_amd64.s, mem_linux_amd64.s) ---

// mmap allocates size bytes of zeroed, read-write memory via the Linux
// mmap(2) syscall. Returns a pointer to the allocated region.
func mmap(size int64) *byte

// munmap releases size bytes of memory starting at ptr via the Linux
// munmap(2) syscall.
func munmap(ptr *byte, size int64)

// readPtr reads an 8-byte pointer value stored at address addr.
func readPtr(addr *byte) *byte

// writePtr writes an 8-byte pointer value val at address addr.
func writePtr(addr *byte, val *byte)

// memzero zeroes size bytes starting at ptr.
func memzero(ptr *byte, size int64)

// slabMetaAddr returns the address of the slab metadata BSS area.
func slabMetaAddr() *byte

// --- Slab allocator ---

// slabAlloc allocates at least size bytes of zeroed memory from the slab
// allocator. For sizes <= 2048, uses power-of-2 size class free lists and
// zeroes the slot before returning. For sizes > 2048, falls through to
// mmap directly (kernel zeroes pages).
func slabAlloc(size int64) *byte {
    // Large allocation: direct mmap (kernel zeroes pages)
    if size > 2048 {
        rounded := (size + 4095) & -4096
        return mmap(rounded)
    }

    // Determine size class (inline, no sub-call)
    var class int64
    if size <= 8 {
        class = 0
    } else if size <= 16 {
        class = 1
    } else if size <= 32 {
        class = 2
    } else if size <= 64 {
        class = 3
    } else if size <= 128 {
        class = 4
    } else if size <= 256 {
        class = 5
    } else if size <= 512 {
        class = 6
    } else if size <= 1024 {
        class = 7
    } else {
        class = 8
    }

    // Determine slot size (inline, no sub-call)
    var slotSize int64
    if class == 0 {
        slotSize = 8
    } else if class == 1 {
        slotSize = 16
    } else if class == 2 {
        slotSize = 32
    } else if class == 3 {
        slotSize = 64
    } else if class == 4 {
        slotSize = 128
    } else if class == 5 {
        slotSize = 256
    } else if class == 6 {
        slotSize = 512
    } else if class == 7 {
        slotSize = 1024
    } else {
        slotSize = 2048
    }

    // Compute free list head address (inline pointer arithmetic)
    offset := class * 8
    base := unsafe.ToUintptr(slabMetaAddr())
    headAddr := unsafe.FromUintptr(base + uintptr(offset))

    // Check if free list is empty
    head := readPtr(headAddr)

    if int64(unsafe.ToUintptr(head)) == 0 {
        // Free list empty — carve a new page
        page := mmap(4096)
        nSlots := 4096 / slotSize

        // Build the free list by linking slots from last to first.
        // Start with null as the "next" for the last slot.
        prev := unsafe.FromUintptr(uintptr(0))

        // i starts at the last slot and works backwards
        i := nSlots - 1
        for i >= 0 {
            slotOff := i * slotSize
            slotAddr := unsafe.ToUintptr(page) + uintptr(slotOff)
            slot := unsafe.FromUintptr(slotAddr)
            writePtr(slot, prev)
            prev = slot
            i = i - 1
        }

        // prev is now the first slot (head of the free list)
        writePtr(headAddr, prev)
        head = prev
    }

    // Pop the head of the free list
    next := readPtr(head)
    writePtr(headAddr, next)

    // Zero the slot before returning
    memzero(head, slotSize)

    return head
}

// slabFree returns a previously allocated block to the slab free list.
// For blocks > 2048 bytes, calls munmap directly.
func slabFree(ptr *byte, size int64) {
    // Large allocation: direct munmap
    if size > 2048 {
        rounded := (size + 4095) & -4096
        munmap(ptr, rounded)
        return
    }

    // Determine size class (inline, no sub-call)
    var class int64
    if size <= 8 {
        class = 0
    } else if size <= 16 {
        class = 1
    } else if size <= 32 {
        class = 2
    } else if size <= 64 {
        class = 3
    } else if size <= 128 {
        class = 4
    } else if size <= 256 {
        class = 5
    } else if size <= 512 {
        class = 6
    } else if size <= 1024 {
        class = 7
    } else {
        class = 8
    }

    // Compute free list head address (inline pointer arithmetic)
    offset := class * 8
    base := unsafe.ToUintptr(slabMetaAddr())
    headAddr := unsafe.FromUintptr(base + uintptr(offset))

    // Push ptr onto the free list
    oldHead := readPtr(headAddr)
    writePtr(ptr, oldHead)
    writePtr(headAddr, ptr)
}

// --- Public API ---

// newRaw allocates size bytes of zeroed memory and returns a pointer
// to the allocated region, with an 8-byte refcount header prepended.
// The returned pointer is offset +8 from the real allocation; the
// refcount at ptr-8 is initialized to 1.
// Implemented as an ABI-compliant wrapper in assembly (mem_linux_amd64.s).
func newRaw(size int64) *byte

// freeRaw releases memory at ptr of given size. Adjusts ptr back by 8
// and size up by 8 to account for the refcount header.
// Implemented as an ABI-compliant wrapper in assembly (mem_linux_amd64.s).
func freeRaw(ptr *byte, size int64)

// incRef increments the refcount at ptr-8.
// Called when a heap-owned pointer is copied to another variable.
// Implemented in assembly (mem_linux_amd64.s).
func incRef(ptr *byte)

// decRef decrements the refcount at ptr-8. If it reaches zero,
// the allocation is freed via slabFree. Otherwise, the count is
// stored back and no deallocation occurs.
// Implemented in assembly (mem_linux_amd64.s).
func decRef(ptr *byte, size int64)

// New allocates zeroed memory for a value of type T and returns a *T.
// This is a compiler intrinsic — the compiler replaces New[T]() with
// a call to newRaw(sizeof(T)) and casts the result to *T.
// The stub declaration allows the type checker to resolve alloc.New.
func New[T any]() *T

// Free releases memory previously allocated by New.
// This is a compiler intrinsic — the compiler replaces Free(ptr) with
// a call to decRef(ptr, sizeof(T)) where T is inferred from the pointer type.
// The stub declaration allows the type checker to resolve alloc.Free.
func Free(ptr *byte)
