package main

// contractTable lists the functions the generated code calls that the escape
// analysis does not apply at the call, and why that is sound. Each entry is
// reviewed: a function belongs here only if a call of it cannot keep a pointer
// into the caller's frame past the call, store one into memory that outlives
// the frame, or return one the analysis does not otherwise track. The
// runtime's own functions (pluto/rt) are reported apart, as Runtime, until
// the contract table shared with the lowerer (internal/escape2/contract)
// lists them.
var contractTable = map[string]string{
	// The default of an omitted allocator parameter (section 10.13). It
	// takes nothing from the caller's frame; what it returns, the task's
	// default allocator, is modelled by the analysis's local-arena rules
	// (internal/escape/arena.go), which cover calls that leave the
	// parameter out.
	"pluto/alloc.Default": "default allocator argument",
}
