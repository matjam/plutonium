package main

type Color enum { Red; Green; Blue }

type Order enum { Less = -1; Equal; Greater }

type R packed uint8 { c bits[Color, 1] }