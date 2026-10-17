package main

import "pluto/errors"

func apply(f func(string) error) error { return f("x") }

func main() {
	f := errors.New
	_ = f("a")
	_ = apply(errors.New)
	defer errors.New("late")
}
