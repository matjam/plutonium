package main

func main() {
	sum := 0
	n := 0
top:
	v := n
	f := func() int { return v * 2 }
	sum += f()
	n++
	if n < 3 {
		goto top
	}
	println(sum)
}
