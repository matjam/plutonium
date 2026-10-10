package errors

import (
	"pluto/alloc"
	"pluto/rt"
	"unsafe"
)

// Join returns an error that wraps the given errors. Any nil errors are
// discarded. Join returns nil if every value in errs is nil. The error's
// message is the messages of the non-nil errors, each on its own line.
//
// The error is one block from the default allocator, alloc.Default(),
// holding the error and its list of errors; its message is a string from
// the same allocator. To use another allocator, install it around the call
// with alloc.Use: Join is variadic, so it cannot take a defaulted allocator
// parameter (see the language spec, section 5.1). The joined error owns the
// errors it wraps; errors.Free releases them all.
func Join(errs ...error) error {
	a := alloc.Default()
	n, size := 0, 0
	ascii := true
	for _, err := range errs {
		if err == nil {
			continue
		}
		if n > 0 {
			size++
		}
		m := err.Error()
		size += len(m)
		ascii = ascii && rt.KnownASCII(m)
		n++
	}
	if n == 0 {
		return nil
	}
	// The message, built in one buffer: the messages again, one per line.
	msg := ""
	if size > 0 {
		buf, ctl := rt.NewStringBuffer(size, a)
		text := unsafe.Slice(buf, size)
		k, w := 0, 0 // errors and bytes written
		for _, err := range errs {
			if err == nil {
				continue
			}
			if k > 0 {
				text[w] = '\n'
				w++
			}
			w += copy(text[w:], err.Error())
			k++
		}
		msg = rt.MakeString(buf, w, ctl, ascii)
		rt.ReleaseString(ctl)
	}
	var hdr joinError
	listOff := unsafe.Sizeof(hdr)
	total := listOff + uintptr(n)*unsafe.Sizeof(errs[0])
	p := alloc.Raw(total, unsafe.Alignof(hdr), a)
	e := (*joinError)(p)
	e.errs = unsafe.Slice((*error)(unsafe.Add(p, listOff)), n)
	k := 0
	for _, err := range errs {
		if err != nil {
			e.errs[k] = err
			k++
		}
	}
	e.msg = msg
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

// Free releases the message and returns the joined error's memory to its
// allocator. It does not free the wrapped errors; errors.Free does.
func (e *joinError) Free() {
	var hdr joinError
	rt.ReleaseValues(e, 1)
	e.a.Free(unsafe.Pointer(e), e.size, unsafe.Alignof(hdr))
}
