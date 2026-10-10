package main

import "pluto/fmt"

// A function called in a loop keeps a view of the loop's memory (language
// spec sections 4.5 and 6.3).

type Point struct{ x, y int }

func put(buf []*int, i int, p *int) { buf[i] = p }

func putFn(fs []func() int, i int, f func() int) { fs[i] = f }

func putPoint(ps []*Point, i int, p *Point) { ps[i] = p }

func main() {
	var arr [4]*int
	var fs [4]func() int
	var ps [4]*Point
	for i := 0; i < 3; i++ {
		v := i
		put(arr[:], i, &v)
		f := func() int { return i * 10 }
		putFn(fs[:], i, f)
		putPoint(ps[:], i, &Point{i, i})
	}
	fmt.Println(len(arr), len(fs), len(ps))
}
