package main

// Composite literal keys may name promoted fields (language spec section
// 11.3): the key selects the field inside the embedded struct.

type Object struct{ name, color string }

type Point3D struct {
	Object
	x, y, z float64
}

type Line struct {
	Object
	p, q Point3D
}

type Inner struct{ a, b int }

type Middle struct {
	tag byte
	Inner
}

type Outer struct {
	n int
	Middle
	last *int
}

// A package-level literal with promoted keys.
var global = Outer{a: 7, n: 1, tag: 'g', b: 8}

var seven = 7

var withPtr = Outer{last: &seven, b: 3}

//pu:noinline
func ret(v int) Outer { return Outer{b: v, a: v * 2} }

func main() {
	origin := Point3D{}
	line1 := Line{Object{}, origin, Point3D{y: -4, z: 12.3}}
	println(line1.q.x, line1.q.y, line1.q.z)
	line2 := Line{name: "diagonal", q: Point3D{x: 1, y: 1, z: 1}}
	println(line2.name, line2.color == "", line2.q.x, line2.p.x)
	p := Point3D{color: "red", z: 2, name: "pt"}
	println(p.name, p.color, p.x, p.z)

	// Two levels of embedding, with fields around the embedded ones.
	o := Outer{b: 2, n: 5, a: 1, tag: 't'}
	println(o.n, o.tag, o.a, o.b, o.last == nil)
	println(global.n, global.tag, global.a, global.b)
	println(*withPtr.last, withPtr.b, withPtr.a)
	r := ret(4)
	println(r.a, r.b)

	// Nested and pointer literals.
	ps := []*Point3D{{name: "a", x: 1}, {color: "b"}}
	println(ps[0].name, ps[0].x, ps[1].color)
	m := map[string]Outer{"k": {a: 9}}
	println(m["k"].a, m["k"].b)
}
