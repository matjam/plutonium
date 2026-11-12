package main

import (
	"pluto/fmt"
	"pluto/os"
	"pluto/sync"
)

type Logger struct {
	mu     sync.Mutex
	w      *os.File
	prefix string
}

func (l *Logger) Printf(format string, args ...any) {
	l.mu.Lock()
	fmt.Fprintf(l.w, l.prefix+format, args...)
	l.mu.Unlock()
}

var std = &Logger{w: os.Stderr, prefix: "app: "}

func work(n int, done chan bool) {
	std.Printf("task %d\n", n)
	done <- true
}

func main() {
	done := make(chan bool)
	task work(1, done)
	task work(2, done)
	<-done
	<-done
}
