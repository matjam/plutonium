package main

func main() {
	var p *int
	n := 1
	if n > 0 {
		goto skip
	}
	println("not reached")
skip:
	v := n
	p = &v
	println(*p)
}
