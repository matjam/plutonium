package main
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

func main() {
	f := x86.SaveFlagsCli()
	ticks++
	x86.RestoreFlags(f)
}