package main
import "pluto/arch/x86"

//pu:percpu
var ticks uint64

func main() {
	x86.Cli()
	ticks++
	x86.Sti()
}