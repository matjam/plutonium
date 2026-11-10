package main

var cfg int

func reader(done chan bool) {
	println(cfg)
	done <- true
}

// The deferred write runs after the task starts, so it is a shared write.
func main() {
	done := make(chan bool)
	defer func() { cfg = 0 }()
	task reader(done)
	<-done
}
