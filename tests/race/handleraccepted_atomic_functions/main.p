package main
import (
	"pluto/arch/x86"
	"pluto/sync/atomic"
)

var ticks int64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	atomic.AddInt64(&ticks, 1)
}

func main() {
	_ = x86.InterruptGate(timer, 0x08, 0)
	println(atomic.LoadInt64(&ticks))
}