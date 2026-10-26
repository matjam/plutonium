package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var _ = x86.Pause
var _ = rt.DisablePreemption

//pu:percpu
var ticks uint64

//pu:extern ext_put
func ext_put(p *uint64, x uint64)

func main() {
	var l uint64
	ext_put(&l, ticks)
	ticks = l + 1
}