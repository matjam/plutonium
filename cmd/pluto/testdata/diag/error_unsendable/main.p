package main

// Every error may cross tasks, so a value whose type is not sendable cannot
// become one (language spec section 8.8).

type Detail struct{ code int }

type Failure struct {
	detail shared[Detail]
}

func (f Failure) Error() string { return "failed" }
func (f Failure) Report()       {}

type Reporter interface {
	error
	Report()
}

func fail(f Failure) error { return f }

func wrapped(f Failure) error { return error(f) }

func report(f Failure) Reporter { return f }

// A synced[T] counts atomically, so this one is sendable.
type Fine struct {
	detail synced[Detail]
}

func (f Fine) Error() string { return "fine" }

func ok(f Fine) error { return f }

// An interface that is not an error keeps the whole-program rule, and a
// lent conversion in the frame never counts.
func show(a any) {}

// Comparing an error with a value converts the value to error too.
func compare(err error, f Failure, r Reporter, fine Fine) bool {
	a := err == f
	b := f != err
	c := r == f
	switch err {
	case f:
		return true
	}
	d := err == fine
	return a || b || c || d
}

func main() {
	var f Failure
	show(f.detail)
	fail(f)
	wrapped(f)
	report(f)
	ok(Fine{})
}
