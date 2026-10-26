package main
type Counter struct{ n int }

var counter Counter

func bump(c *Counter, done chan bool) {
	c.n++
	done <- true
}

func main() {
	done := make(chan bool)
	task bump(&counter, done)
	task bump(&counter, done)
	<-done
	<-done
}