package main

import (
	"pluto/errors"
	"pluto/fmt"
)

// Errors without a garbage collector.
//
// An error is a value of the interface type error, as in Go. Where one lives
// is a decision the language makes visible (language spec chapter 16):
//
//   - errors.New(text) is a sentinel. The text must be a constant, and the
//     error is a static, read-only object that belongs to the call site, so
//     it never allocates and works before an allocator exists (section 16.3).
//   - fmt.Errorf(format, args...) builds an error that records context, such
//     as a name or a count. It allocates, and the error is a counted object
//     freed when the last copy of it goes away (sections 16.5 and 16.7).
//   - %w in the format wraps another error. errors.Is looks through the
//     wrapping, so a caller can test for the cause without parsing text
//     (section 16.6).

var (
	ErrEmpty    = errors.New("empty name") // static, allocation-free
	ErrNotFound = errors.New("not found")
)

func check(name string) error {
	if name == "" {
		return ErrEmpty
	}
	if len(name) > 8 {
		return fmt.Errorf("name %q too long (%d > 8)", name, len(name))
	}
	return nil
}

var users = [2]string{"pluto", "ada"}

func find(name string) error {
	for _, u := range users {
		if u == name {
			return nil
		}
	}
	return fmt.Errorf("find %q: %w", name, ErrNotFound)
}

func main() {
	for _, n := range [3]string{"", "pluto", "plutonium"} {
		if err := check(n); err != nil {
			fmt.Println("error:", err, "| is ErrEmpty:", errors.Is(err, ErrEmpty))
			continue
		}
		fmt.Println("ok:", n)
	}

	err := find("grace")
	fmt.Println(err)
	fmt.Println("is ErrNotFound:", errors.Is(err, ErrNotFound))
	fmt.Println("is ErrEmpty:", errors.Is(err, ErrEmpty))
	fmt.Println("cause:", errors.Unwrap(err))
	fmt.Println("find ada:", find("ada"))
}
