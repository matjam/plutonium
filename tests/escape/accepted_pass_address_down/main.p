package main

func set(p *int) { *p = 2 }
func main() {
	x := 1
	set(&x)
	_ = x
}
