package main
type Thread struct{ id int }

//pu:percpu
var current *Thread

func main() {
	idle, next := &thread0, &thread1
	switch current {
	case idle:
		current = next
	}
}