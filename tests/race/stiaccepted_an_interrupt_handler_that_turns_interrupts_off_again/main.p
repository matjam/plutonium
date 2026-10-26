package main
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	x86.Sti()
	x86.Cli()
	ticks++
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }