package main

type Color enum { Red; Green; Blue }

type Shade enum { Light; Dark; Blue }

func constant() Shade { return Shade(Color.Green) }

func checked(c Color) (Shade, bool) {
	s, ok := Shade(c)
	return s, ok
}

func unchecked(c Color) Shade { return Shade(c) }
