package main

func main() {
	v := 0
	var p *int
top:
	p = &v
	v++
	if v < 3 {
		goto top
	}
	println(*p)
}
