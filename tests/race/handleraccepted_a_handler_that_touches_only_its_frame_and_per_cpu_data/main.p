package main
import "pluto/arch/x86"

//pu:percpu
var depth int

//pu:interrupt
func fault(f *x86.InterruptFrame, code uint64) {
	depth = int(code)
	f.RIP += 2
}

func main() { _ = x86.InterruptGate(fault, 0x08, 0) }