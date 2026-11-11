package main

type Bytes []byte
type MyByte byte
type Name string
type MyRune rune
type Fd int32
type Runes []rune
type MyRunes []MyRune

func use(...any) {}

func g[T ~string | ~int](i int) T { return T(i) }
