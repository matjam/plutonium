package main

type Handler interface{ Serve(s string) string }

type echoHandler struct{ calls int }

func (h *echoHandler) Serve(s string) string { return s }

var handlers = map[string]Handler{}

func Register(name string, h Handler) { handlers[name] = h }

func init() { Register("echo", &echoHandler{}) }

func main() {
	println(handlers["echo"].Serve("hi"))
}
