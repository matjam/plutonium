package main

type Item struct{ x int }

func mk() owned[Item] { return new(Item{x: 7}) }

func read(p *int) int { return *p }

func main() {
	println(read(&mk().x))
	if p := &mk().x; *p > 0 {
		println(*p)
	}
	p := &mk().x
	println(*p)
}
