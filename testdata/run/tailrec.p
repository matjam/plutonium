package main

// Self-recursive calls in tail position, plain and accumulating, become
// loops. The results must match the recursive definitions.

func fib(n int64) int64 {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func gcd(a, b int) int {
	if b == 0 {
		return a
	}
	return gcd(b, a%b)
}

func fact(n uint32) uint32 {
	if n <= 1 {
		return 1
	}
	return uint32(n) * fact(n-1)
}

func sumTo(n int, k int8) int8 {
	if n == 0 {
		return k
	}
	return sumTo(n-1, k) + int8(n)
}

func bits(x uint64) uint64 {
	if x == 0 {
		return 0
	}
	return (x & 0xff) ^ bits(x>>8)
}

func orAll(xs []uint16) uint16 {
	if len(xs) == 0 {
		return 0
	}
	return orAll(xs[1:]) | xs[0]
}

func andAll(xs []uint16) uint16 {
	if len(xs) == 0 {
		return 0xffff
	}
	return xs[0] & andAll(xs[1:])
}

// Mixed operations: only the first kind found accumulates.
func mixed(n int) int {
	if n <= 0 {
		return 1
	}
	if n%3 == 0 {
		return mixed(n-1) * 2
	}
	return mixed(n-1) + n
}

var trace [8]int
var ntrace int

func walk(n int) {
	if n == 0 {
		return
	}
	trace[ntrace] = n
	ntrace++
	walk(n - 1)
}

func scale(x float64, n int) float64 {
	if n == 0 {
		return x
	}
	return scale(x*1.5, n-1)
}

// Two results: left alone.
func divmod(a, b, q int) (int, int) {
	if a < b {
		return q, a
	}
	return divmod(a-b, b, q+1)
}

// A local whose address reaches the recursive call: left alone.
func chain(n int, prev *int) int {
	x := n
	if n == 0 {
		return *prev
	}
	return chain(n-1, &x) + *prev
}

func main() {
	println(fib(30), gcd(1071, 462), fact(13), sumTo(100, 3), bits(0x0102040810204080))
	println(orAll([]uint16{1, 2, 4, 0x100}), andAll([]uint16{0xff0f, 0x0fff, 0xf0ff}), mixed(10))
	walk(5)
	println(ntrace, trace[0], trace[4])
	println(int64(scale(2, 4) * 1000))
	q, r := divmod(47, 5, 0)
	println(q, r)
	z := 7
	println(chain(4, &z))
}
