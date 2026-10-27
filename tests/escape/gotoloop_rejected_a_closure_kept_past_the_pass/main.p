package main

func main() {
	var fs [3]func() int
	n := 0
top:
	v := n
	fs[n] = func() int { return v }
	n++
	if n < 3 {
		goto top
	}
	println(fs[0](), fs[1](), fs[2]())
}
