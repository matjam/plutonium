package main

import "pluto/sync"

func main() {
	n := 1
	var l sync.Locker
	task func() { _ = n; _ = l }()
}
