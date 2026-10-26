package main
var n int

var hook func()

func callHook() { hook() }

func main() {
	hook = start
	callHook()
	n++
}

func start() { task worker() }

func worker() { n++ }