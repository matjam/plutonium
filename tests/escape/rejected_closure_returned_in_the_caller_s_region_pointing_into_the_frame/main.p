package main

func mk() func() int {
	x := 3
	p := &x
	return func() int { return *p }
}
func main() { println(mk()()) }
