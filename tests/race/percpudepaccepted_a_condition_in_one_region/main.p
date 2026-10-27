package main
import "pluto/rt"

type Thread struct{ id int }

//pu:percpu
var current *Thread

func main() {
	idle, next := &thread0, &thread1
	rt.DisablePreemption()
	if current == idle {
		current = next
	}
	rt.EnablePreemption()
}