package main

// Loops with small constant trip counts, which the optimiser unrolls, and
// some it must not.

type Body struct{ x, y, vx, vy, m float64 }

var bodies [5]Body
var grid [4][4]int64

//pu:noinline
func pairs(dt float64) {
	for i := 0; i < len(bodies); i++ {
		bi := &bodies[i]
		for j := i + 1; j < len(bodies); j++ {
			bj := &bodies[j]
			dx, dy := bi.x-bj.x, bi.y-bj.y
			d2 := dx*dx + dy*dy + 0.01
			mag := dt / d2
			bi.vx -= dx * bj.m * mag
			bi.vy -= dy * bj.m * mag
			bj.vx += dx * bi.m * mag
			bj.vy += dy * bi.m * mag
		}
	}
	for i := 0; i < len(bodies); i++ {
		b := &bodies[i]
		b.x += dt * b.vx
		b.y += dt * b.vy
	}
}

//pu:noinline
func triangle() int64 {
	s := int64(0)
	for i := 0; i <= 4; i++ {
		for j := 0; j < i; j++ {
			grid[i&3][j] += int64(i*10 + j)
			s += grid[i&3][j]
		}
	}
	return s
}

//pu:noinline
func steps() (int, int, int) {
	a := 0
	i := 1
	for ; i < 20; i += 3 {
		a = a*3 + i
	}
	b := 0
	for k := 10; k <= 10; k++ {
		b += k
	}
	c := 0
	for k := 7; k < 7; k++ {
		c++
	}
	return a + i, b, c
}

//pu:noinline
func narrow() int {
	n := 0
	for i := int8(120); i < 127; i++ {
		n += int(i)
	}
	return n
}

//pu:noinline
func early(k int) int {
	// Two ways out: stays a loop.
	s := 0
	for i := 0; i < 8; i++ {
		if i*k > 12 {
			break
		}
		s += i
	}
	return s
}

//pu:noinline
func limits() (int, int) {
	a, b := 0, 0
	for i := 0; i < 16; i++ {
		a += i * i
	}
	for i := 0; i < 17; i++ {
		b += i * i
	}
	return a, b
}

func main() {
	for i := range bodies {
		bodies[i] = Body{float64(i) * 1.5, float64(i*i) - 3, 0.1 * float64(i), -0.2, 1 + float64(i)/4}
	}
	for range 100 {
		pairs(0.01)
	}
	for _, b := range bodies {
		println(int64(b.x*1e9), int64(b.y*1e9), int64(b.vx*1e9), int64(b.vy*1e9))
	}
	println(triangle(), triangle())
	println(steps())
	println(narrow())
	println(early(3), early(100), early(0))
	println(limits())
}
