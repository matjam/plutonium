package main
var hits int

func handle(done chan bool) {
	hits++
	done <- true
}

func main() {
	done := make(chan bool)
	for i := 0; i < 4; i++ {
		task handle(done)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}