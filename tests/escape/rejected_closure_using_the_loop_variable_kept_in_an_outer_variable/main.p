package main

func main() {
	var f0 func() int
	for i := 0; i < 3; i++ {
		f := func() int { return i * 10 }
		if i == 0 {
			f0 = f
		}
	}
	println(f0())
}
