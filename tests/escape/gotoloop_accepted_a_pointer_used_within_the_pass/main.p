package main

func main() {
	sum := 0
	n := 0
top:
	v := n
	p := &v
	sum += *p
	n++
	if n < 3 {
		goto top
	}
	println(sum)
}
