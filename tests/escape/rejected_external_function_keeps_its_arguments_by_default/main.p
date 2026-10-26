package main

//pu:extern ext_keep
func extKeep(p *int)
func main() {
	x := 1
	extKeep(&x)
}
