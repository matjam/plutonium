package os

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/vec"
)

// A NameError reports that a name or a value the operating system gave the
// program is not valid UTF-8, so it cannot be a string (language spec
// section 7.3 and F.9). Op names the function that found it ("DirEntry.Name",
// "Getenv", ...) and Bytes holds the bytes exactly as the system gave them.
// The error owns its copy of the bytes: they stay valid for as long as the
// error does, and its last counted reference frees them (section 16.7).
//
// The exact bytes are also available without the error: DirEntry.RawName,
// GetenvBytes, ArgBytes and the other Bytes forms; and the Lossy form of
// the function (DirEntry.NameLossy, GetenvLossy, ...) returns a string with
// each invalid byte replaced by U+FFFD, for display.
type NameError owner struct {
	Op    string // the function that found the name, for example "DirEntry.Name"
	Bytes []byte // the name or value, exactly as the system gave it
	buf   vec.Vec[byte]
}

// Error returns "op: not valid UTF-8: " and the bytes with U+FFFD for each
// invalid one, a new string.
func (e *NameError) Error() string {
	return e.Op + ": not valid UTF-8: " + lossyCopy(e.Bytes, alloc.Default())
}

// NewNameError returns a NameError for op and the bytes b, allocated from
// a, by default the default allocator. It keeps a copy of b and of op.
func NewNameError(op string, b []byte, a alloc.Allocator = alloc.Default()) synced[NameError] {
	e := alloc.Synced[NameError](a)
	e.Op = rt.KeepString(op, a)
	// Built apart from the error, which is a counted object other tasks may
	// hold, and moved in.
	buf := vec.WithCapacity[byte](len(b), a)
	buf.Append(b...)
	e.buf = buf
	e.Bytes = e.buf.Slice()
	return e
}

// validBytes reports whether b is valid UTF-8.
func validBytes(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	valid, _ := rt.ValidUTF8(&b[0], len(b))
	return valid
}

// strictView returns b as a string if it is valid UTF-8: an uncounted view
// of b, which must outlive it and not change. Otherwise it returns a
// *NameError for op holding the bytes. Every function of this package that
// turns the system's bytes into a string under its Go name does so here.
func strictView(op string, b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	valid, ascii := rt.ValidUTF8(&b[0], len(b))
	if !valid {
		return "", NewNameError(op, b)
	}
	return rt.MakeString(&b[0], len(b), 0, ascii), nil
}
