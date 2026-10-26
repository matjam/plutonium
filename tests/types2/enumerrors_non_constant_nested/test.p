package main

type Color enum { Red; Green; Blue }

type Order enum { Less = -1; Equal; Greater }

func f(n int) (Color, bool) {
	c, ok := Color(int(Color(n)))
	return c, ok
}