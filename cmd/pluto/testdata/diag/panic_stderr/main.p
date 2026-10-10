package main

import (
	"pluto/os"
	"pluto/rt"
)

// The panic handler writes to os.Stderr, and the program makes a pipe, so
// a write to a File may wait for the poller.
//
//pu:panic
func onPanic(p *rt.Panic) {
	os.Stderr.WriteString("panicked\n")
	os.Exit(3)
}

func main() {
	r, w, _ := os.Pipe()
	r.Close()
	w.Close()
	panic("boom")
}
