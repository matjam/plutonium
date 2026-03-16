//pu:build linux

package fmt

// write performs a raw write(2) syscall.
// Implemented in assembly (write_linux_amd64.s).
func write(fd int64, ptr *byte, len int64) int64

// Assembly helpers for accessing `any` values in variadic slices.
// Implemented in any_linux_amd64.s.
func anyElemPtr(base *byte, i int64) *byte
func anyTypeID(ptr *byte) int64
func anyData(ptr *byte) int64
func ptrLoad(addr *byte) int64
func ptrAdd(base *byte, offset int64) *byte

// Prints writes len bytes starting at ptr to stdout.
func Prints(ptr *byte, len int64) {
	write(1, ptr, len)
}

// Println writes a newline character to stdout.
func Println() {
	var nl byte
	nl = byte(10)
	write(1, &nl, 1)
}

// PrintByte writes a single byte to stdout.
func PrintByte(b byte) {
	write(1, &b, 1)
}

// PrintDigit writes a single decimal digit (0-9) to stdout.
func PrintDigit(d int64) {
	var ch byte
	ch = byte(48 + d)
	write(1, &ch, 1)
}

// printIntRec recursively prints decimal digits of n (most significant first).
// Precondition: n > 0.
func printIntRec(n int64) {
	if n == 0 {
		return
	}
	printIntRec(n / 10)
	PrintDigit(n % 10)
}

// PrintInt writes an integer in decimal to stdout.
func PrintInt(n int64) {
	if n == 0 {
		PrintDigit(0)
		return
	}
	if n < 0 {
		var minus byte
		minus = byte(45)
		write(1, &minus, 1)
		n = 0 - n
	}
	printIntRec(n)
}

// printUint writes an unsigned integer in decimal to stdout.
func printUint(n int64) {
	// Treat n as unsigned. For values that fit in int64, PrintInt works.
	// TODO: proper unsigned handling for values > MaxInt64
	if n == 0 {
		PrintDigit(0)
		return
	}
	printIntRec(n)
}

// printHex writes an integer in lowercase hexadecimal to stdout.
func printHex(n int64) {
	if n == 0 {
		PrintByte(48)
		return
	}
	printHexRec(n)
}

func printHexRec(n int64) {
	if n == 0 {
		return
	}
	printHexRec(n / 16)
	d := n % 16
	if d < 10 {
		PrintByte(byte(48 + d))
	} else {
		PrintByte(byte(87 + d))
	}
}

// Printf formats according to a format string and writes to stdout.
// Supports: %d (decimal int), %x (hex), %c (char), %s (string),
// %p (pointer), %% (literal %).
func Printf(format string, args ...any) {
	fmtLen := len(format)
	argIdx := int64(0)
	argsLen := len(args)

	i := int64(0)
	for i < fmtLen {
		ch := format[i]
		if ch == 37 {
			// '%' character
			i = i + 1
			if i >= fmtLen {
				// Trailing '%' — just print it
				PrintByte(37)
				return
			}
			verb := format[i]

			if verb == 100 {
				// %d — decimal integer
				if argIdx < argsLen {
					ap := anyElemPtr(args, argIdx)
					data := anyData(ap)
					PrintInt(data)
					argIdx = argIdx + 1
				}
			} else if verb == 120 {
				// %x — hex integer
				if argIdx < argsLen {
					ap := anyElemPtr(args, argIdx)
					data := anyData(ap)
					printHex(data)
					argIdx = argIdx + 1
				}
			} else if verb == 99 {
				// %c — character
				if argIdx < argsLen {
					ap := anyElemPtr(args, argIdx)
					data := anyData(ap)
					PrintByte(byte(data))
					argIdx = argIdx + 1
				}
		} else if verb == 115 {
			// %s — string
			if argIdx < argsLen {
				ap := anyElemPtr(args, argIdx)
				hdrPtr := anyData(ap)
				// hdrPtr is a pointer to a string header {ptr *byte, len int64}
				// Read the string pointer (first 8 bytes of header)
				strPtr := ptrLoad(hdrPtr)
				// Read the string length (second 8 bytes of header)
				strLen := ptrLoad(ptrAdd(hdrPtr, 8))
				Prints(strPtr, strLen)
				argIdx = argIdx + 1
			}
			} else if verb == 112 {
				// %p — pointer in hex with 0x prefix
				if argIdx < argsLen {
					ap := anyElemPtr(args, argIdx)
					data := anyData(ap)
					PrintByte(48)
					PrintByte(120)
					printHex(data)
					argIdx = argIdx + 1
				}
			} else if verb == 37 {
				// %% — literal %
				PrintByte(37)
			} else {
				// Unknown verb, print as-is
				PrintByte(37)
				PrintByte(verb)
			}
		} else {
			PrintByte(ch)
		}
		i = i + 1
	}
}
