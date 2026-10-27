package main

func main() {
	var vs [3]int
	n := 0
top:
	v := n * 10
	vs[n] = v
	n++
	if n < 3 {
		goto top
	}
	println(vs[0], vs[1], vs[2])
}
