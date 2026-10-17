package main

import (
	"pluto/arch/x86"
	"pluto/rt"
	"pluto/sync/atomic"
)

//pu:panic
func onPanic(p *rt.Panic) {
	p.Print(rt.Console{})
	for {
		x86.Cli()
		x86.Hlt()
	}
}

// ticks is plain data shared with the timer handler: not enough, even with
// volatile or interrupts disabled, since the handler may run on any
// processor.
var ticks int

// seen is atomic: allowed.
var seen atomic.Int64

// limit is read by the handler and written only before it is installed:
// allowed.
var limit = 10

var (
	mu    rt.Lock
	state int
)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
	seen.Add(1)
	println(limit)
	mu.Lock()
	state = 1
	mu.Unlock()
}

func main() {
	limit = 20
	_ = x86.HandlerAddr(timer)
	flags := x86.SaveFlagsCli()
	ticks = 0
	x86.RestoreFlags(flags)
	println(state, seen.Load())
}
