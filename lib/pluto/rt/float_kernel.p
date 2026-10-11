//pu:build kernel

package rt

// In kernel mode floating point is unavailable outside //pu:fpu functions
// (language spec section 20.7), so print and println write a float as its
// bits in hexadecimal, as pluto/fmt does: float64=0x4020000000000000.
func printFloat(bits uint64, bitSize int) {
	if bitSize == 32 {
		printString("float32=")
	} else {
		printString("float64=")
	}
	writePointer(Console{}, uintptr(bits)) // hexadecimal with 0x
}

// stmtFloat is printFloat inside a print statement (rt.p).
func stmtFloat(bits uint64, bitSize int) {
	if bitSize == 32 {
		stmtString("float32=")
	} else {
		stmtString("float64=")
	}
	writePointer(stmtOut{}, uintptr(bits))
}
