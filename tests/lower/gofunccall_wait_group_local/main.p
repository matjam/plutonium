package main

import (
	"pluto/rt"
	"pluto/sync"
	"pluto/time"
)

type counter struct{ n int }

func (c *counter) add() { c.n++ }

func run(f func()) { f() }

func run2() {}

func start(f func()) {
	var wg sync.WaitGroup
	wg.Task(f)
	wg.Wait()
}

func spawn(n int, f func(), more ...func()) {
	for _, g := range more {
		g()
	}
	task f()
}

func spawnT[T any](x T, f func()) { task run(f) }

func later(f func()) { time.AfterFunc(time.Second, f) }

var _ = rt.MinStackSize

var _ = time.Second

func main() {
	var wg sync.WaitGroup
	n := 0
	f := func() { n++ }
	wg.Task(f)
}
