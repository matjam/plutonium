package main

import (
	"pluto/sync"
	"pluto/vec"
)

type Logger owner struct {
	mu  sync.Mutex
	buf vec.Vec[string]
}

var std = &Logger{}

func (l *Logger) Print(s string) {
	l.mu.Lock()
	l.buf.Push(s)
	l.mu.Unlock()
}

func work(done chan bool) {
	std.Print("hello\n")
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
}
