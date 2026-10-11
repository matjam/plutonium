package main

func main() {
	out := make(chan int, 4)
	for i := 0; i < 4; i++ {
		task func() {
			out <- i * i
		}()
	}
	task println("hello")
	println(<-out)
}
