package main
type Thread struct{ id int }

//pu:percpu
var current *Thread

func log(t *Thread) { println(t == nil) }

func main() {
	log(current)
	println("between")
	u := &thread3
	current = u
}