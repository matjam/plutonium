package main

// Types defined from a union or a packed type (language spec sections 7.13
// and 7.14): type V U makes a new union with U's variants, or a new packed
// type with U's fields and layout. V has none of U's methods, may declare
// its own, and converts to and from U explicitly. The program checks that
// the variants, payloads, fields and layout survive the conversions both
// ways, and that an owner union defined from another drops its payload once.

import (
	"pluto/fmt"
	"unsafe"
)

type Shape union {
	Circle float64
	Rect   struct{ w, h int }
	None
}

func (s Shape) Kind() string { return "shape" }

// Figure has Shape's variants and none of its methods.
type Figure Shape

func (f Figure) Area() float64 {
	switch v := f.(type) {
	case Circle:
		return 3 * v * v
	case Rect:
		return float64(v.w * v.h)
	case None:
		return 0
	}
}

func describe(s Shape) string {
	switch v := s.(type) {
	case Shape.Circle:
		return fmt.Sprintf("circle %v", float64(v))
	case Rect:
		return fmt.Sprintf("rect %dx%d", v.w, v.h)
	case None:
		return "none"
	}
}

type Result[T any] union {
	Ok  T
	Err string
}

// IntResult is defined from an instance of a generic union.
type IntResult Result[int]

func (r IntResult) Value() int {
	if v, ok := r.(IntResult.Ok); ok {
		return int(v)
	}
	return -1
}

// Ctrl is a packed control register; Ctrl2 has its fields and layout.
type Ctrl packed uint32 {
	enable bits[bool, 1]
	mode   bits[uint8, 3]
	_      bits[uint32, 4]
	div    bits[uint16, 16]
	_      bits[uint32, 8]
}

func (c Ctrl) On() bool { return c.enable }

type Ctrl2 Ctrl

// Entry is a packed union; Entry2 has its tag, variants and layout.
type Entry packed union uint16 {
	kind bits[uint8, 1]

	Small case 0 {
		n bits[uint16, 15]
	}
	Pair case 1 {
		a bits[uint8, 7]
		b bits[uint8, 8]
	}
}

type Entry2 Entry

func entry(e Entry2) string {
	switch v := e.(type) {
	case Small:
		return fmt.Sprintf("small %d", v.n)
	case Entry2.Pair:
		return fmt.Sprintf("pair %d %d", v.a, v.b)
	}
}

type File owner struct{ name string }

func (f *File) Drop() {
	if f.name != "" {
		fmt.Println("close", f.name)
	}
}

type Slot owner union {
	Empty
	Open File
}

// Slot2 is an owner union defined from Slot.
type Slot2 owner Slot

func main() {
	// A defined union has the variants, and converts both ways.
	f := Figure.Rect{w: 3, h: 4}
	fmt.Println(f.Area(), describe(Shape(f)), Shape(f).Kind())
	c := Figure(Shape.Circle(2))
	fmt.Println(c.Area(), describe(Shape(c)))
	var n Figure = Figure.None
	fmt.Println(n.Area(), describe(Shape(n)), n == Figure.None, c == Figure.Circle(2))
	back := Figure(Shape(Figure.Circle(1.5)))
	if v, ok := back.(Figure.Circle); ok {
		fmt.Println("circle again", float64(v))
	}
	fmt.Println(unsafe.Sizeof(f) == unsafe.Sizeof(Shape.None), unsafe.Alignof(f) == unsafe.Alignof(Shape.None))

	// A generic instance.
	r := IntResult.Ok(42)
	e := IntResult(Result[int].Err("bad"))
	fmt.Println(r.Value(), e.Value())
	if s, ok := Result[int](e).(Result[int].Err); ok {
		fmt.Println("err", string(s))
	}

	// A defined packed type keeps the fields and the layout.
	k := Ctrl2{enable: true, mode: 5, div: 0x1234}
	fmt.Printf("%#x %d %d %v\n", uint32(k), k.mode, k.div, k.enable)
	o := Ctrl(k)
	fmt.Printf("%#x %v %d\n", uint32(o), o.On(), o.div)
	k2 := Ctrl2(Ctrl(0x00abcd0b))
	fmt.Printf("%#x %d %d %v\n", uint32(k2), k2.mode, k2.div, k2.enable)
	k2.mode = 2
	fmt.Printf("%#x %#x\n", uint32(k2), uint32(Ctrl(k2)))
	fmt.Println(unsafe.Sizeof(k), unsafe.Sizeof(o), [4]byte(k2) == [4]byte(Ctrl(k2)))

	// A defined packed union keeps the tag and the variants.
	p := Entry2.Pair{a: 9, b: 200}
	fmt.Printf("%s %#x %#x\n", entry(p), uint16(p), uint16(Entry(p)))
	q := Entry2(Entry.Small{n: 77})
	fmt.Printf("%s %#x\n", entry(q), uint16(q))
	fmt.Println(entry(Entry2(uint16(0x0301))), unsafe.Sizeof(p))

	// An owner union defined from another moves its payload through the
	// conversions, and drops it once.
	{
		s := Slot2.Open(File{name: "a"})
		t := Slot(s)
		u := Slot2(t)
		switch v := u.(type) {
		case Open:
			fmt.Println("open", v.name)
		case Empty:
		}
	}
	{
		s := Slot2(Slot.Open(File{name: "b"}))
		_ = s
	}
	fmt.Println("done")
}
