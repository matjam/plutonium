// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package strconv

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/unicode/utf8"
	"unsafe"
)

// A NumError records a failed conversion, as Go's does: the function that
// failed, the input, and the reason (usually ErrSyntax or ErrRange).
//
// Failed conversions return a counted NumError. NewNumError retains the
// function and input strings and formats the message once. The last error
// reference releases its storage (language spec section 16.7).
type NumError struct {
	Func string // the failing function (ParseBool, ParseInt, ParseUint, ParseFloat, ParseComplex)
	Num  string // the input
	Err  error  // the reason the conversion failed (ErrRange, ErrSyntax, ...)

	msg string
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

// NewNumError returns a NumError for the function fn failing on num with
// err, with its counted object and message allocated from a, by default the
// default allocator. For example, NewNumError("ParseFloat", s, ErrSyntax)
// is the error Go's ParseFloat returns for a malformed s.
func NewNumError(fn, num string, err error, a alloc.Allocator = alloc.Default()) synced[NumError] {
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
	p, ctl := rt.NewStringBuffer(n, a)
	text := unsafe.Slice(p, n)
	k := copy(text, prefix)
	k += copy(text[k:], fn)
	k += copy(text[k:], parsing)
	q = quoter{buf: text[k : k+q.n]}
	q.quote(num)
	k += q.n
	text[k], text[k+1] = ':', ' '
	copy(text[k+2:], errMsg)
	// The text is valid UTF-8: the function name, the quoted input (Quote
	// output), the message, all valid strings, joined at their edges.
	msg := rt.MakeString(p, n, ctl, false)
	rt.ReleaseString(ctl)
	e := alloc.Synced[NumError](a)
	e.Func = rt.KeepString(fn, a)
	e.Num = rt.KeepString(num, a)
	e.Err = err
	e.msg = msg
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
