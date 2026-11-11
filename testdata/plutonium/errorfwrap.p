package main

// %w wrappers of errors an ErrorBuf makes, used while the ErrorBuf or its
// owner lives (sections 16.5 and 18.3, D69).

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
