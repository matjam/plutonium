package main
func lookup(m map[string]int, done chan int) {
	done <- m["a"] + len(m)
}

func main() {
	m := map[string]int{"a": 1}
	m["b"] = 2
	done := make(chan int)
	task lookup(m, done)
	task lookup(m, done)
	println(<-done, <-done)
}