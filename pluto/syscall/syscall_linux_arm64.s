//pu:build linux && arm64

// func Syscall(num, a1, a2, a3 uintptr) (r1, r2, err uintptr)
// aarch64 Linux syscall convention:
//   x8  = syscall number
//   x0-x5 = arguments
//   x0  = return value
TEXT ·Syscall(SB),NOSPLIT,$0-56
    MOVD num+0(FP),  R8
    MOVD a1+8(FP),   R0
    MOVD a2+16(FP),  R1
    MOVD a3+24(FP),  R2
    MOVD $0, R3
    MOVD $0, R4
    MOVD $0, R5
    SVC $0
    MOVD R0, r1+32(FP)
    MOVD $0, r2+40(FP)
    MOVD $0, err+48(FP)
    RET

// func Syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, err uintptr)
TEXT ·Syscall6(SB),NOSPLIT,$0-80
    MOVD num+0(FP),  R8
    MOVD a1+8(FP),   R0
    MOVD a2+16(FP),  R1
    MOVD a3+24(FP),  R2
    MOVD a4+32(FP),  R3
    MOVD a5+40(FP),  R4
    MOVD a6+48(FP),  R5
    SVC $0
    MOVD R0, r1+56(FP)
    MOVD $0, r2+64(FP)
    MOVD $0, err+72(FP)
    RET
