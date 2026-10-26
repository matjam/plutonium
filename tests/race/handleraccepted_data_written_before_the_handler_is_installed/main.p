package main
import "pluto/arch/x86"

var table [4]int

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	println(table[1])
}

func main() {
	table[1] = 7
	_ = x86.InterruptGate(timer, 0x08, 0)
}