package main
import "pluto/arch/x86"

var count int

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	count++
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
}