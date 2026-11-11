package main

import (
	"pluto/fmt"
	"pluto/os"
)

func closer(done chan bool) {
	f := take(os.Stdout)
	_ = f
	done <- true
}

func printer(done chan bool) {
	fmt.Println("hi")
	done <- true
}

func main() {
	done := make(chan bool)
	task printer(done)
	task closer(done)
	<-done
	<-done
}
