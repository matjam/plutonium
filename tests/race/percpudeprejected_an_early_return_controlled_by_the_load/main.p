package main
type Thread struct{ id int }

//pu:percpu
var current *Thread

func main() {
	idle, next := &thread0, &thread1
	if current != idle {
		return
	}
	current = next
}