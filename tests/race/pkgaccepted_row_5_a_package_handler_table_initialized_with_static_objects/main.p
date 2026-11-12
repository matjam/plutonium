package main

type Handler interface{ Serve(s string) string }

type echoHandler struct{ calls int }

func (h *echoHandler) Serve(s string) string { return s }

var handlers = map[string]Handler{"echo": &echoHandler{}}

func work(done chan string) {
	done <- handlers["echo"].Serve("hi")
}

func main() {
	done := make(chan string)
	task work(done)
	task work(done)
	println(<-done, <-done)
}
