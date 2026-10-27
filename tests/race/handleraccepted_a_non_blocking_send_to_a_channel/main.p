package main
import "pluto/arch/x86"

var events chan int

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	select {
	case events <- 1:
	default:
	}
}

func main() {
	events = make(chan int, 4)
	_ = x86.InterruptGate(timer, 0x08, 0)
	<-events
}