package main

type Color enum {
	Red
	Green
	Blue
}

type Errno enum uint16 {
	OK     = 0
	EPERM
	ENOENT
	EIO    = 5
}

type Order enum { Less = -1; Equal; Greater }

type Big enum { Zero; Huge = 1 << 40 }

type Stringer interface{ String() string }

type Named enum { A; B }

func (n Named) String() string { return "named" }

// Shade has a method with a member's name; Shade.Dark is the member.
type Shade enum { Light; Dark }

func (s Shade) Dark() bool { return s == Shade.Dark }

type Reg packed uint8 {
	color Color
	order Order
	wide  bits[Color, 4]
}

func Max[T interface{ ~uint8 }](a, b T) T {
	if a > b {
		return a
	}
	return b
}

var names = [len(Color)]string{Color.Red: "red", Color.Green: "green", Color.Blue: "blue"}

const n = len(Errno)

func use(c Color, raw int, s Shade) (string, bool) {
	switch c {
	case Red:
		return names[c], true
	case Color.Green, Blue:
	}
	switch c {
	case Red:
	default:
	}
	x, ok := Color(raw)
	var y, ok2 = Errno(uint16(raw))
	_, _ = y, ok2
	ok = ok && x == Color.Blue && c < Color(2) && Max(c, x) != Color.Red
	var str Stringer = c
	_ = str.String() + Named.A.String() + Color.Red.String()
	for v := range Color {
		_ = v
	}
	var r Reg
	r.color = Color.Blue
	_ = 1 << c
	_ = uint8(c) + 1
	_ = s.Dark()
	return Shade.Dark.String(), ok
}

// An exhaustive switch with no default case is a terminating statement.
func word(c Color) string {
	switch c {
	case Red:
		return "red"
	case Green, Blue:
		panic("cool")
	}
}

func local() {
	type Dir enum { North; South }
	_ = Dir.South.String()
}
