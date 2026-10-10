package fmt

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/rt"
	"pluto/unicode/utf8"
	"unsafe"
)

// maxWrapped is the number of %w operands Errorf records.
const maxWrapped = 8

// Errorf formats according to a format specifier and returns the result as
// an error whose memory comes from the default allocator, alloc.Default().
// To use another allocator, install it around the call with alloc.Use:
// Errorf is variadic, so it cannot take a defaulted allocator parameter
// (see the language spec, section 5.1).
//
// As in Go, the %w verb formats an error operand like %v and makes the
// result wrap it: with one %w the error has an Unwrap() error method, with
// several an Unwrap() []error method (up to 8 operands are recorded). The
// new error owns the errors it wraps, so errors.Free on it releases the
// whole chain.
//
// The message is formatted when the error is made, into a string (language
// spec section 6.10), which the error keeps: it does not depend on the
// operands staying alive, and Error returns it without copying. Freeing the
// error releases it.
//
//pu:alloc
func Errorf(format string, args ...any) error {
	a := alloc.Default()
	var ws [maxWrapped]error
	var w stringSink
	var p printer
	p.w = &w
	p.wrapErrs = true
	p.wrapped = ws[:0]
	p.doPrintf(format, args)
	p.flush()
	msg := w.string()
	nw := len(p.wrapped)

	var one wrapError
	var many wrapErrors
	var e0 error
	hdr, align := unsafe.Sizeof(one), unsafe.Alignof(one)
	if nw > 1 {
		hdr = unsafe.Sizeof(many) + uintptr(nw)*unsafe.Sizeof(e0)
	}
	mem := alloc.Raw(hdr, align, a)
	if nw > 1 {
		e := (*wrapErrors)(mem)
		e.msg = msg
		e.errs = unsafe.Slice((*error)(unsafe.Add(mem, unsafe.Sizeof(many))), nw)
		copy(e.errs, ws[:nw])
		e.a, e.size = a, hdr
		return e
	}
	e := (*wrapError)(mem)
	e.msg = msg
	if nw == 1 {
		e.err = ws[0]
	}
	e.a, e.size = a, hdr
	return e
}

// A wrapError is Errorf's result with at most one %w operand.
type wrapError struct {
	msg  string
	err  error
	a    alloc.Allocator
	size uintptr
}

func (e *wrapError) Error() string { return e.msg }
func (e *wrapError) Unwrap() error { return e.err }

// Free releases the message and returns the error's memory to its
// allocator; errors.Free also frees the error it wraps.
func (e *wrapError) Free() {
	var hdr wrapError
	rt.ReleaseValues(e, 1)
	e.a.Free(unsafe.Pointer(e), e.size, unsafe.Alignof(hdr))
}

// A wrapErrors is Errorf's result with several %w operands.
type wrapErrors struct {
	msg  string
	errs []error
	a    alloc.Allocator
	size uintptr
}

func (e *wrapErrors) Error() string   { return e.msg }
func (e *wrapErrors) Unwrap() []error { return e.errs }

// Free releases the message and returns the error's memory to its
// allocator; errors.Free also frees the errors it wraps.
func (e *wrapErrors) Free() {
	var hdr wrapErrors
	rt.ReleaseValues(e, 1)
	e.a.Free(unsafe.Pointer(e), e.size, unsafe.Alignof(hdr))
}

// errorBufSize is the capacity of an ErrorBuf's message.
const errorBufSize = 200

// An ErrorBuf is storage for one formatted error, owned by whoever holds
// it: a parser, a device, a connection. Its Errorf method formats into the
// buffer and returns the ErrorBuf itself as the error, so the error is valid
// for as long as the ErrorBuf is and until its next Errorf call. Nothing is
// allocated. A message longer than 200 bytes is cut short, and only the
// first %w operand is wrapped.
//
//	type Parser struct {
//		errBuf fmt.ErrorBuf
//		...
//	}
//
//	return p.errBuf.Errorf("line %d: unexpected %q", p.line, tok)
//
// An ErrorBuf in a function's own frame must not be returned (see the
// language spec, section 4.4).
type ErrorBuf struct {
	text [errorBufSize]byte
	n    int
	err  error
}

// Errorf formats into b and returns b as an error. It panics if the error
// would wrap itself.
func (b *ErrorBuf) Errorf(format string, args ...any) error {
	var tmp [errorBufSize]byte
	var ws [1]error
	var p printer
	p.wrapErrs = true
	p.wrapped = ws[:0]
	p.dst = tmp[:0]
	p.doPrintf(format, args)
	p.flush()
	var wrapped error
	if len(p.wrapped) > 0 {
		wrapped = p.wrapped[0]
		if errors.Is(wrapped, b) {
			panic("fmt: ErrorBuf.Errorf would wrap itself")
		}
	}
	b.n = copy(b.text[:], p.dst)
	b.n = sanitize(b.text[:b.n])
	b.err = wrapped
	return b
}

// sanitize makes text valid UTF-8 in place, so that Error can return it as
// a string: a rune cut short at the end, where the message did not fit, is
// dropped, and any other invalid byte becomes '?'. It returns the new
// length.
func sanitize(text []byte) int {
	n := len(text)
	for i := 0; i < n; {
		if text[i] < utf8.RuneSelf {
			i++
			continue
		}
		r, size := utf8.DecodeRune(text[i:n])
		if r != utf8.RuneError || size != 1 {
			i += size
			continue
		}
		if !utf8.FullRune(text[i:n]) {
			return i // cut short at the end
		}
		text[i] = '?'
		i++
	}
	return n
}

// Error returns the message. It is a view of the ErrorBuf's storage, not a
// copy: like the error itself it is valid until the next Errorf. The text is
// valid UTF-8: sanitize made every byte of it so, and cut it short rather
// than leave half a rune.
func (b *ErrorBuf) Error() string { return unsafe.String(&b.text[0], b.n) }

// Unwrap returns the error wrapped by the last Errorf's %w operand.
func (b *ErrorBuf) Unwrap() error { return b.err }
