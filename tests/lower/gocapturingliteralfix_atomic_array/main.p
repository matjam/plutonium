package main

import "pluto/sync/atomic"

func main() {
	var a [2]atomic.Int32
	task func() { a[0].Add(1) }()
}
