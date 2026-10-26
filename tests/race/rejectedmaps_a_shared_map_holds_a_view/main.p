package main
var data [4]int

func keep(m map[int]*int, done chan bool) {
	done <- true
}

func main() {
	done := make(chan bool)
	m := map[int]*int{}
	task keep(m, done)
	data[0] = 1
	<-done
}