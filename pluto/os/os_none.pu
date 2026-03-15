//pu:build none

package os

// pluto/os is not available in bare-metal (OS=none) builds.
//
// In a kernel or bootloader, there is no operating system to service
// file descriptor operations. Use hardware-specific I/O instead:
//   - UART: write directly to memory-mapped registers
//   - Framebuffer: write to the framebuffer physical address
//   - Storage: use a hardware driver, not OS file I/O
//
// This file provides stub declarations so that code referencing
// pluto/os constants (Stdin, Stdout, Stderr) still compiles, but
// any attempt to call Write/Read/Open/Close/Exit will produce a
// clear error at compile time via the //pu:error directive.

// Write is not available in bare-metal builds.
//pu:error "os.Write is not available in freestanding builds; use hardware I/O directly"
func Write(fd int, p []byte) (int, error)

// Read is not available in bare-metal builds.
//pu:error "os.Read is not available in freestanding builds; use hardware I/O directly"
func Read(fd int, p []byte) (int, error)

// Open is not available in bare-metal builds.
//pu:error "os.Open is not available in freestanding builds; use hardware I/O directly"
func Open(name string, flags int, perm uint32) (int, error)

// Close is not available in bare-metal builds.
//pu:error "os.Close is not available in freestanding builds; use hardware I/O directly"
func Close(fd int) error

// Exit is not available in bare-metal builds.
// To halt a bare-metal system, use an architecture-specific instruction
// (HLT on x86, WFI on ARM) or reset via a hardware register.
//pu:error "os.Exit is not available in freestanding builds; halt the CPU directly"
func Exit(code int)
