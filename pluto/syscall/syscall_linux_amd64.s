//pu:build linux && amd64

// func Syscall(num, a1, a2, a3 uintptr) (r1, r2, err uintptr)
TEXT ·Syscall(SB),NOSPLIT,$0-56
    MOVQ num+0(FP),  AX
    MOVQ a1+8(FP),   DI
    MOVQ a2+16(FP),  SI
    MOVQ a3+24(FP),  DX
    XORQ R10, R10
    XORQ R8, R8
    XORQ R9, R9
    SYSCALL
    MOVQ AX, r1+32(FP)
    MOVQ DX, r2+40(FP)
    MOVQ $0, err+48(FP)
    RET

// func Syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, err uintptr)
TEXT ·Syscall6(SB),NOSPLIT,$0-80
    MOVQ num+0(FP),  AX
    MOVQ a1+8(FP),   DI
    MOVQ a2+16(FP),  SI
    MOVQ a3+24(FP),  DX
    MOVQ a4+32(FP),  R10
    MOVQ a5+40(FP),  R8
    MOVQ a6+48(FP),  R9
    SYSCALL
    MOVQ AX, r1+56(FP)
    MOVQ DX, r2+64(FP)
    MOVQ $0, err+72(FP)
    RET
