package main

// A closure built in a loop that captures only variables declared outside
// it is the same on every iteration, so it may be kept past the loop
// (section 18.4). So may one that captures the header's variables when the
// loop is left at once.
func main() {
	total := 0
	var add func(int)
	for i := 1; i <= 3; i++ {
		add = func(n int) { total += n }
		add(i)
	}
	add(10)
	println(total)

	var kept func() int
	for i, v := range [3]int{5, 6, 7} {
		if v == 6 {
			kept = func() int { return i + v }
			break
		}
	}
	println(kept())
}
