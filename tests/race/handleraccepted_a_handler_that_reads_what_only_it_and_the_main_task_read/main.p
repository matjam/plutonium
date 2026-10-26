package main
import "pluto/arch/x86"

var limit = 10

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	println(limit)
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	println(limit)
}