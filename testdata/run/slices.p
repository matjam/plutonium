package main

func total(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func reverse(xs []int) {
	for i, j := 0, len(xs)-1; i < j; i, j = i+1, j-1 {
		xs[i], xs[j] = xs[j], xs[i]
	}
}

func bubble(xs []int) {
	for i := 0; i < len(xs); i++ {
		for j := 0; j+1 < len(xs)-i; j++ {
			if xs[j] > xs[j+1] {
				xs[j], xs[j+1] = xs[j+1], xs[j]
			}
		}
	}
}

var global [100]int

func main() {
	var arr [10]int
	for i := range arr {
		arr[i] = i * i
	}
	s := arr[2:7]
	println("slice", len(s), cap(s), s[0], s[4], total(s))
	s2 := s[1:3:4]
	println("3-index", len(s2), cap(s2), s2[0], s2[1])
	reverse(arr[:])
	println("reversed", arr[0], arr[9], total(arr[:]))
	buf := arr[:4]
	buf[0], buf[1], buf[2], buf[3] = 5, 3, 9, 1
	buf = buf[:5]
	buf[4] = 7
	println("append", len(buf), cap(buf), buf[0], buf[4], arr[3])
	bubble(buf)
	println("sorted", buf[0], buf[1], buf[2], buf[3], buf[4])
	n := copy(arr[5:], []int{100, 200, 300})
	println("copy", n, arr[5], arr[6], arr[7], arr[8])
	m := [3][4]int{}
	for i := 0; i < 3; i++ {
		for j := 0; j < 4; j++ {
			m[i][j] = i*10 + j
		}
	}
	println("matrix", m[2][3], m[1][2], len(m), len(m[0]))
	for i := range global {
		global[i] = i
	}
	println("global", total(global[:]), global[99])
	p := &arr
	p[0] = -1
	println("ptr array", arr[0], len(p))
	lit := []string{"a", "bb", "ccc"}
	l := 0
	for _, str := range lit {
		l += len(str)
	}
	println("strings", l, lit[2])
	var nilSlice []int
	println("nil slice", len(nilSlice), nilSlice == nil)
	two := [...]int{1: 10, 3: 30}
	println("keyed", len(two), two[0], two[1], two[3])
}
