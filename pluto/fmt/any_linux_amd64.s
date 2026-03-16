#pu:build linux && amd64

# Assembly helpers for accessing `any` values in variadic argument slices.
#
# An `any` value is 16 bytes: { typeID int64, data int64 }
# A variadic []any slice stores these contiguously on the stack.
#
# These helpers let Plutonium code read individual fields without
# pointer arithmetic or unsafe casts.

.text

# fmt.anyElemPtr(base *byte, i int64) *byte
# Returns a pointer to the i-th any element: base + i * 16
.globl fmt.anyElemPtr
.type fmt.anyElemPtr, @function
fmt.anyElemPtr:
    movq %rsi, %rax     # rax = i
    shlq $4, %rax       # rax = i * 16
    addq %rdi, %rax     # rax = base + i * 16
    ret

# fmt.anyTypeID(ptr *byte) int64
# Reads the typeID field (first 8 bytes) at ptr.
.globl fmt.anyTypeID
.type fmt.anyTypeID, @function
fmt.anyTypeID:
    movq (%rdi), %rax   # rax = *(int64*)ptr
    ret

# fmt.anyData(ptr *byte) int64
# Reads the data field (second 8 bytes) at ptr + 8.
.globl fmt.anyData
.type fmt.anyData, @function
fmt.anyData:
    movq 8(%rdi), %rax  # rax = *(int64*)(ptr + 8)
    ret

# fmt.ptrLoad(addr *byte) int64
# Reads 8 bytes (a 64-bit value) at the given address.
# Used to dereference pointers stored in memory (e.g., string headers).
.globl fmt.ptrLoad
.type fmt.ptrLoad, @function
fmt.ptrLoad:
    movq (%rdi), %rax   # rax = *(int64*)addr
    ret

# fmt.ptrAdd(base *byte, offset int64) *byte
# Returns base + offset. Pointer arithmetic helper.
.globl fmt.ptrAdd
.type fmt.ptrAdd, @function
fmt.ptrAdd:
    leaq (%rdi,%rsi), %rax  # rax = base + offset
    ret
