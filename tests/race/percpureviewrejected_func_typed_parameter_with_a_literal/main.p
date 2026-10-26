package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

func apply(f func(uint64), v uint64) { f(v) }

func main() {
	x := ticks
	apply(func(v uint64) { ticks = v }, x+1)
}