package main
import "pluto/arch/x86"

//pu:percpu
var seen [4]uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	seen[0]++
	seen[1] = seen[0]
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }