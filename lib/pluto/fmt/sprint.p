package fmt

import (
	"pluto/alloc"
	"pluto/rt"
	"unsafe"
)

// A stringSink collects formatted output in a counted string buffer from
// the current allocator, for Sprintf and Errorf. It is a small
// strings.Builder, which fmt cannot import (strings' tests use testing,
// which uses fmt).
type stringSink struct {
	buf []byte
	ctl uintptr // reference to the buffer, or 0
}

func (w *stringSink) Write(p []byte) (int, error) {
	if cap(w.buf)-len(w.buf) < len(p) {
		c := 2*cap(w.buf) + len(p)
		if len(w.buf) == 0 {
			c = len(p) // the first flush is often all of the output
		}
		a := alloc.Default()
		if a == nil {
			panic("fmt: no allocator: call alloc.SetDefault before building strings")
		}
		q, ctl := rt.NewStringBuffer(c, a)
		nb := unsafe.Slice(q, c)[:len(w.buf)]
		copy(nb, w.buf)
		if w.ctl != 0 {
			rt.ReleaseString(w.ctl)
		}
		w.buf, w.ctl = nb, ctl
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

// string returns the output as a string and drops the sink's reference to
// the buffer. Bytes that are not valid UTF-8 (from a %s of a []byte) make it
// a copy with U+FFFD in their place.
func (w *stringSink) string() string {
	n := len(w.buf)
	if n == 0 {
		return ""
	}
	p := unsafe.SliceData(w.buf)
	valid, ascii := rt.ValidUTF8(p, n)
	var s string
	if valid {
		s = rt.MakeString(p, n, w.ctl, ascii)
	} else {
		s = rt.StringFromBytes(p, n, alloc.Default())
	}
	rt.ReleaseString(w.ctl)
	w.buf, w.ctl = nil, 0
	return s
}

// Sprintf formats according to a format specifier and returns the resulting
// string, allocated from the current allocator, alloc.Default(). Bytes that
// are not valid UTF-8 (from a %s of a []byte, say) become U+FFFD, as in
// string(b).
//
//pu:alloc
func Sprintf(format string, args ...any) string {
	var w stringSink
	var p printer
	p.w = &w
	p.doPrintf(format, args)
	p.flush()
	return w.string()
}

// Sprint formats args with %v and returns the resulting string, adding
// spaces between operands when neither is a string.
//
//pu:alloc
func Sprint(args ...any) string {
	var w stringSink
	var p printer
	p.w = &w
	p.doPrint(args, false)
	p.flush()
	return w.string()
}

// Sprintln formats args with %v, separated by spaces and followed by a
// newline, and returns the resulting string.
//
//pu:alloc
func Sprintln(args ...any) string {
	var w stringSink
	var p printer
	p.w = &w
	p.doPrint(args, true)
	p.flush()
	return w.string()
}
