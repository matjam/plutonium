package main

// rt.Goexit ends the calling puroutine after the deferred calls of every
// function on it have run, innermost first, however the functions were
// called: directly, through a func value, an interface, a type parameter
// or a range-over-func loop. A deferred call that calls Goexit lets the
// deferred calls after it run. (In Go, with "runtime" for "pluto/rt", the
// program prints the same.)

import (
	"pluto/fmt"
	"pluto/rt"
)

var done = make(chan string)

func stop(where string) {
	defer fmt.Println("stop deferred:", where)
	rt.Goexit()
	fmt.Println("not reached: after Goexit")
}

func direct() {
	defer func() { done <- "direct" }()
	defer fmt.Println("direct deferred")
	stop("direct")
	fmt.Println("not reached: direct")
}

func viaValue(f func(string)) {
	defer func() { done <- "func value" }()
	f("func value")
	fmt.Println("not reached: func value")
}

type Stopper interface {
	Stop(where string) int
}

type halter struct{ name string }

func (h halter) Stop(where string) int {
	defer fmt.Println("Stop deferred:", h.name)
	stop(where)
	return 1
}

func viaInterface(x Stopper) {
	defer func() { done <- "interface" }()
	n := x.Stop("interface")
	fmt.Println("not reached: interface", n)
}

func viaTypeParam[T Stopper](x T) {
	defer func() { done <- "type parameter" }()
	n := x.Stop("type parameter")
	fmt.Println("not reached: type parameter", n)
}

// Seq is a closure constructor: an iterator over 0 to n-1.
func Seq(n int) func(yield func(int) bool) {
	return func(yield func(int) bool) {
		defer fmt.Println("iterator deferred")
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func viaRange() {
	defer func() { done <- "range" }()
	for i := range Seq(5) {
		if i == 2 {
			stop("range body")
		}
		fmt.Println("range body", i)
	}
	fmt.Println("not reached: range")
}

func inDefer() {
	defer func() { done <- "deferred Goexit" }()
	defer fmt.Println("runs after the deferred Goexit")
	defer stop("deferred")
	fmt.Println("inDefer returns")
}

func label(n int) string {
	if n > 0 {
		stop("in a result")
	}
	return "label"
}

func inExpression() {
	defer func() { done <- "expression" }()
	s := "x" + label(1) + "y"
	fmt.Println("not reached:", s)
}

func main() {
	go direct()
	fmt.Println(<-done)
	go viaValue(stop)
	fmt.Println(<-done)
	go viaInterface(halter{"interface"})
	fmt.Println(<-done)
	go viaTypeParam(halter{"type parameter"})
	fmt.Println(<-done)
	go viaRange()
	fmt.Println(<-done)
	go inDefer()
	fmt.Println(<-done)
	go inExpression()
	fmt.Println(<-done)
	fmt.Println("main returns")
}
