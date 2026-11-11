package main

// A %w wrapper of an error that an ErrorBuf makes may be used while the
// ErrorBuf, or the owner holding it, still lives (sections 16.5 and 18.3).

import "pluto/fmt"

type Conn owner struct {
	eb fmt.ErrorBuf
	n  int
}

func consume(c Conn) {}

func usedBeforeMove() {
	c := Conn{}
	w := fmt.Errorf("conn: %w", c.eb.Errorf("x %d", 1))
	println(w.Error())
	consume(c)
}

func frameBuf() {
	var eb fmt.ErrorBuf
	w := fmt.Errorf("conn: %w", eb.Errorf("x %d", 1))
	println(w.Error())
}

func twoWrapped() {
	var a, b fmt.ErrorBuf
	w := fmt.Errorf("both: %w and %w", a.Errorf("a"), b.Errorf("b"))
	println(w.Error())
}

func main() {
	usedBeforeMove()
	frameBuf()
	twoWrapped()
}
