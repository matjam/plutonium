package main

import stdsync "pluto/sync"

func main() {
	var mu stdsync.Mutex
	task func() { mu.Lock() }()
}
