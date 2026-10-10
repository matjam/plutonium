package fmt

import (
	"pluto/alloc"
	"pluto/errors"
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
// The message is formatted when the error is made, into one block from the
// allocator together with the error itself, so it does not depend on the
// operands staying alive. Errorf formats twice: once to measure, once to
// fill.
func Errorf(format string, args ...any) error {
	a := alloc.Default()
	var ws [maxWrapped]error
	var p printer
	p.count = true
	p.wrapErrs = true
	p.wrapped = ws[:0]
	p.doPrintf(format, args)
	p.flush()
	n := p.written
	nw := len(p.wrapped)

	var one wrapError
	var many wrapErrors
	var e0 error
	hdr, align := unsafe.Sizeof(one), unsafe.Alignof(one)
	listOff := hdr
	if nw > 1 {
		hdr = unsafe.Sizeof(many)
		listOff = hdr
		hdr += uintptr(nw) * unsafe.Sizeof(e0)
	}
	total := hdr + uintptr(n)
	mem := alloc.Raw(total, align, a)
	text := unsafe.Slice((*byte)(unsafe.Add(mem, hdr)), n)

	var q printer
	q.wrapErrs = true
	q.dst = text[:0]
	q.doPrintf(format, args)
	q.flush()
	msg := unsafe.String(unsafe.SliceData(text), len(q.dst))

	if nw > 1 {
		e := (*wrapErrors)(mem)
		e.msg = msg
		e.errs = unsafe.Slice((*error)(unsafe.Add(mem, listOff)), nw)
		copy(e.errs, ws[:nw])
		e.a, e.size = a, total
		return e
	}
	e := (*wrapError)(mem)
	e.msg = msg
	if nw == 1 {
		e.err = ws[0]
	}
	e.a, e.size = a, total
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

// Free returns the error's memory to its allocator; errors.Free also frees
// the error it wraps.
func (e *wrapError) Free() {
	var hdr wrapError
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

// Free returns the error's memory to its allocator; errors.Free also frees
// the errors it wraps.
func (e *wrapErrors) Free() {
	var hdr wrapErrors
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
	b.err = wrapped
	return b
}

func (b *ErrorBuf) Error() string { return unsafe.String(&b.text[0], b.n) }

// Unwrap returns the error wrapped by the last Errorf's %w operand.
func (b *ErrorBuf) Unwrap() error { return b.err }
