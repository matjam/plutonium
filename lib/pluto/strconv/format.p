package strconv

import (
	"pluto/alloc"
	"pluto/rt"
)

// The functions that return new strings (language spec section 18.8). Each
// formats into a buffer on the stack and copies the result into a string
// from the current allocator, or formats straight into a new string's
// buffer of the size it has measured; FormatBool, and Itoa and
// FormatInt for 0 to 99, return static strings and allocate nothing.

const smallsString = "00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

// small returns the static string of 0 <= i < 100.
func small(i int) string {
	if i < 10 {
		return digits[i : i+1]
	}
	return smallsString[i*2 : i*2+2]
}

// Itoa returns the decimal text of i, FormatInt(int64(i), 10).
func Itoa(i int) string { return FormatInt(int64(i), 10) }

// FormatInt returns the text of i in the given base (2 to 36), with
// lower-case letters for digits above 9.
func FormatInt(i int64, base int) string {
	if 0 <= i && i < 100 && base == 10 {
		return small(int(i))
	}
	var buf [65]byte
	n := putInt(buf[:], i, base)
	return rt.StringFromSlice(buf[len(buf)-n:], alloc.Default())
}

// FormatUint returns the text of u in the given base (2 to 36).
func FormatUint(u uint64, base int) string {
	if u < 100 && base == 10 {
		return small(int(u))
	}
	var buf [64]byte
	n := PutUint(buf[:], u, base)
	return rt.StringFromSlice(buf[len(buf)-n:], alloc.Default())
}

// FormatBool returns "true" or "false", static strings.
func FormatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
