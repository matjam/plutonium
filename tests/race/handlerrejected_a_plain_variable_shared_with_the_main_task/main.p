package main
import "pluto/arch/x86"

var ticks int

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	ticks++
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	println(ticks)
}