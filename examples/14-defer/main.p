package main

import (
	"pluto/alloc"
	"pluto/fmt"
)

// defer for scope-bound cleanup: deferred calls run in reverse order when
// the function returns, with arguments evaluated at the defer statement.

// Resource has a Drop method, so it is declared owner (language spec 6.4).
type Resource owner struct {
	name string
	open bool
}

func (r *Resource) Drop() {
	r.open = false
	fmt.Printf("dropped %s\n", r.name)
}

func use(name string) {
	r := alloc.New[Resource]()
	defer alloc.Free(r) // calls r.Drop() before freeing
	r.name = name
	r.open = true
	fmt.Printf("using %s (open=%t)\n", r.name, r.open)
}

func order() (result int) {
	defer func() { result *= 10 }()
	for i := 1; i <= 3; i++ {
		result += i
	}
	x := 5
	defer fmt.Printf("deferred x=%d\n", x) // x is evaluated now
	x = 6
	return result
}

func main() {
	use("first")
	use("second")
	fmt.Printf("order=%d\n", order())
}
