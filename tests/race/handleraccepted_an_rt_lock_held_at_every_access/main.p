package main
import (
	"pluto/arch/x86"
	"pluto/rt"
)

var (
	mu    rt.Lock
	queue [8]int
	head  int
)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	mu.Lock()
	queue[head%8]++
	head++
	mu.Unlock()
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	mu.Lock()
	n := head
	mu.Unlock()
	println(n)
}