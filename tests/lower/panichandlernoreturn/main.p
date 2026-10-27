package main

import (
	"pluto/os"
	"pluto/rt"
)

//pu:panic
func onPanic(p *rt.Panic) {
	println(p.Message)
	os.Exit(3)
}

func main() {}
