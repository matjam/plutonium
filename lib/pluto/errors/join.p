package errors

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/vec"
	"unsafe"
)

// Join returns an error that wraps the given errors. Any nil errors are
// discarded. Join returns nil if every value in errs is nil. The error's
// message is the messages of the non-nil errors, each on its own line.
//
// The error, its list and its message use the default allocator,
// alloc.Default(). To use another allocator, install it around the call
// with alloc.Use: Join is variadic, so it cannot take a defaulted allocator
// parameter (see the language spec, section 10.13). The joined error owns the
// errors it wraps; their last references release them automatically.
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
	e := alloc.Synced[joinError](a)
	e.errs = vec.Make[error](n, a)
	list := e.errs.Slice()
	k := 0
	for _, err := range errs {
		if err != nil {
			list[k] = err
			k++
		}
	}
	e.msg = msg
	return e
}

// A joinError is the result of Join.
type joinError owner struct {
	errs vec.Vec[error]
	msg  string
}

func (e *joinError) Error() string   { return e.msg }
func (e *joinError) Unwrap() []error { return e.errs.Slice() }
