package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

//pu:percpu
var a uint64

func main() {
	a = ticks
	ticks = a + 1
}