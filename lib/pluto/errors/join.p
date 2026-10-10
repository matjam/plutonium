package errors

import (
	"pluto/alloc"
	"unsafe"
)

// Join returns an error that wraps the given errors, allocated from the
// default allocator. Any nil errors are discarded. Join returns nil if every
// value in errs is nil. The error's message is the messages of the non-nil
// errors, each on its own line. See JoinIn.
func Join(errs ...error) error {
	return JoinIn(alloc.Default(), errs...)
}

// JoinIn is Join with the memory taken from a: one block holding the error,
// its list of errors and its message. The joined error owns the errors it
// wraps; errors.Free releases them all.
func JoinIn(a alloc.Allocator, errs ...error) error {
	n, size := 0, 0
	for _, err := range errs {
		if err == nil {
			continue
		}
		if n > 0 {
			size++
		}
		size += len(err.Error())
		n++
	}
	if n == 0 {
		return nil
	}
	var hdr joinError
	listOff := unsafe.Sizeof(hdr)
	textOff := listOff + uintptr(n)*unsafe.Sizeof(errs[0])
	total := textOff + uintptr(size)
	p := alloc.Raw(a, total, unsafe.Alignof(hdr))
	e := (*joinError)(p)
	e.errs = unsafe.Slice((*error)(unsafe.Add(p, listOff)), n)
	text := unsafe.Slice((*byte)(unsafe.Add(p, textOff)), size)
	k, t := 0, 0
	for _, err := range errs {
		if err == nil {
			continue
		}
		if k > 0 {
			text[t] = '\n'
			t++
		}
		t += copy(text[t:], err.Error())
		e.errs[k] = err
		k++
	}
	e.msg = unsafe.String(unsafe.SliceData(text), size)
	e.a = a
	e.size = total
	return e
}

// A joinError is the result of Join.
type joinError struct {
	errs []error
	msg  string
	a    alloc.Allocator
	size uintptr
}

func (e *joinError) Error() string   { return e.msg }
func (e *joinError) Unwrap() []error { return e.errs }

// Free returns the joined error's memory to its allocator. It does not free
// the wrapped errors; errors.Free does.
func (e *joinError) Free() {
	var hdr joinError
	e.a.Free(unsafe.Pointer(e), e.size, unsafe.Alignof(hdr))
}
