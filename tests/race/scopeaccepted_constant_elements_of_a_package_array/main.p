package main
var table [2]int

func one(done chan bool) {
	table[0]++
	done <- true
}

func two(done chan bool) {
	table[1]++
	done <- true
}

func main() {
	done := make(chan bool)
	task one(done)
	task two(done)
	<-done
	<-done
}