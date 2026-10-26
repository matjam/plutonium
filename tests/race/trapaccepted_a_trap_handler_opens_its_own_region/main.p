package main
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

//pu:interrupt trap
func timer(f *x86.InterruptFrame) {
	fl := x86.SaveFlagsCli()
	ticks++
	x86.RestoreFlags(fl)
}

func main() { _ = x86.TrapGate(timer, 0x08, 0) }