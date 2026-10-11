package rt

import "unsafe"

// Support for debugging tools such as the debug allocator
// (pluto/alloc/debug): where an allocation came from, how to print a
// program counter. Stopping the program after a diagnostic (BeginPanic and
// Abort) is in panic.p.

// CallerPC returns the return address of the function that calls it: a
// program counter just after the call instruction in that function's caller.
// The compiler implements it with a single instruction. Where that function
// is inlined, it returns the address of the inlined code instead, which lies
// at the same place in the caller. Called through a function value it
// returns 0.
func CallerPC() uintptr { return 0 }

// allocSite is the return address of the new or make (or pluto/alloc
// function) that is allocating or freeing, or 0, for code that runs
// outside any task (a scheduler's own context). Each task has its own
// (Task.allocSite), since tasks allocate in parallel.
var allocSite uintptr

// AllocSite returns the return address of the allocating or freeing call in
// progress on the running task, or 0 if it is not known: an allocator may
// record it as where memory was allocated or freed. When TrackSites is
// set, new and make set it, as do alloc.New, alloc.Raw and alloc.Free;
// code that calls an Allocator's methods directly does not.
func AllocSite() uintptr {
	if t := current(); t != nil {
		return t.allocSite
	}
	return allocSite
}

// StackSite is the site AllocSite reports while the runtime allocates a
// task's stack. Such memory belongs to the runtime, which frees it
// when the task ends; leak reports leave it out.
const StackSite = 1

// SetAllocSite publishes pc as the site of the allocation or free about to
// be made (0 once it is done). pluto/alloc uses it for its typed functions.
// It is not inlined, so that the small functions that call it (alloc.Free)
// still inline into their callers and their rt.CallerPC names the call.
//
//pu:noinline
func SetAllocSite(pc uintptr) {
	if t := current(); t != nil {
		t.allocSite = pc
		return
	}
	allocSite = pc
}

// A Symbolizer describes the code at pc: the function, and the file and line
// it was compiled from. It returns ok false if it does not know pc.
type Symbolizer func(pc uintptr) (fn string, file string, line int, ok bool)

var symbolizer Symbolizer

// SetSymbolizer replaces the function PrintPC uses to name program
// counters; nil restores the default, which reads the trace table that
// stack traces use (docs/spec/debugging.md). A kernel that loads code the
// table does not describe can register its own.
func SetSymbolizer(s Symbolizer) { symbolizer = s }

// PrintPC prints the function and source position of the code at pc, as a
// stack trace names a frame: "main.f (main.p:12)". Where pc is in inlined
// code, the function is the inlined one. If the symbolizer does not know pc
// (the program was built with --trace=false, or pc is not in Plutonium
// code) it prints the address instead: "pc 0x401a2c". For a return
// address, pass the address minus one, so the position is that of the call
// rather than of the line after it.
func PrintPC(pc uintptr) {
	lookup := symbolizer
	if lookup == nil {
		lookup = traceSymbolize
	}
	fn, file, line, ok := lookup(pc)
	if !ok {
		printString("pc ")
		printPointer(unsafe.Pointer(pc))
		return
	}
	printString(fn)
	printString(" (")
	printString(file)
	printString(":")
	printInt(int64(line))
	printString(")")
}
