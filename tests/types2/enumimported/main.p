package main

import "m/gfx"

type C = gfx.Color

func f(c gfx.Color) bool {
	switch c {
	case Red, gfx.Color.Green, C.Blue:
	default:
	}
	return c == gfx.Color.Red
}
