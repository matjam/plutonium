package main
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt trap
func timer(f *x86.InterruptFrame) {
	x86.Cli()
	x := ticks
	ticks = x + 1
	x86.Sti()
}

func main() { _ = x86.TrapGate(timer, 0x08, 0) }