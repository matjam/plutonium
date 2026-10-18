package main

import "pluto/rt"

//pu:panic
func first(p *rt.Panic) {
	p.Print(rt.Console{})
	rt.Abort()
}

//pu:panic
func second(p *rt.Panic) {
	rt.Abort()
}

func main() {}
