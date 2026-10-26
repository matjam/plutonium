package main

// a user package
type R owner struct{ p *int }
//pu:resultholds p
func hold(p *int) R { return R{p} }
func main() { _ = hold(nil) }
