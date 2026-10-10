package main

type shape interface {
	area(scale int) int
}

type rect struct{ w, h int }

func (r *rect) area(scale int) int {
	if scale < 0 {
		panic("negative scale")
	}
	return r.w * r.h / scale
}

func run(f func(int) int, n int) int {
	return f(n) + 1
}

func main() {
	var s shape = &rect{w: 2, h: 3}
	println(s.area(1))
	println(run(func(n int) int {
		return s.area(n - 5)
	}, 3))
}
