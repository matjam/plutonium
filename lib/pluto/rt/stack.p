package rt

import "unsafe"

// stackLo and stackHi bound the main thread's stack: the hosted runtime sets
// them at start (to the stack it reserved for //pu:stack, or to the process
// stack and its resource limit), a freestanding program with SetStackBounds.
// stackLo is 0 if the lowest usable address is unknown.
var stackLo, stackHi uintptr

// stackSize is the size of the main thread's stack, for the message of a
// stack overflow: what //pu:stack asked for, the process stack's resource
// limit, or the bounds SetStackBounds was given. It is 0 if unknown.
var stackSize uintptr

// guardLo and guardHi bound the guard region below the stack the hosted
// runtime reserves for //pu:stack (zero otherwise), so that a fault there is
// reported as a stack overflow.
var guardLo, guardHi uintptr

// StackBounds returns the lowest and highest addresses of the main thread's
// stack, as far as the runtime knows them: lo is 0 if the stack's limit is
// unknown (an unlimited process stack, or a freestanding program that has
// not called SetStackBounds).
func StackBounds() (lo, hi uintptr) { return stackLo, stackHi }

// SetStackBounds records the bounds of the stack the program runs on, for a
// freestanding program that sets up its own stack.
func SetStackBounds(lo, hi uintptr) { stackLo, stackHi, stackSize = lo, hi, hi-lo }

// StackLeft returns how many bytes of stack remain below the caller's
// frame before the stack's limit, or ^uintptr(0) if that is unknown: the
// limit is unknown, or the caller is running neither on the main thread's
// stack nor on a task's (whose limit is its guard page). Deeply
// recursive code (a type checker walking nested types) calls it to stop
// with an error before it overflows the stack:
//
//	if rt.StackLeft() < 256<<10 {
//		return errTooDeep
//	}
//
//pu:noinline
func StackLeft() uintptr {
	var here byte
	sp := uintptr(unsafe.Pointer(&here))
	if t := current(); t != nil && t.stackLo != 0 && sp >= t.stackLo && sp < t.stackHi {
		return sp - t.stackLo
	}
	if stackLo == 0 || sp < stackLo || sp >= stackHi {
		return ^uintptr(0)
	}
	return sp - stackLo
}
