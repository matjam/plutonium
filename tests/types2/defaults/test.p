package main

type Allocator interface{ Alloc(n int) int }

type heap struct{}

func (heap) Alloc(n int) int { return n }

func Default() Allocator { return heap{} }

var calls int

func New[T any](a Allocator = Default()) *T { return nil }

func Grow(n int, factor int = 2, limit int = 1 << 20) int { return n * factor }

func Sized[T any](n int = sizeOf[T]()) int { return n }

func sizeOf[T any]() int { return 0 }

func Pick[T any](x T, y T = zero[T]()) T { return y }

func zero[T any]() T {
	var z T
	return z
}

func Pair() (int, int) { return 1, 2 }

type Counter struct{ n int }

func (c *Counter) Add(d int = 1) int { return c.n + d }

type Box[T any] struct{ v T }

func (b *Box[T]) Size(n int = sizeOf[T]()) int { return n }

type Adder interface{ Add(d int) int }

// Parameters shadow package-level names in the body, but a default never
// sees them; the default here names the package-level n.
var n = 5

func Shadow(x int, y int = n) int { return y }

// A function literal in a default declares its own parameters.
func Apply(x int, f func(int) int = func(x int) int { return x + 1 }) int { return f(x) }

func use() {
	_ = New[int]()
	_ = New[int](heap{})
	_ = Grow(1)
	_ = Grow(1, 3)
	_ = Grow(1, 3, 4)
	_ = Grow(Pair())
	_ = Sized[int]()
	_ = Pick(1)
	_ = Pick("a", "b")
	var c Counter
	_ = c.Add()
	_ = c.Add(2)
	var b Box[string]
	_ = b.Size()
	var a Adder = &c // defaults do not change the method's type
	_ = a.Add(3)
	var f func(int) int = c.Add
	_ = f(4)
	var g func(int, int, int) int = Grow
	_ = g(1, 2, 3)
	defer Grow(1)
	_ = Shadow(1)
	_ = Apply(1)
}
