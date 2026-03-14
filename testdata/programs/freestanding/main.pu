//pu:build freestanding

package main

// A freestanding program — no OS, no syscalls.
// This would be used as a kernel entry point.

// KernelMain is the entry point for a bare-metal kernel.
// In a real kernel, this would be called from assembly after
// setting up the stack and BSS.
func KernelMain() {
	// Initialize the arena allocator with a static buffer
	var arena [65536]byte
	_ = arena
	// ... kernel initialization code here
}
