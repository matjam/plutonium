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
	f := T.set
	x := ticks
	f(T{}, x+1)
}