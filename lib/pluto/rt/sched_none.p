//pu:build none

package rt

// Freestanding builds have no backend of their own: the program binds one
// with //pu:scheduler (a kernel's, abi.md section 12).

// panicThread identifies the processor that is panicking. Freestanding
// builds cannot tell processors apart, so a second panic always stops at
// once.
func panicThread() uintptr { return 1 }

// otherPanic is called by a second panic while one is printed elsewhere.
//
//pu:noreturn
func otherPanic() { abort() }

// flushStackCache has nothing to do: backends own their stacks.
func flushStackCache() {}
