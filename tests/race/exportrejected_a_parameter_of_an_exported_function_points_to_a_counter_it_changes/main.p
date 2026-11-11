package main

// C code may call an exported function from any thread with any pointer,
// so the memory a pointer parameter reaches crosses to the call (language
// spec section 19.3, Crossings): a Counter changed without a lock is E3501.

type Counter struct{ n int }

//pu:export
func bump(c *Counter) { c.n++ }

func worker(done chan int) { done <- 1 }

func main() {
	done := make(chan int)
	task worker(done)
	println(<-done)
}
