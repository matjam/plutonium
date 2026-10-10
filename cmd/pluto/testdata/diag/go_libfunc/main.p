package main

import (
	"pluto/sync"
	"pluto/time"
)

type counter struct{ n int }

func (c *counter) add() { c.n++ }

func main() {
	var wg sync.WaitGroup
	n := 0
	wg.Go(func() { n++ })

	work := func() { n-- }
	wg.Go(work)

	var c counter
	time.AfterFunc(time.Second, c.add)

	time.AfterFunc(time.Second, func() { println(n) })

	// Not reported: captures nothing.
	wg.Go(func() { println("hello") })
	wg.Wait()
}
