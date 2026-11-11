package main

func worker(id int, done chan bool, out chan int) {
	for {
		select {
		case <-done:
			out <- id
			return
		default:
		}
	}
}

func main() {
	done := make(chan bool)
	out := make(chan int, 3)
	for i := 0; i < 3; i++ {
		task worker(i, done, out)
	}
	close(done)
	sum := 0
	for i := 0; i < 3; i++ {
		sum += <-out
	}
	println(sum)
}
