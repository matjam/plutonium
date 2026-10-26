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
var arr [2]uint32

func main() {
	rt.DisablePreemption()
	x := arr[0]
	rt.EnablePreemption()
	rt.DisablePreemption()
	arr[0] = x + 1
	rt.EnablePreemption()
}