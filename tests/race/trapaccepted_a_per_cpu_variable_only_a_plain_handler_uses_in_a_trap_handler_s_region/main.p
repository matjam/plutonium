package main
import "pluto/arch/x86"

//pu:percpu
var a uint64

//pu:percpu
var b uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) { a++ }

//pu:interrupt trap
func fault(f *x86.InterruptFrame) {
	b = 1
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	_ = x86.TrapGate(fault, 0x08, 0)
}