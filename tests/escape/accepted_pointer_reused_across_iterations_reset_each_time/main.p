package main

func main() {
	var p *int
	for i := 0; i < 3; i++ {
		x := i
		p = &x
		println(*p)
		p = nil
	}
}
