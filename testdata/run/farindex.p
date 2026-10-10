package main

// Guarded loads at constant indices whose byte offsets do not fit in a
// 32-bit displacement, from slices of unknown length. The address is base
// plus a huge constant; instruction selection used to recurse forever on
// it. Found by tools/difftest.

type pair struct {
	a, b int64
}

//pu:noinline
func get(s []int) int {
	k := 872395105224214373
	if uint(k) < uint(len(s)) {
		return s[k]
	}
	return -1
}

//pu:noinline
func getNeg(s []int32) int32 {
	k := -1 << 40
	if k >= 0 && k < len(s) {
		return s[k]
	}
	return -2
}

//pu:noinline
func getPair(s []pair) int64 {
	k := 1 << 35
	if uint(k) < uint(len(s)) {
		return s[k].b
	}
	return -3
}

//pu:noinline
func setFar(s []uint8) int {
	k := 1 << 40
	if k < len(s) {
		s[k] = 7
		return 1
	}
	return 0
}

func main() {
	s := []int{10, 20, 30}
	println(get(s), getNeg([]int32{1, 2}), getPair([]pair{{1, 2}, {3, 4}}), setFar([]uint8{1}))
}
