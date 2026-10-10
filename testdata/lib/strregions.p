// Strings captured by closures that functions return: the region the
// caller provides holds them, releases what it held when a call refills
// it, and releases the rest when the caller returns (language spec
// sections 4.5 and 6.10). Run under the debug allocator, nothing leaks
// and nothing is used after it is freed.
package main

import (
	"pluto/fmt"
	"pluto/strconv"
	"pluto/strings"
)

func heap(s string) string { return s + strconv.Itoa(len(s)) }

// greeter captures a heap string parameter.
func greeter(name string) func() string {
	greeting := "hi " + name
	return func() string { return greeting }
}

// pieces returns an iterator over a string it builds.
func pieces(n int) func(func(string) bool) {
	s := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			s += ","
		}
		s += strconv.Itoa(i)
	}
	return func(yield func(string) bool) {
		for p := range strings.SplitSeq(s, ",") {
			if !yield(p) {
				return
			}
		}
	}
}

// labelled returns a closure through another function's.
func labelled(label string) func() string {
	g := greeter(label)
	return func() string { return g() + "!" }
}

type tag struct{ text string }

func (t tag) show() string { return "<" + t.text + ">" }

// shower returns a method value whose receiver copy holds a string.
func shower(text string) func() string {
	t := tag{text: heap(text)}
	return t.show
}

// named returns a closure from a named result that holds a string.
func named(s string) (f func() string, last string) {
	last = heap(s)
	f = func() string { return last }
	return
}

func main() {
	total := 0
	for i := 0; i < 50; i++ {
		g := greeter(heap("x"))
		total += len(g())
		n := 0
		for p := range pieces(i % 5) {
			n += len(p)
		}
		total += n
		total += len(labelled(heap("y"))())
		total += len(shower("z")())
	}
	fmt.Println("total", total)

	f, last := named("abc")
	fmt.Println(f(), last)

	var mk func(string) func() string = greeter
	for i := 0; i < 3; i++ {
		h := mk(heap("via value"))
		fmt.Println(h())
	}
}
