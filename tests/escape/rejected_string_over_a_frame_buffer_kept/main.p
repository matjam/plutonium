package main

import "unsafe"
var name string
func main() {
	var buf [4]byte
	name = unsafe.String(&buf[0], 4)
}
