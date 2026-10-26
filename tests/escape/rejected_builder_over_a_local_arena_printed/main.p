package main

import (
	"pluto/arena"
	"pluto/fmt"
	"pluto/strings"
)
func f() {
	ar := arena.NewFixed(256)
	var b strings.Builder
	b.Init(&ar)
	b.WriteString("x")
	fmt.Println(b.String())
}
func main() { f() }
