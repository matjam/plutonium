package main

import (
	"pluto/arena"
	"pluto/fmt"
	"pluto/strings"
)
type pair struct{ a, b string }
func f() {
	ar := arena.NewFixed(256)
	var b strings.Builder
	b.Init(&ar)
	b.WriteString("x")
	fmt.Println(pair{b.String(), "y"})
}
func main() { f() }
