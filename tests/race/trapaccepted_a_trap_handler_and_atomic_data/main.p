package main
import (
	"pluto/arch/x86"
	"pluto/sync/atomic"
)

var n atomic.Int64

//pu:interrupt trap
func fault(f *x86.InterruptFrame) { n.Add(1) }

func main() {
	_ = x86.TrapGate(fault, 0x08, 0)
	println(n.Load())
}