// Package errors provides simple error values.
//
// Plutonium does not allocate behind your back, so an error made with New
// lives in memory from the default allocator. Package-level errors are best
// written as static values:
//
//	var ErrClosed = &errors.Text{Msg: "file already closed"}
package errors

// A Text is an error whose message is fixed.
type Text struct {
	Msg string
}

func (t *Text) Error() string { return t.Msg }

// New returns an error with the given message, allocated from the default
// allocator.
func New(msg string) error {
	t := new(Text)
	t.Msg = msg
	return t
}

// Is reports whether err is target. Plutonium has no wrapping, so this is
// equality, provided for code written against Go's errors package.
func Is(err, target error) bool { return err == target }
