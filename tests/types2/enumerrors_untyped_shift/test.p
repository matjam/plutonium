package main

type Color enum { Red; Green; Blue }

type Order enum { Less = -1; Equal; Greater }

func f(n uint) Color { return 1 << n }