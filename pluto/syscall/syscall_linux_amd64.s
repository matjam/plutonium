# Plutonium syscall stubs for Linux x86_64
# Assembled with: as --64 -o syscall_linux_amd64.o syscall_linux_amd64.s
#
#pu:build linux && amd64

.text
.globl syscall.Syscall
.type syscall.Syscall, @function
syscall.Syscall:
    # Arguments: rdi=num, rsi=a1, rdx=a2, rcx=a3
    # Linux syscall convention: rax=num, rdi=a1, rsi=a2, rdx=a3
    movq %rdi, %rax       # syscall number
    movq %rsi, %rdi       # arg1
    movq %rdx, %rsi       # arg2
    movq %rcx, %rdx       # arg3
    syscall
    # Return: rax=r1 (already in rax)
    ret

.globl syscall.Syscall6
.type syscall.Syscall6, @function
syscall.Syscall6:
    # Arguments: rdi=num, rsi=a1, rdx=a2, rcx=a3, r8=a4, r9=a5
    #   6th arg is on stack at 8(%rsp) due to calling convention
    movq %rdi, %rax       # syscall number
    movq %rsi, %rdi       # arg1
    movq %rdx, %rsi       # arg2
    movq %rcx, %rdx       # arg3
    movq %r8, %r10        # arg4 (note: syscall uses r10 not rcx)
    movq %r9, %r8         # arg5
    movq 8(%rsp), %r9     # arg6 from stack
    syscall
    ret
