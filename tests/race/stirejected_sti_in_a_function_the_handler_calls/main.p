package main
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

func allow() { x86.Sti() }

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	allow()
	ticks++
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }