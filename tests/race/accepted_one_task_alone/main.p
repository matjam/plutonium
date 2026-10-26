package main
var log [16]int
var n int

func logger(done chan bool) {
	for i := 0; i < 16; i++ {
		log[n] = i
		n++
	}
	done <- true
}

func main() {
	done := make(chan bool)
	task logger(done)
	<-done
}