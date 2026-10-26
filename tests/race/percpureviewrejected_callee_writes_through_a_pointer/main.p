package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

func put(p *uint64, x uint64) { *p = x }

func main() {
	var l uint64
	put(&l, ticks)
	ticks = l + 1
}