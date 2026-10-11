package main

// Or clauses (language spec section 11.27) that assign fields, map and
// slice elements and a loop variable, use err in closures and deferred
// calls, nest, and appear in select and switch clauses.

import (
	"pluto/errors"
	"pluto/fmt"
	"pluto/strconv"
)

var errOdd = errors.New("odd")

func half(n int) (int, error) {
	if n%2 != 0 {
		return n, errOdd
	}
	return n / 2, nil
}

func two(n int) (int, string, error) {
	h := half(n) or return
	return h, strconv.Itoa(h), nil
}

type rec struct {
	a int
	m map[string]int
}

// places assigns to fields, map elements and slice elements.
func places(n int) (rec, error) {
	r := rec{m: map[string]int{}}
	xs := []int{0, 0, 0}
	r.a = half(n) or return r, err
	r.m["k"] = half(n + 2) or return
	xs[1] = half(n + 4) or return
	_, s := two(n) or return
	fmt.Println("places", r.a, r.m["k"], xs[1], s)
	return r, nil
}

// captured uses err in a closure in the clause.
func captured(n int) (err2 error) {
	half(n) or {
		show := func() { fmt.Println("captured", err) }
		show()
		defer func() { fmt.Println("deferred sees", err) }()
		return err
	}
	return nil
}

// nested or statements in clauses and closures.
func nested(a, b int) error {
	half(a) or {
		half(b) or return fmt.Errorf("both: %w", err)
		return err
	}
	f := func(x int) int {
		h := half(x) or return -1
		return h
	}
	fmt.Println("nested", f(4), f(5))
	return nil
}

// selects uses or in the clauses of select and switch statements.
func selects(ch chan int) int {
	select {
	case v := <-ch:
		h := half(v) or return -v
		return h
	default:
		return 0
	}
}

func grow(i int) (int, error) {
	if i == 2 {
		return 0, errOdd
	}
	return i*2 + 1, nil
}

// loopVar: an or statement assigns the loop variable.
func loopVar() {
	for i := 0; i < 10; i++ {
		i = grow(i) or continue
		fmt.Println("loopVar", i)
	}
}

func main() {
	places(4)
	_, e := places(3)
	fmt.Println("places error", e)
	fmt.Println("captured", captured(3))
	fmt.Println(nested(3, 5), nested(3, 4), nested(2, 0))
	ch := make(chan int, 1)
	ch <- 6
	fmt.Println("selects", selects(ch))
	ch <- 7
	fmt.Println("selects", selects(ch))
	loopVar()
}
