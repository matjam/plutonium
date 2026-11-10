package main

type Color enum { Red; Green; Blue }

type Shade enum { Light; Dark; Blue }

// Plain is an ordinary integer type with Color's underlying type.
type Plain Color

func constant() Shade { return Shade(uint8(Color.Green)) }

func checked(c Color) (Shade, bool) {
	s, ok := Shade(uint8(c))
	return s, ok
}

func toInt(c Color) (int, float64, Plain) { return int(c), float64(c), Plain(c) }

func fromPlain(p Plain) (Color, bool) {
	c, ok := Color(p)
	return c, ok
}
