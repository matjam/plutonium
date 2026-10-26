package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

type T struct{ n int }

func (T) set(x uint64) { ticks = x }

func main() {
	var t T
	m := t.set
	x := ticks
	m(x + 1)
}