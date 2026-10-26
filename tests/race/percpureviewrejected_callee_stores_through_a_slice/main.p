package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

func put(s []uint64, x uint64) { s[0] = x }

func main() {
	var l [1]uint64
	put(l[:], ticks)
	ticks = l[0] + 1
}