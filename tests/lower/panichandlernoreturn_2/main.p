package main

import "pluto/rt"

//pu:panic
func onPanic(p *rt.Panic) {
	println(p.Message)
}

func main() {}
