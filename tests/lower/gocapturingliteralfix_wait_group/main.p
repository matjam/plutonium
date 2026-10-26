package main

import "pluto/sync"

func main() {
	var wg sync.WaitGroup
	task func() { defer wg.Done() }()
}
