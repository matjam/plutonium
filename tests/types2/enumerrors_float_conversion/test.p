package main

type Color enum { Red; Green; Blue }

type Order enum { Less = -1; Equal; Greater }

func f(x float64) { c, ok := Color(x); _, _ = c, ok }