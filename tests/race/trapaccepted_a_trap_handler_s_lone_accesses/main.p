package main
import "pluto/arch/x86"

type Thread struct{ id int }

//pu:percpu
var current *Thread

var boot Thread

//pu:interrupt trap
func fault(f *x86.InterruptFrame) {
	t := current
	_ = t
	current = &boot
}

func main() { _ = x86.TrapGate(fault, 0x08, 0) }