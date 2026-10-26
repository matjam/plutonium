package main

import (
	"pluto/strings"
	"unsafe"
)
func f() string {
	b := [2]byte{'o', 'k'}
	return strings.Slice(unsafe.String(&b[0], 2), 0, 1)
}
func main() { println(f()) }
