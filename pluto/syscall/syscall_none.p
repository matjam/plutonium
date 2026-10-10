//pu:build none

package syscall

// On bare metal (OS=none) there is no operating system to call into.
// Importing pluto/syscall in a freestanding build is a compile error.
//
// If you need to perform hardware I/O in a kernel or bootloader, call
// the hardware directly via memory-mapped I/O or inline assembly.
//
// This file exists solely to produce a clear error message rather than
// a confusing "undefined: Syscall" linker error.

// Syscall is not available in freestanding (bare metal) builds.
// Use memory-mapped I/O or inline assembly to communicate with hardware.
func Syscall(num, a1, a2, a3 uintptr) (r1, r2, err uintptr) {
	// This body is intentionally unreachable.
	// The compiler should reject imports of pluto/syscall in freestanding mode.
	return 0, 0, 0
}

func Syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, err uintptr) {
	return 0, 0, 0
}
