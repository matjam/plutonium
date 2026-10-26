package main

type Bytes []byte
type MyByte byte
type Name string
type MyRune rune
type Fd int32
type Runes []rune
type MyRunes []MyRune

func use(...any) {}

func f(a *[4]byte) string { return string(a[:]) }