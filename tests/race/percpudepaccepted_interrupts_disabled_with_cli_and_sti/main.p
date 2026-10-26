package main
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

func main() {
	x86.Cli()
	x := ticks
	ticks = x + 1
	x86.Sti()
}