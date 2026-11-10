package main

// A method with a value receiver of an owner type consumes its receiver,
// so it has no method value (spec sections 11.9, 18.5): E3315. The call
// and the method expression are allowed.

type Conn owner struct{ fd int32 }

func (c Conn) Close() error { return nil }
func (c *Conn) Fd() int32   { return c.fd }

// Pooled embeds Conn, so it promotes Close.
type Pooled owner struct {
	Conn
	id int
}

func main() {
	var c Conn
	fd := c.Fd
	_ = fd()
	done := c.Close
	_ = done

	var p Pooled
	shut := p.Close
	_ = shut

	close := Conn.Close
	_ = close(Conn{})
	_ = c.Close()
}
