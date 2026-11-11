package main

import "pluto/arch/x86"

type Dev struct{ n int }

var (
	devs  [4]Dev
	table [4]*Dev
)

//pu:interrupt
func timer(f *x86.InterruptFrame) {
	d := table[0]
	println(d.n)
}

func main() {
	for i := range devs {
		devs[i].n = i
		table[i] = &devs[i]
	}
	_ = x86.InterruptGate(timer, 0x08, 0)
}
