package main

func main() {
	var g func() int
	for i, v := range [2]int{5, 6} {
		if i == 1 {
			g = func() int { return v + i }
		}
	}
	println(g())
}
