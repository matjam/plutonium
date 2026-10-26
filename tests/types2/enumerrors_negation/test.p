package main

type Color enum { Red; Green; Blue }

type Order enum { Less = -1; Equal; Greater }

func f(c Order) Order { return -c }