package main

import (
	"pluto/fmt"
	"pluto/os"
)

func work(name string, done chan bool) {
	if f, err := os.Open(name); err == nil {
		f.Close()
	}
	fmt.Println("done with", name)
	done <- true
}

func main() {
	done := make(chan bool)
	task work("a", done)
	task work("b", done)
	<-done
	<-done
}
