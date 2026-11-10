package main

type Shape union {
	Circle float64
	None
}

func (s Shape) Kind() string { return "shape" }

type Figure Shape

type Ctrl packed uint32 {
	mode bits[uint8, 4]
	_    bits[uint32, 28]
}

func (c Ctrl) Mode() uint8 { return c.mode }

type Ctrl2 Ctrl

type File owner struct{ fd int }

type Slot owner union {
	Empty
	Open File
}

type Slot2 Slot // a union with an owner payload must be declared owner

func noMethods(f Figure, c Ctrl2) (string, uint8) { return f.Kind(), c.Mode() }

func implicit(f Figure, c Ctrl2) (Shape, Ctrl) {
	var s Shape = f
	var k Ctrl = c
	return s, k
}

func back(s Shape) Figure { return s }

func exhaustive(f Figure) int {
	switch f.(type) {
	case Circle:
		return 1
	}
	return 0
}

func unknown() Figure { return Figure.Square(1) }
