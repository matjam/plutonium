package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

func set(x uint64) { ticks = x }

func main() {
	g := set
	x := ticks
	g(x + 1)
}