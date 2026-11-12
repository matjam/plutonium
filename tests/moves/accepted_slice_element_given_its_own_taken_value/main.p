package main

// Assigning to a place that is not known to be empty drops its old
// contents, so Drop sees the zero value for a[0] and a[1] and again after
// s[0] = take(&s[0]) empties s[0]. The taken value moves back in and each
// value is dropped once (language spec sections 18.5, 18.6).

type L owner struct{ x int }

func (l *L) Drop() { println("drop", l.x) }

func mk(x int) L { return L{x: x} }

func main() {
	var a [2]L
	a[0] = mk(1)
	a[1] = mk(2)
	s := a[:]
	s[0] = take(&s[0])
	println("end", s[0].x)
}
