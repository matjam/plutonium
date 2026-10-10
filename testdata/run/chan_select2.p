// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adapted from Go's test/chan/select2.go for the common subset of Go and
// Plutonium. The original checks with runtime.ReadMemStats that selects do
// not consume memory; Plutonium's select allocates nothing (its cases live
// in the selecting frame), so this keeps the loops and counts the
// receives.

package main

func sender(c chan int, n int) {
	for i := 0; i < n; i++ {
		c <- 1
	}
}

func receiver(c, dummy chan int, n int) int {
	got := 0
	for i := 0; i < n; i++ {
		select {
		case v := <-c:
			got += v
		case <-dummy:
			panic("dummy")
		}
	}
	return got
}

func main() {
	c := make(chan int)
	dummy := make(chan int)

	// warm up
	go sender(c, 100000)
	a := receiver(c, dummy, 100000)

	// second time
	go sender(c, 100000)
	b := receiver(c, dummy, 100000)
	println("received", a, b)
}
