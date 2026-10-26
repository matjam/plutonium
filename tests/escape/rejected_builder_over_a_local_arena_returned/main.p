package main

import (
	"pluto/arena"
	"pluto/strings"
)
func f() string {
	ar := arena.NewFixed(256)
	var b strings.Builder
	b.Init(&ar)
	b.WriteString("x")
	return b.String()
}
func main() { _ = f() }
