// A range clause with = assigns the iteration values as an assignment
// statement does (language spec sections 12.7 and 12.10): in every
// iteration the operands of the left side are evaluated first, then the
// key and the value are stored in order. Function calls on the left are
// evaluated once per iteration.
package main

type Color enum {
	Red
	Green
	Blue
}

var slots [4]int
var calls int

func next() *int {
	p := &slots[calls]
	calls++
	return p
}

var cslots [3]Color
var ccalls int

func nextC() *Color {
	p := &cslots[ccalls]
	ccalls++
	return p
}

func pairs(yield func(int, int) bool) {
	for k := 0; k < 3; k++ {
		if !yield(k, (k+1)*10) {
			return
		}
	}
}

func reset() {
	slots = [4]int{}
	calls = 0
}

func main() {
	// The example of section 12.7.
	i := 2
	x := []int{3, 5, 7}
	for i, x[i] = range x {
		break
	}
	println(i, x[0], x[1], x[2])

	// Each value goes to the index the previous iteration left in i.
	i = 0
	x = []int{1, 2, 3}
	for i, x[i] = range x {
	}
	println(i, x[0], x[1], x[2])

	j := 0
	y := [3]int{1, 2, 3}
	for j, y[j] = range y {
	}
	println(j, y[0], y[1], y[2])

	k := 0
	var r [3]rune
	for k, r[k] = range "abc" {
	}
	println(k, int(r[0]), int(r[1]), int(r[2]))

	n := 0
	var z [3]int
	for n, z[n] = range pairs {
	}
	println(n, z[0], z[1], z[2])

	m := map[string]int{"a": 0}
	src := map[string]int{"a": 9}
	key := "a"
	for key, m[key] = range src {
	}
	println(key, m["a"])

	// The left side is evaluated in every iteration.
	reset()
	for *next() = range 3 {
	}
	println(calls, slots[0], slots[1], slots[2])

	reset()
	for _, *next() = range []int{7, 8, 9} {
	}
	println(calls, slots[0], slots[1], slots[2])

	reset()
	for *next() = range "hé!" {
	}
	println(calls, slots[0], slots[1], slots[2])

	for *nextC() = range Color {
	}
	println(ccalls, int(cslots[0]), int(cslots[1]), int(cslots[2]))

	reset()
	ch := make(chan int, 3)
	ch <- 4
	ch <- 5
	ch <- 6
	close(ch)
	for *next() = range ch {
	}
	println(calls, slots[0], slots[1], slots[2])

	reset()
	for _, *next() = range pairs {
	}
	println(calls, slots[0], slots[1], slots[2])
}
