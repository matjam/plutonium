package main

// The language features Plutonium's error conventions rely on, written
// without imports so that Go can run the same program: sentinels in static
// storage, word-sized error codes, errors kept in their owner's storage,
// Unwrap chains with Is and a generic As, typed nil pointers in an error,
// and comparison and switches on error values.

type textError struct{ msg string }

func (e *textError) Error() string { return e.msg }

// newError hands out sentinels from static storage, as pluto/errors.New
// does.
var texts [8]textError
var nTexts int

func newError(msg string) error {
	t := &texts[nTexts]
	nTexts++
	t.msg = msg
	return t
}

var (
	errNotFound = newError("not found")
	errDenied   = newError("permission denied")
	errEOF      = &textError{"EOF"}
)

// errno is a word-sized error code.
type errno uintptr

const (
	eNOENT errno = 2
	eACCES errno = 13
	eIO    errno = 5
)

func (e errno) Error() string {
	switch e {
	case eNOENT:
		return "no such file or directory"
	case eACCES:
		return "permission denied"
	case eIO:
		return "input/output error"
	}
	return "unknown error"
}

func (e errno) Is(target error) bool {
	switch target {
	case errNotFound:
		return e == eNOENT
	case errDenied:
		return e == eACCES
	}
	return false
}

// wrapped is an error with context, in storage owned by the caller.
type wrapped struct {
	op  string
	err error
}

func (w *wrapped) Error() string { return w.op }
func (w *wrapped) Unwrap() error { return w.err }

type multi struct {
	errs []error
}

func (m *multi) Error() string   { return "multiple errors" }
func (m *multi) Unwrap() []error { return m.errs }

// position is a comparable struct error compared by value.
type position struct{ line, col int }

func (p position) Error() string { return "bad position" }

func is(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		if x, ok := err.(interface{ Is(error) bool }); ok && x.Is(target) {
			return true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, e := range x.Unwrap() {
				if is(e, target) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
	return target == nil
}

func as[E any](err error, target *E) bool {
	// Keep this error held while walking its children. Replacing err in
	// a loop could release the object that holds a borrowed target.
	if err != nil {
		if e, ok := any(err).(E); ok {
			*target = e
			return true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			return as(x.Unwrap(), target)
		case interface{ Unwrap() []error }:
			for _, e := range x.Unwrap() {
				if as(e, target) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
	return false
}

// A device keeps its last error in its own storage.
type device struct {
	name    string
	lastErr wrapped
	reads   int
}

func (d *device) read() error {
	d.reads++
	switch d.reads {
	case 1:
		return nil
	case 2:
		d.lastErr = wrapped{op: "read " + "sda", err: eIO}
		return &d.lastErr
	}
	return errEOF
}

func open(path string) (int, error) {
	switch path {
	case "/etc/passwd":
		return 3, nil
	case "/root/secret":
		return -1, eACCES
	}
	return -1, eNOENT
}

func mayFail(fail bool) error {
	var p *textError
	if fail {
		p = errEOF
	}
	return p // a nil *textError is a non-nil error
}

func describe(err error) string {
	switch err {
	case nil:
		return "nil"
	case errEOF:
		return "eof"
	case error(eNOENT):
		return "enoent"
	}
	switch e := err.(type) {
	case errno:
		return e.Error()
	case *wrapped:
		return "wrapped " + "error"
	case position:
		if e.line > 0 {
			return "position"
		}
	}
	return "other"
}

func main() {
	println(errNotFound.Error(), errDenied.Error(), errNotFound == errDenied, errNotFound == errNotFound)

	for _, p := range []string{"/etc/passwd", "/root/secret", "/missing"} {
		fd, err := open(p)
		if err != nil {
			println(p, err.Error(), is(err, errNotFound), is(err, errDenied), describe(err))
			continue
		}
		println(p, "fd", fd)
	}

	d := device{name: "sda"}
	for i := 0; i < 3; i++ {
		err := d.read()
		var w *wrapped
		var code errno
		println("read", i, describe(err), as(err, &w), as(err, &code), int(code))
		if w != nil {
			println("  ", w.op, w.err.Error(), is(err, eIO), w == &d.lastErr)
		}
	}

	top := &wrapped{op: "startup", err: &wrapped{op: "load config", err: &multi{errs: []error{errEOF, eNOENT}}}}
	println("chain", is(top, errNotFound), is(top, errEOF), is(top, errDenied), is(top, eNOENT))
	var m *multi
	println("as multi", as(top, &m), len(m.errs))

	println("typed nil", mayFail(false) != nil, mayFail(true) != nil, mayFail(true) == error(errEOF))

	var e1 error = position{1, 2}
	var e2 error = position{1, 2}
	var e3 error = position{2, 1}
	println("by value", e1 == e2, e1 == e3, describe(e1), is(e3, position{2, 1}))

	msg := top.Error
	println("method value", msg())
}
