// An alias names the type it aliases, so a value of an alias type held in an
// interface has that type: the assertion to it succeeds (section 10.8).
package main

type A = int

func main() {
	x := 3
	var i any = A(x)
	_, ok := i.(int)
	var j any = 4
	_, ok2 := j.(A)
	println(ok, ok2)
}
