package main

import (
	"pluto/fmt"
	"pluto/io"
	"pluto/os"
	"pluto/sync"
)

type Logger struct {
	mu     sync.Mutex
	w      io.Writer
	prefix string
}

func (l *Logger) Printf(format string, args ...any) {
	l.mu.Lock()
	fmt.Fprintf(l.w, l.prefix+format, args...)
	l.mu.Unlock()
}

// Buf counts what is written to it, with no lock of its own.
type Buf struct{ n int }

func (b *Buf) Write(p []byte) (int, error) {
	b.n += len(p)
	return len(p), nil
}

var buf = &Buf{}

var std = &Logger{w: os.Stderr, prefix: "app: "}

func init() {
	std.w = buf
}

func work(n int, done chan bool) {
	std.Printf("task %d\n", n)
	done <- true
}

func main() {
	done := make(chan bool)
	task work(1, done)
	task work(2, done)
	fmt.Fprintf(buf, "main\n")
	<-done
	<-done
}
