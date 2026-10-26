package main

type Color enum { Red; Green; Blue }

type Order enum { Less = -1; Equal; Greater }

func f(c Color) int {
	switch c {
	case Red:
		return 1
	case Green, Blue:
		break
	}
}