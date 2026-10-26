package main
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	x86.Sti()
	fl := x86.SaveFlagsCli()
	ticks++
	x86.RestoreFlags(fl)
}

func main() { _ = x86.InterruptGate(timer, 0x08, 0) }