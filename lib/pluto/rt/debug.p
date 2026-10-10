package rt

import "unsafe"

// Support for debugging tools such as the debug allocator
// (pluto/alloc/debug): where an allocation came from, how to print a
// program counter, and stopping the program after a diagnostic.

// CallerPC returns the return address of the function that calls it: a
// program counter just after the call instruction in that function's caller.
// The compiler implements it with a single instruction. Where that function
// is inlined, it returns the address of the inlined code instead, which lies
// at the same place in the caller. Called through a function value it
// returns 0.
func CallerPC() uintptr { return 0 }

// allocSite is the return address of the new or make (or pluto/alloc
// function) that is allocating or freeing, or 0.
var allocSite uintptr

// AllocSite returns the return address of the allocating or freeing call in
// progress, or 0 if it is not known: an allocator may record it as where
// memory was allocated or freed. When TrackSites is set, new and make set
// it, as do alloc.New, alloc.NewIn, alloc.Free and alloc.FreeIn; code that
// calls an Allocator's methods directly does not.
func AllocSite() uintptr { return allocSite }

// SetAllocSite publishes pc as the site of the allocation or free about to
// be made (0 once it is done). pluto/alloc uses it for its typed functions.
func SetAllocSite(pc uintptr) { allocSite = pc }

// A Symbolizer describes the code at pc: the function, and the file and line
// it was compiled from. It returns ok false if it does not know pc.
type Symbolizer func(pc uintptr) (fn string, file string, line int, ok bool)

var symbolizer Symbolizer

// SetSymbolizer registers the function PrintPC uses to name program
// counters. Stack trace support registers one; without it PrintPC prints
// bare addresses.
func SetSymbolizer(s Symbolizer) { symbolizer = s }

// PrintPC prints pc in hexadecimal and, if a symbolizer is registered and
// knows it, the function and source position: "0x401a2c in main.f
// (main.p:12)". For a return address, pass the address minus one, so the
// position is that of the call rather than of the line after it.
func PrintPC(pc uintptr) {
	printPointer(unsafe.Pointer(pc))
	if symbolizer == nil {
		return
	}
	fn, file, line, ok := symbolizer(pc)
	if !ok {
		return
	}
	printString(" in ")
	printString(fn)
	printString(" (")
	printString(file)
	printString(":")
	printInt(int64(line))
	printString(")")
}

// Abort stops the program the way a panic does, after the caller has
// printed its own message: in hosted builds the process exits with status
// 2 without running exit hooks; freestanding builds call the panic handler.
//
//pu:noreturn
func Abort() { abort() }
