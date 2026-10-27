package main

func main() {
	var first []int
	n := 0
top:
	var a [2]int
	a[0] = n
	if n == 0 {
		first = a[:]
	}
	n++
	if n < 3 {
		goto top
	}
	println(first[0])
}
