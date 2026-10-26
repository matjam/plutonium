package main

func main() {
	var p *int
	func() {
		y := 1
		p = &y
	}()
	_ = p
}
