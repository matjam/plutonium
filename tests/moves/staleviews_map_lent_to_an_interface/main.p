package main

// A map or channel in the data word of an ordinary interface is lent: the
// interface holds the variable's word without a count of its own, so it
// is a view of the variable, ended when the variable gets a new value
// (language spec section 7.9 and section 18.3, Views).

func main() {
	m := map[int]int{1: 1}
	var x any = m
	m = map[int]int{}
	println(len(x.(map[int]int)))

	c := make(chan int, 1)
	var y any = c
	c = nil
	println(cap(y.(chan int)))
}
