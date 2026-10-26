package main

import "pluto/alloc"
//pu:extern ext_store
//pu:noescape
func extStore(dst **int, p *int)
func main() {
	x := 1
	extStore(alloc.New[*int](), &x)
}
