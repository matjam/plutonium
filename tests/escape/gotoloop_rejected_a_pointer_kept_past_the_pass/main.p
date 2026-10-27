package main

func main() {
	var ps [3]*int
	n := 0
top:
	v := n
	ps[n] = &v
	n++
	if n < 3 {
		goto top
	}
	println(*ps[0], *ps[1], *ps[2])
}
