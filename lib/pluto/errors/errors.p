// Package errors implements functions to manipulate errors, as Go's errors
// package does, without allocating behind the caller's back.
//
// An error value is an interface, and Plutonium copies a value larger than a
// word into the converting function's frame when it makes an interface (see
// the language spec, section 4.4). So every error that leaves a function is
// either a word-sized value (such as syscall.Errno) or a pointer to storage
// that outlives the call:
//
//   - static storage: sentinel errors made by New in a package-level
//     variable, or written as &errors.Text{...} there;
//   - storage owned by a longer-lived object, such as a parser that keeps
//     its last error in a field and returns a pointer to it;
//   - memory from an allocator: fmt.Errorf, errors.Join and os.PathError
//     take one (the default allocator unless the caller passes another).
//
// Errors that own allocator memory have a Free method; Free releases a whole
// chain. Wrapping an error hands it to the wrapper, so free only the
// outermost one, or allocate errors from an arena and reset it.
package errors

import (
	"pluto/rt"
	"pluto/sync/atomic"
)

// A Text is an error whose message is fixed. New returns one; a package can
// also declare one as a static literal:
//
//	var ErrClosed = &errors.Text{Msg: "file already closed"}
type Text struct {
	Msg string
}

func (t *Text) Error() string { return t.Msg }

// maxNew is the number of errors New can make in one program.
const maxNew = 512

var (
	texts [maxNew]Text
	nNew  uint32
)

// New returns an error whose message is text. Each call returns a distinct
// error, so New is meant for sentinel errors in package-level variables:
//
//	var ErrNotFound = errors.New("not found")
//
// New does not allocate: the error lives in static storage reserved for New,
// which holds 512 errors and panics when it runs out. text must be a
// constant or other static string, since the error keeps it. To build a
// message at run time, use fmt.Errorf with an allocator, or a fmt.ErrorBuf.
func New(text string) error {
	i := atomic.AddUint32(&nNew, 1) - 1
	if i >= maxNew {
		panic("errors.New: more than 512 errors; declare errors in package-level variables, or use fmt.Errorf")
	}
	texts[i].Msg = text
	return &texts[i]
}

// ErrUnsupported indicates that a requested operation cannot be performed,
// because it is unsupported.
var ErrUnsupported = New("unsupported operation")

// Unwrap returns the result of calling the Unwrap method on err, if err's
// type has one returning error. Otherwise it returns nil. It does not unwrap
// errors whose Unwrap method returns []error.
func Unwrap(err error) error {
	u, ok := err.(interface{ Unwrap() error })
	if !ok {
		return nil
	}
	return u.Unwrap()
}

// Is reports whether any error in err's tree matches target.
//
// The tree is err itself followed by the errors reached by calling Unwrap
// repeatedly, depth first. An error matches target if it is equal to it or
// if it has a method Is(error) bool that reports true for it.
func Is(err, target error) bool {
	if err == nil || target == nil {
		return err == target
	}
	return is(err, target, rt.Comparable(target))
}

func is(err, target error, comparable bool) bool {
	for {
		if comparable && err == target {
			return true
		}
		if x, ok := err.(interface{ Is(error) bool }); ok && x.Is(target) {
			return true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
			if err == nil {
				return false
			}
		case interface{ Unwrap() []error }:
			for _, e := range x.Unwrap() {
				if e != nil && is(e, target, comparable) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
}

// As finds the first error in err's tree whose type is E, sets *target to
// it and reports true. An error also matches if it has a method
// As(any) bool that reports true for target. As reads like Go's:
//
//	var perr *os.PathError
//	if errors.As(err, &perr) {
//		fmt.Println(perr.Path)
//	}
//
// E is inferred from target; it is a concrete error type or an interface.
func As[E any](err error, target *E) bool {
	if target == nil {
		panic("errors: target cannot be nil")
	}
	for err != nil {
		if e, ok := any(err).(E); ok {
			*target = e
			return true
		}
		if x, ok := err.(interface{ As(any) bool }); ok && x.As(target) {
			return true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, e := range x.Unwrap() {
				if e != nil && As(e, target) {
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

// AsType finds the first error in err's tree whose type is E and returns
// it, as Go 1.26's errors.AsType does.
func AsType[E error](err error) (E, bool) {
	var e E
	ok := As(err, &e)
	return e, ok
}

// Free releases the memory owned by err and by every error in its tree:
// each error that has a Free method is freed, after the errors it wraps.
// Static and word-sized errors have nothing to free and are skipped, so Free
// accepts any error. err must not be used afterwards.
func Free(err error) {
	for err != nil {
		var next error
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			next = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, e := range x.Unwrap() {
				Free(e)
			}
		}
		if f, ok := err.(interface{ Free() }); ok {
			f.Free()
		}
		err = next
	}
}
