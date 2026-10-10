package main

type T struct {
	buf []byte
	n   int
}

func f() []byte { return nil }

func g(b []byte) { println(len(b), cap(b), b == nil) }

func two() ([]int, int) { return nil, 7 }

func main() {
	g(nil)
	println(len(f()), f() == nil)
	s := []byte{'a', 'b', 'c'}
	println(len(s), s == nil)
	s = nil
	println(len(s), cap(s), s == nil)
	t := T{buf: []byte{'x', 'y'}, n: 3}
	t.buf = nil
	println(len(t.buf), t.n, t.buf == nil)
	u := T{buf: nil, n: 4}
	println(len(u.buf), u.n)
	a, n := two()
	println(len(a), n, a == nil)
	var p *int = nil
	println(p == nil)
}
