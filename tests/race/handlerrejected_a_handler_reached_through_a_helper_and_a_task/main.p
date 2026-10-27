package main
import "pluto/arch/x86"

var log [4]int

func record(i int) { log[i&3] = i }

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	record(1)
}

func work() { record(2) }

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	task work()
}