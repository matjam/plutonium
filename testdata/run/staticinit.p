package main

// Package-level arrays and structs made of constants are initialised
// statically; the rest still run in the package initialiser.

type point struct {
	x, y int32
	name string
	ok   bool
}

type table struct {
	scale  float64
	coeffs [4]float64
	pts    [2]point
}

var coeffs = [...]float64{1.5, -2.25, 3e-10, 0}

var sparse = [8]uint16{2: 7, 5: 9, 11}

var origin = point{name: "origin", ok: true}

var tbl = table{
	scale:  0.5,
	coeffs: [4]float64{1, 2},
	pts:    [2]point{{1, 2, "a", false}, {x: -3, name: "b"}},
}

var names = [3]string{"zero", "", "two"}

var dynamic = [2]int{len(names), cap(sparse[:])}

func milli(f float64) int64 { return int64(f * 1000) }

func main() {
	println(milli(coeffs[0]), milli(coeffs[1]), coeffs[2] > 0, milli(coeffs[3]))
	for _, v := range sparse {
		print(v, ",")
	}
	println()
	println(origin.name, origin.ok, origin.x, origin.y)
	println(milli(tbl.scale), milli(tbl.coeffs[0]), milli(tbl.coeffs[1]), milli(tbl.coeffs[3]))
	println(tbl.pts[0].x, tbl.pts[0].y, tbl.pts[0].name, tbl.pts[1].x, tbl.pts[1].name, tbl.pts[1].ok)
	println(names[0], len(names[1]), names[2])
	println(dynamic[0], dynamic[1])
	coeffs[1] = 4
	names[1] = "one"
	println(milli(coeffs[1]), names[1])
}
