// Plutonium syscall stubs for Linux aarch64
// Assembled with: as -o syscall_linux_arm64.o syscall_linux_arm64.s
//
//pu:build linux && arm64

.text
.globl syscall.Syscall
.type syscall.Syscall, %function
syscall.Syscall:
    // Arguments: x0=num, x1=a1, x2=a2, x3=a3
    // Linux syscall convention: x8=num, x0-x5=args
    mov x8, x0            // syscall number
    mov x0, x1            // arg1
    mov x1, x2            // arg2
    mov x2, x3            // arg3
    svc #0
    // Return: x0=r1 (already in x0)
    ret

.globl syscall.Syscall6
.type syscall.Syscall6, %function
syscall.Syscall6:
    // Arguments: x0=num, x1=a1, x2=a2, x3=a3, x4=a4, x5=a5, x6=a6
    //   7th arg is on stack at [sp] due to calling convention
    mov x8, x0            // syscall number
    mov x0, x1            // arg1
    mov x1, x2            // arg2
    mov x2, x3            // arg3
    mov x3, x4            // arg4
    mov x4, x5            // arg5
    ldr x5, [sp]          // arg6 from stack
    svc #0
    ret
