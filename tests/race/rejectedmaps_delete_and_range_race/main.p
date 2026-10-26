package main
func drain(m map[int]bool, done chan bool) {
	for k := range m {
		delete(m, k)
	}
	done <- true
}

func main() {
	m := map[int]bool{1: true, 2: true}
	done := make(chan bool)
	task drain(m, done)
	for i := 0; i < 10; i++ {
		m[i] = true
	}
	<-done
}