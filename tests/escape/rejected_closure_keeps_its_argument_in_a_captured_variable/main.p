package main

func each(f func(p *int)) {
	x := 1
	f(&x)
}
func main() {
	var last *int
	each(func(p *int) { last = p })
	_ = last
}
