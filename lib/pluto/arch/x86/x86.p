// Package x86 gives access to x86-64 features a kernel needs.
//
// Today it holds the interrupt handler support of abi.md section 10.4. The
// instruction intrinsics of abi.md section 10.5 are proposed (roadmap item 8).
package x86

// InterruptFrame is what the CPU pushes when it delivers an interrupt or
// exception, lowest address first. A //pu:interrupt handler receives a
// pointer to it; changes to it take effect when the handler returns.
type InterruptFrame struct {
	RIP, CS, RFLAGS, RSP, SS uint64
}

// HandlerAddr returns the entry address of the //pu:interrupt function h,
// for an IDT gate. The compiler expands it; h must name a handler, and
// anything else is a compile error.
func HandlerAddr(h any) uintptr {
	panic("x86.HandlerAddr must be called with a //pu:interrupt function")
}
