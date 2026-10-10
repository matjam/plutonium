package main

// A panic in a deferred call that runs for a panic goes straight to the
// panic handler: the deferred calls after it do not run. Both messages are
// printed, as Go prints them, and the trace goes from the second panic
// through the deferred call to where the first one happened.

type conn struct{ open bool }

func (c *conn) Close() {
	println("closing")
	if c.open {
		panic("close failed")
	}
}

func work(c *conn, n int) int {
	defer c.Close()
	defer println("work deferred, n =", n)
	return 10 / n
}

func main() {
	defer println("not run: the second panic stops the deferred calls")
	c := &conn{open: true}
	println(work(c, 0))
}
