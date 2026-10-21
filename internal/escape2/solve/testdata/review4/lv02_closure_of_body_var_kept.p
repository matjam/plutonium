package main

// A closure that captures a variable of the loop's body, kept past the
// iteration: the body's variable is made again by the next iteration, in
// the same memory, so the kept closure would see its values (section 18.4).
func main() {
	var fs []func() int
	var kept func() int
	for i := 0; i < 3; i++ {
		x := i * 10
		f := func() int { return x }
		if i == 0 {
			kept = f
		}
	}
	_ = fs
	println(kept())
}
