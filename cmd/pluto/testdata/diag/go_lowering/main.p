package main

func main() {
	out := make(chan int, 4)
	for i := 0; i < 4; i++ {
		go func() {
			out <- i * i
		}()
	}
	go println("hello")
	println(<-out)
}
