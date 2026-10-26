package main
type Thread struct{ id int }

//pu:percpu
var current *Thread

func get() *Thread { return current }

func set(t *Thread) { current = t }

func main() {
	t := get()
	set(t)
}