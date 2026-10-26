package main

type counter struct{ n int }

func (c *counter) add() { c.n++ }

func (c counter) get() int { return c.n }

type task func()

func (t *task) set(f func()) { *t = f }

func run(f func()) { f() }

func runAll(fs ...func()) {}

func run2() {}

func pair() (func(), bool) { return nil, true }

func start(f func()) { task run(f) }

func main() {
	n := 1
	f := func() { n++ }
	task f()
}
