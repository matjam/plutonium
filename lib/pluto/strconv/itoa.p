// Package strconv converts between numbers and their text form.
//
// Itoa, FormatInt, FormatFloat, Quote and the other Format functions return
// strings, as in Go, allocated from the current allocator (format.p). The
// Append functions append to a caller's buffer instead and, like
// Plutonium's append, need room in its capacity: they never allocate.
//
// Parts of this package are adapted from Go's strconv package.
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.
package strconv

const digits = "0123456789abcdefghijklmnopqrstuvwxyz"

// AppendInt appends the text of i in the given base (2 to 36).
func AppendInt(dst []byte, i int64, base int) []byte {
	if i < 0 {
		dst = append(dst, '-')
		return AppendUint(dst, uint64(-i), base)
	}
	return AppendUint(dst, uint64(i), base)
}

// AppendUint appends the text of u in the given base (2 to 36).
func AppendUint(dst []byte, u uint64, base int) []byte {
	var buf [64]byte
	n := PutUint(buf[:], u, base)
	return append(dst, buf[len(buf)-n:]...)
}

// PutUint formats u in base right-aligned at the end of buf and returns the
// number of bytes used. buf must hold 64 bytes for base 2.
func PutUint(buf []byte, u uint64, base int) int {
	if base < 2 || base > 36 {
		panic("strconv: illegal base")
	}
	i := len(buf)
	b := uint64(base)
	if base == 10 {
		for u >= 100 {
			q := u / 100
			r := u - q*100
			i -= 2
			buf[i] = byte('0' + r/10)
			buf[i+1] = byte('0' + r%10)
			u = q
		}
		if u >= 10 {
			i -= 2
			buf[i] = byte('0' + u/10)
			buf[i+1] = byte('0' + u%10)
			return len(buf) - i
		}
		i--
		buf[i] = byte('0' + u)
		return len(buf) - i
	}
	for u >= b {
		i--
		buf[i] = digits[u%b]
		u /= b
	}
	i--
	buf[i] = digits[u]
	return len(buf) - i
}

// AppendBool appends "true" or "false".
func AppendBool(dst []byte, b bool) []byte {
	if b {
		return append(dst, "true"...)
	}
	return append(dst, "false"...)
}
