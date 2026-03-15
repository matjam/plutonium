#pu:build linux && amd64

# fmt.write(fd int64, ptr *byte, len int64) int64
#
# Performs the Linux write(2) syscall directly.
#
# Plutonium calling convention (System V AMD64 ABI):
#   rdi = fd
#   rsi = ptr
#   rdx = len
#
# Linux syscall convention:
#   rax = syscall number (1 = SYS_WRITE)
#   rdi = fd
#   rsi = buf
#   rdx = count
#
# The arguments are already in the correct registers for the syscall,
# so we just need to set rax and invoke syscall.

.text
.globl fmt.write
.type fmt.write, @function
fmt.write:
    movq $1, %rax       # SYS_WRITE = 1
    syscall
    ret
