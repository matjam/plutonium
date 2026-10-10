// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package strconv

import (
	"pluto/alloc"
	"pluto/unicode/utf8"
	"unsafe"
)

// A NumError records a failed conversion, as Go's does: the function that
// failed, the input, and the reason (usually ErrSyntax or ErrRange).
//
// The parse functions return the reason itself, so that parsing does not
// allocate; a caller that wants Go's *NumError makes one with NewNumError,
// which formats the message, "strconv.ParseFloat: parsing "1x": invalid
// syntax", once, into the same block as the NumError. Func and Num view that
// block, so the error stays valid however long the input lives. Release it
// with errors.Free (or by resetting the arena it came from).
type NumError struct {
	Func string // the failing function (ParseBool, ParseInt, ParseUint, ParseFloat, ParseComplex)
	Num  string // the input
	Err  error  // the reason the conversion failed (ErrRange, ErrSyntax, ...)

	msg  string
	a    alloc.Allocator // allocator that owns the NumError, or nil
	size uintptr
}

// Error returns "strconv.Func: parsing Num: Err", with Num quoted as a Go
// string. A NumError not made by NewNumError has no stored message and
// reports Err's message.
func (e *NumError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Func
}

// Unwrap returns the reason, so that errors.Is(err, strconv.ErrRange) works.
func (e *NumError) Unwrap() error { return e.Err }

// Free returns the NumError's memory to the allocator it came from. It does
// not free Err; errors.Free frees the whole chain.
func (e *NumError) Free() {
	if e.a == nil {
		return
	}
	var hdr NumError
	e.a.Free(unsafe.Pointer(e), e.size, unsafe.Alignof(hdr))
}

// NewNumError returns a NumError for the function fn failing on num with
// err, allocated with its message in one block from a, by default the
// default allocator. For example, NewNumError("ParseFloat", s, ErrSyntax)
// is the error Go's ParseFloat returns for a malformed s.
func NewNumError(fn, num string, err error, a alloc.Allocator = alloc.Default()) *NumError {
	var hdr NumError
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	const prefix = "strconv."
	const parsing = ": parsing "
	// Pass 1 counts the quoted input, pass 2 writes it.
	var q quoter
	q.quote(num)
	n := len(prefix) + len(fn) + len(parsing) + q.n + 2 + len(errMsg)
	// The block holds the NumError, a copy of num, then the message.
	off := unsafe.Sizeof(hdr)
	total := off + uintptr(len(num)) + uintptr(n)
	p := alloc.Raw(total, unsafe.Alignof(hdr), a)
	numText := unsafe.Slice((*byte)(unsafe.Add(p, off)), len(num))
	copy(numText, num)
	text := unsafe.Slice((*byte)(unsafe.Add(p, off+uintptr(len(num)))), n)
	k := copy(text, prefix)
	fnAt := k
	k += copy(text[k:], fn)
	k += copy(text[k:], parsing)
	q = quoter{buf: text[k : k+q.n]}
	q.quote(num)
	k += q.n
	text[k], text[k+1] = ':', ' '
	copy(text[k+2:], errMsg)
	msg := unsafe.String(unsafe.SliceData(text), n)
	e := (*NumError)(p)
	e.Func = msg[fnAt : fnAt+len(fn)]
	e.Num = unsafe.String(unsafe.SliceData(numText), len(num))
	e.Err = err
	e.msg = msg
	e.a = a
	e.size = total
	return e
}

// A quoter writes a double-quoted Go string literal into buf, or only counts
// the bytes it would write when buf is empty.
type quoter struct {
	buf []byte
	n   int
}

const (
	lowerhex = "0123456789abcdef"
)

func (q *quoter) put(b byte) {
	if q.n < len(q.buf) {
		q.buf[q.n] = b
	}
	q.n++
}

func (q *quoter) putString(s string) {
	for i := 0; i < len(s); i++ {
		q.put(s[i])
	}
}

// quote writes s as Strconv's Quote does: printable runes as they are, a
// backslash before " and \, escapes for the rest.
func (q *quoter) quote(s string) {
	q.put('"')
	for len(s) > 0 {
		r, width := utf8.DecodeRuneInString(s)
		if width == 1 && r == utf8.RuneError {
			q.putString(`\x`)
			q.put(lowerhex[s[0]>>4])
			q.put(lowerhex[s[0]&0xF])
		} else {
			q.escapedRune(r)
		}
		s = s[width:]
	}
	q.put('"')
}

func (q *quoter) escapedRune(r rune) {
	if r == '"' || r == '\\' { // always backslashed
		q.put('\\')
		q.put(byte(r))
		return
	}
	if IsPrint(r) {
		var enc [4]byte
		n := utf8.EncodeRune(enc[:], r)
		for i := 0; i < n; i++ {
			q.put(enc[i])
		}
		return
	}
	switch r {
	case '\a':
		q.putString(`\a`)
	case '\b':
		q.putString(`\b`)
	case '\f':
		q.putString(`\f`)
	case '\n':
		q.putString(`\n`)
	case '\r':
		q.putString(`\r`)
	case '\t':
		q.putString(`\t`)
	case '\v':
		q.putString(`\v`)
	default:
		switch {
		case r < ' ' || r == 0x7f:
			q.putString(`\x`)
			q.put(lowerhex[byte(r)>>4])
			q.put(lowerhex[byte(r)&0xF])
		case !utf8.ValidRune(r):
			r = 0xFFFD
			fallthrough
		case r < 0x10000:
			q.putString(`\u`)
			for s := 12; s >= 0; s -= 4 {
				q.put(lowerhex[r>>uint(s)&0xF])
			}
		default:
			q.putString(`\U`)
			for s := 28; s >= 0; s -= 4 {
				q.put(lowerhex[r>>uint(s)&0xF])
			}
		}
	}
}
