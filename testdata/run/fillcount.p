package main

// Byte-fill loops and conditional counters.

var big [1000]bool
var bytes [600]byte

//pu:noinline
func fillAll() {
	for i := 0; i < len(big); i++ {
		big[i] = true
	}
}

//pu:noinline
func fillRange() int {
	i := 100
	for ; i < 500; i++ {
		bytes[i] = 7
	}
	return i
}

//pu:noinline
func fillShort(n int) {
	for i := 0; i < n; i++ {
		bytes[i] = 9
	}
}

//pu:noinline
func count(xs []bool) (int, int, int8) {
	yes, no := 0, 0
	down := int8(0)
	for _, x := range xs {
		if x {
			yes++
		}
		if !x {
			no++
		} else {
			down--
		}
	}
	return yes, no, down
}

//pu:noinline
func countLess(xs []int, k int) int {
	n := 0
	for _, x := range xs {
		if x < k {
			n += 1
		}
	}
	return n
}

func main() {
	fillAll()
	big[3] = false
	big[999] = false
	y, n, d := count(big[:])
	println(y, n, d)
	println(fillRange(), bytes[99], bytes[100], bytes[499], bytes[500])
	fillShort(10)
	println(bytes[0], bytes[9], bytes[10])
	println(countLess([]int{5, -3, 9, 2, 7, 1}, 5))
}
