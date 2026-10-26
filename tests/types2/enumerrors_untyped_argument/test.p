package main

type Color enum { Red; Green; Blue }

type Order enum { Less = -1; Equal; Greater }

func g(Color) {}
func f() { g(2) }