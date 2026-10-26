package main
func count(words [3]string, done chan int) {
	m := map[string]int{}
	for _, w := range words {
		m[w]++
	}
	add(m, "extra")
	done <- len(m)
}

func add(m map[string]int, k string) { m[k]++ }

func main() {
	done := make(chan int)
	for i := 0; i < 4; i++ {
		task count([3]string{"a", "b", "a"}, done)
	}
	local := map[string]int{}
	add(local, "x")
	for i := 0; i < 4; i++ {
		println(<-done)
	}
	println(len(local))
}