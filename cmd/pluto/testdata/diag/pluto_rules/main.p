package main

import "pluto/errors"

type Color enum {
	Red
	Green
	Blue
}

func describe(c Color) int {
	switch c {
	case Red:
		return 1
	}
	return 0
}

func grow(n int, factor int = 2, limit int) int { return n * factor }

func fail(name string) error { return errors.New("no such file: " + name) }

func main() {
	counts := map[string]int{}
	_ = counts
	_ = describe(Color.Green)
	_ = grow(1, 2, 3)
	_ = fail("x")
}
