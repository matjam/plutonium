package main

import "pluto/fmt"
type P struct{ x, y int }
func main() {
	s := "hello"
	fmt.Printf("%s %v %d\n", s, P{1, 2}, 3)
}
