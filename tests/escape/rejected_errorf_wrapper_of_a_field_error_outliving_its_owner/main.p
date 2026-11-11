package main

// A %w wrapper of an error that an ErrorBuf field makes is a view of the
// owner holding the field (sections 16.5 and 18.3): it may not be returned
// or stored where it outlives the owner.

import "pluto/fmt"

type Conn owner struct {
	eb fmt.ErrorBuf
	n  int
}

var sink error

func returned() error {
	c := Conn{}
	return fmt.Errorf("conn: %w", c.eb.Errorf("x %d", 1))
}

func stored() {
	c := Conn{}
	sink = fmt.Errorf("conn: %w", c.eb.Errorf("x %d", 1))
}

func main() {
	_ = returned()
	stored()
}
