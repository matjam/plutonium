package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var n int

//pu:interrupt trap
func fault(f *x86.InterruptFrame) { n++ }

func work() { n++ }

func main() {
	_ = x86.TrapGate(fault, 0x08, 0)
	task work()
	rt.Yield()
}