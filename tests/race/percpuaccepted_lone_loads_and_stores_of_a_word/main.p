package main
type Thread struct{ id int }

//pu:percpu
var current *Thread

//pu:percpu
var depth int

func set(t *Thread) { current = t }

func get() *Thread { return current }

func main() {
	t := get()
	println(t == nil)
	set(&thread1)
	d := depth
	println(d)
	depth = 7
	println(current == nil)
}