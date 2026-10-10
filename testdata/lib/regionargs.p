// Calls that run again while what they returned before is still around
// (language spec section 4.5). A call's region is released and refilled
// when the call runs again, after its arguments are evaluated, so nothing
// the call built in it on its previous run may reach those arguments; the
// compiler rejects the programs where it could (E3101). These are the
// shapes that stay legal: closures used within the iteration, results of
// the previous run passed on as values (strings and the results of calling
// the closures), method values, closures of other calls of the same loop,
// a fixed number of compositions, and a slice of steps applied by one
// closure. Run under the debug allocator, nothing leaks and nothing is used
// after it is freed.
package main

import (
	"pluto/fmt"
	"pluto/strconv"
)

func count(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func filter(it func(func(int) bool), keep func(int) bool) func(func(int) bool) {
	return func(yield func(int) bool) {
		it(func(x int) bool {
			if keep(x) {
				return yield(x)
			}
			return true
		})
	}
}

// filterAll applies every step in one closure: what a loop of filter calls
// would build, in a region of fixed size.
func filterAll(it func(func(int) bool), mods []int) func(func(int) bool) {
	return func(yield func(int) bool) {
		it(func(x int) bool {
			for _, m := range mods {
				if x%m == 0 {
					return true
				}
			}
			return yield(x)
		})
	}
}

func sum(it func(func(int) bool)) int {
	t := 0
	for x := range it {
		t += x
	}
	return t
}

// greeter captures a counted string.
func greeter(name string) func() string {
	greeting := "hi " + name
	return func() string { return greeting }
}

type tag struct{ text string }

func (t tag) show() string { return "<" + t.text + ">" }

// shower returns a method value whose receiver copy holds a counted string.
func shower(text string) func() string {
	t := tag{text: text + strconv.Itoa(len(text))}
	return t.show
}

func main() {
	// Iterators composed within each iteration: every call's closure is
	// used before the call runs again.
	for n := 1; n <= 4; n++ {
		evens := 0
		for x := range filter(count(n*5), func(x int) bool { return x%2 == 0 }) {
			evens += x
		}
		odd := filter(count(n*5), func(x int) bool { return x%2 == 1 })
		t := 0
		for x := range odd {
			t += x
		}
		fmt.Println(n, evens, t)
	}

	// A fixed number of compositions, and a slice of steps.
	mods := []int{2, 3, 5}
	it := filter(filter(filter(count(40), func(x int) bool { return x%2 != 0 }), func(x int) bool { return x%3 != 0 }), func(x int) bool { return x%5 != 0 })
	fmt.Println(sum(it), sum(filterAll(count(40), mods)))

	// The previous run's string, passed on as a value: the result of
	// calling its closure is the caller's own string, so releasing the
	// region before the next run leaves it alone.
	name := "a" + strconv.Itoa(0)
	for i := 0; i < 4; i++ {
		g := greeter(name)
		name = g() + strconv.Itoa(i)
	}
	fmt.Println(name)

	// The same through a method value, and through a goto loop.
	text := "x" + strconv.Itoa(1)
	i := 0
again:
	{
		show := shower(text)
		text = show()
	}
	i++
	if i < 3 {
		goto again
	}
	fmt.Println(text)

	// Through a func value: the call reserves the largest region of its
	// signature, refilled on every run.
	mk := greeter
	for j := 0; j < 3; j++ {
		if j == 1 {
			mk = shower
		}
		text = mk(text)()
	}
	fmt.Println(text)
}
