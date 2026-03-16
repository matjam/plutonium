#pu:build linux && amd64

# alloc.mmap(size int64) *byte
#
# Allocates size bytes of zeroed, read-write memory via the Linux mmap(2)
# syscall.
#
# Plutonium calling convention (System V AMD64 ABI):
#   rdi = size
#
# Linux mmap syscall convention:
#   rax = syscall number (9 = SYS_MMAP)
#   rdi = addr   (NULL = 0, let kernel choose)
#   rsi = length
#   rdx = prot   (PROT_READ | PROT_WRITE = 3)
#   r10 = flags  (MAP_PRIVATE | MAP_ANONYMOUS = 0x22)
#   r8  = fd     (-1, unused with MAP_ANONYMOUS)
#   r9  = offset (0)
#
# Returns pointer in rax (MAP_FAILED = -1 on error).

.text
.globl alloc.mmap
.type alloc.mmap, @function
alloc.mmap:
    movq %rdi, %rsi       # rsi = length (size arg)
    xorq %rdi, %rdi       # rdi = addr = NULL
    movq $3, %rdx         # rdx = PROT_READ | PROT_WRITE
    movq $0x22, %r10      # r10 = MAP_PRIVATE | MAP_ANONYMOUS
    movq $-1, %r8         # r8  = fd = -1
    xorq %r9, %r9         # r9  = offset = 0
    movq $9, %rax         # rax = SYS_MMAP
    syscall
    ret

# alloc.munmap(ptr *byte, size int64)
#
# Releases memory previously allocated by mmap.
#
# Plutonium calling convention (System V AMD64 ABI):
#   rdi = ptr
#   rsi = size
#
# Linux munmap syscall convention:
#   rax = syscall number (11 = SYS_MUNMAP)
#   rdi = addr
#   rsi = length
#
# Arguments are already in the correct registers.

.globl alloc.munmap
.type alloc.munmap, @function
alloc.munmap:
    movq $11, %rax        # rax = SYS_MUNMAP
    syscall
    ret
