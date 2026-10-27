package main

//pu:extern ext_read
//pu:noescape
func extRead(p *int) int
func main() {
	x := 1
	_ = extRead(&x)
}
