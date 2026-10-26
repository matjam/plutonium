package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/strings"
)
func f() string {
	var b strings.Builder
	b.Init(alloc.Default())
	defer b.Free()
	b.WriteString("x")
	fmt.Println(b.String())
	return b.String()
}
func main() { _ = f() }
