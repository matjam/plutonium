package main

import "pluto/fmt"

// Owners: memory and resources that are freed without a garbage collector.
//
// An owner type holds something that must be released, such as a file
// descriptor or a block of memory. A value of owner type has exactly one
// holder. Assigning it, passing it or returning it moves it, and the place it
// came from holds nothing afterwards (language spec sections 7.18 and 18.5).
// When the holder's scope ends, the compiler calls the type's Drop method, so
// release happens exactly once, at a point you can see in the source
// (section 18.6). Using a variable after it was moved is a compile error, not
// a bug found at run time.
//
// A pointer *T is a view: it borrows a value without owning it, never moves
// or frees it, and may not outlive it (section 18.3). Functions that only look
// at an owner take a pointer to it, and the caller keeps ownership.

// A Conn stands for an open connection. The word owner makes it move instead
// of copy, and allows a Drop method.
type Conn owner struct {
	id int
}

func (c *Conn) Drop() { fmt.Printf("  conn %d closed\n", c.id) }

func open(id int) Conn { return Conn{id: id} } // the result moves to the caller

// use borrows its argument. The caller still owns the connection.
func use(c *Conn) { fmt.Printf("  using conn %d\n", c.id) }

// finish takes its argument by value, so it consumes it: the connection is
// dropped when finish returns.
func finish(c Conn) { fmt.Printf("  finishing conn %d\n", c.id) }

// A node is an owner too, because it has an owner field (a struct holding an
// owner must be declared owner itself).
type node owner struct {
	conn Conn
}

func main() {
	fmt.Println("open two connections")
	a := open(1)
	b := open(2)
	use(&a) // a view: borrows, does not move
	use(&b)

	fmt.Println("a block drops what it holds when it ends")
	{
		scratch := open(3)
		use(&scratch)
	} // conn 3 is closed here

	fmt.Println("hand b to a function that consumes it")
	finish(b) // b moves into finish; using b, or a view of it, after this line would not compile

	fmt.Println("new returns an owned[node], a unique box on the heap")
	n := new(node)
	n.conn = open(4)
	n.conn = open(5) // assigning drops the connection the place held before
	fmt.Printf("  node holds conn %d\n", n.conn.id)

	fmt.Println("end of main: variables are dropped in reverse order")
}
