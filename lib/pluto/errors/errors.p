// Package errors implements functions to manipulate errors, as Go's errors
// package does, without allocating behind the caller's back.
//
// An error value is an interface, and Plutonium copies a value larger than a
// word into the converting function's frame when it makes an interface (see
// the language spec, section 7.9). So every error that leaves a function is
// either a word-sized value (such as syscall.Errno) or a pointer to storage
// that outlives the call:
//
//   - the interface's data word: errors made by New with a constant
//     message, anywhere, which hold a count;
//   - static storage: errors written as &errors.Text{...} in a
//     package-level variable;
//   - storage owned by a longer-lived object, such as a parser that keeps
//     its last error in a field and returns a pointer to it;
//   - counted objects: fmt.Errorf, errors.Join and library error types
//     keep their context and the errors they wrap alive.
//
// Copies of an error count a shared or synced object that it holds. The
// last reference drops the object and frees it to its original allocator
// (language spec section 16.7). No manual release is needed.
package errors

import "pluto/rt"

// A Text is an error whose message is fixed. New returns one; a package can
// also declare one as a static literal:
//
//	var ErrClosed = &errors.Text{Msg: "file already closed"}
type Text struct {
	Msg string
}

func (t *Text) Error() string { return t.Msg }

// New returns an error whose message is text, which must be a constant.
// It works anywhere, as in Go: in a package-level variable,
//
//	var ErrNotFound = errors.New("not found")
//
// or inside a function, return errors.New("bad header").
//
// Each evaluation returns a new error, as in Go, and New does not allocate.
// The compiler makes an error type for each call site and a counter for it
// in static storage; a call adds one to the counter and returns an error of
// the site's type that holds the new count. Two results of one call site
// differ, a copy of a result is equal to it, and different call sites return
// different errors even when their messages are equal. A package-level
// variable is evaluated once. A message that is not a constant is a compile
// error; build one at run time with fmt.Errorf or a fmt.ErrorBuf. New cannot
// be used as a func value or deferred.
func New(text string) error {
	// Never reached: the compiler replaces every call.
	panic("errors.New: called without compiler support")
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
	if err != nil {
		if e, ok := any(err).(E); ok {
			*target = e
			return true
		}
		if x, ok := err.(interface{ As(any) bool }); ok && x.As(target) {
			return true
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			return As(x.Unwrap(), target)
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
