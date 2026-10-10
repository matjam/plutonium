package main

import (
	"pluto/fmt"
	"unsafe"
)

// Default parameter values (language spec section 5.1).

var calls int

func next() int {
	calls++
	return calls
}

func trace(name string, v int) int {
	fmt.Printf("eval %s=%d\n", name, v)
	return v
}

// Defaults are evaluated at each call, after the explicit arguments.
func add(a int, b int = trace("b", 10), c int = trace("c", next())) int {
	return a + b + c
}

// A default reads package state when the call runs, not when the function
// is declared.
var scale = 1

func mul(x int, k int = scale) int { return x * k }

type Point struct{ X, Y int }

func origin(p Point = Point{1, 2}) Point { return p }

func label(s string = "none", n int = len("four")) {
	fmt.Printf("%s/%d\n", s, n)
}

func double(x int) int { return 2 * x }

func apply(x int, f func(int) int = double) int { return f(x) }

func applyLit(x int, f func(int) int = func(v int) int { return v + 100 }) int { return f(x) }

// Generic defaults may use the function's type parameters.
func sizeOf[T any]() int {
	var z T
	return int(unsafe.Sizeof(z))
}

func sized[T any](n int = sizeOf[T]()) int { return n }

func pick[T any](x T, y T = x0[T]()) T { return y }

func x0[T any]() T {
	var z T
	return z
}

type Counter struct{ n int }

func (c *Counter) Add(d int = 1) int {
	c.n += d
	return c.n
}

// A method of a generic type may use the receiver's type parameters.
type Box[T any] struct{ v T }

func (b *Box[T]) Size(n int = sizeOf[T]()) int { return n }

// A method with type parameters of its own may use them too.
func (b *Box[T]) Both[U any](u U, n int = sizeOf[T]() + sizeOf[U]()) int { return n }

// Promoted methods keep their defaults.
type Outer struct{ Counter }

type Adder interface{ Add(d int) int }

func pair() (int, int) { return 1000, 2000 }

func show(tag string, n int = next()) {
	fmt.Printf("%s %d\n", tag, n)
}

// A package-level initializer may call a function with defaults.
var initial = add(100, 1)

// The variables a default uses are initialised before any initializer
// that calls the function, as those its body uses are.
var early = withLate()

func withLate(a int = late + 1) int { return a }

var late = lateValue()

func lateValue() int { return 41 }

func main() {
	fmt.Println("initial", initial)
	fmt.Println("early", early, late)

	fmt.Println(add(1, 2, 3))
	fmt.Println(add(1, 2))
	fmt.Println(add(1))

	fmt.Println(mul(5))
	scale = 3
	fmt.Println(mul(5))
	fmt.Println(mul(5, 7))

	p := origin()
	fmt.Println(p.X, p.Y)
	q := origin(Point{7, 8})
	fmt.Println(q.X, q.Y)

	label()
	label("x")
	label("y", 9)

	fmt.Println(apply(21))
	fmt.Println(apply(21, func(v int) int { return v - 1 }))
	fmt.Println(applyLit(1))

	fmt.Println(sized[int64](), sized[int8](), sized[Point](), sized[byte](5))
	fmt.Println(pick(42), pick("s") == "", pick(1, 2))

	var c Counter
	c.Add()
	c.Add()
	fmt.Println(c.Add(10))

	// A method value has the method's full signature.
	f := c.Add
	fmt.Println(f(100))

	// So does a call through an interface.
	var a Adder = &c
	fmt.Println(a.Add(1000))

	var b Box[[3]int32]
	fmt.Println(b.Size(), b.Size(1))
	fmt.Println(b.Both(int16(1)), b.Both[int64](1), b.Both("s", 3))

	var o Outer
	o.Add()
	fmt.Println(o.Add())

	// f(g()) with g returning fewer values than f has parameters.
	fmt.Println(add(pair()))

	// A deferred call evaluates its defaults with its other arguments,
	// when the defer statement runs.
	calls = 40
	func() {
		defer show("deferred")
		calls = 50
		show("direct")
	}()
	defer c.Add()
	fmt.Println("calls", calls)
}
