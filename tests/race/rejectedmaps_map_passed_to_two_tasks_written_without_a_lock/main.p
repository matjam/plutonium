package main
func count(words [3]string, m map[string]int, done chan bool) {
	for _, w := range words {
		m[w]++
	}
	done <- true
}

func main() {
	m := map[string]int{}
	done := make(chan bool)
	task count([3]string{"a", "b", "a"}, m, done)
	task count([3]string{"b", "c", "c"}, m, done)
	<-done
	<-done
	println(len(m))
}