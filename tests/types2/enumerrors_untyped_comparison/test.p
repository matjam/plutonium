package main

type Color enum { Red; Green; Blue }

type Order enum { Less = -1; Equal; Greater }

func f(c Color) bool { return c == 1 }