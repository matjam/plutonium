//pu:build kernel

package rt

// In kernel mode floating point is unavailable outside //pu:fpu functions
// (language spec section 12.3), so print and println write a float as its
// bits in hexadecimal, as pluto/fmt does: float64=0x4020000000000000.
func printFloat(bits uint64, bitSize int) {
	if bitSize == 32 {
		printString("float32=")
	} else {
		printString("float64=")
	}
	writePointer(nil, uintptr(bits)) // hexadecimal with 0x
}
