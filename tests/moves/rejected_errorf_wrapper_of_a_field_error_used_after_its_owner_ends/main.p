package main

// A %w wrapper of an error that an ErrorBuf field makes is a view of the
// owner holding the field (sections 16.5 and 18.3): it may not be used once
// the owner is moved or dropped.

import "pluto/fmt"

type Conn owner struct {
	eb fmt.ErrorBuf
	n  int
}

func consume(c Conn) {}

func usedAfterMove() {
	c := Conn{}
	w := fmt.Errorf("conn: %w", c.eb.Errorf("x %d", 1))
	consume(c)
	println(w.Error())
}

func usedAfterDrop() {
	var w error
	{
		c := Conn{}
		w = fmt.Errorf("conn: %w", c.eb.Errorf("x %d", 1))
	}
	println(w.Error())
}

func main() {
	usedAfterMove()
	usedAfterDrop()
}
