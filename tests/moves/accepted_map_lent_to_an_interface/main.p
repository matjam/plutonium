package main

// An interface made from a map or channel may be used while the variable
// keeps that map or channel, and a new one lent after the variable's new
// value is fine (language spec sections 7.9 and 18.3).

func main() {
	m := map[int]int{1: 1}
	var x any = m
	println(len(x.(map[int]int)))
	m = map[int]int{}
	x = m
	println(len(x.(map[int]int)))

	c := make(chan int, 1)
	y := any(c)
	println(cap(y.(chan int)))
}
