// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adapted from Go's test/chan/doubleselect.go for the common subset of Go
// and Plutonium: no flag or runtime packages, a slice instead of a map,
// and the closing puroutine takes its channels as arguments.

// Test the situation in which two cases of a select can
// both end up running. See http://codereview.appspot.com/180068.

package main

const iterations = 100000

// sender sends a counter to one of four different channels. If two
// cases both end up running in the same iteration, the same value will be sent
// to two different channels.
func sender(n int, c1, c2, c3, c4 chan<- int) {
	defer close(c1)
	defer close(c2)
	defer close(c3)
	defer close(c4)

	for i := 0; i < n; i++ {
		select {
		case c1 <- i:
		case c2 <- i:
		case c3 <- i:
		case c4 <- i:
		}
	}
}

// mux receives the values from sender and forwards them onto another channel.
// It would be simpler to just have sender's four cases all be the same
// channel, but this doesn't actually trigger the bug.
func mux(out chan<- int, in <-chan int, done chan<- bool) {
	for v := range in {
		out <- v
	}
	done <- true
}

// recver gets a steam of values from the four mux's and checks for duplicates.
func recver(in <-chan int) int {
	seen := make([]bool, iterations)
	n := 0
	for v := range in {
		if seen[v] {
			println("got duplicate value: ", v)
			panic("fail")
		}
		seen[v] = true
		n++
	}
	return n
}

func closer(done <-chan bool, cmux chan int) {
	<-done
	<-done
	<-done
	<-done
	close(cmux)
}

func main() {
	c1 := make(chan int)
	c2 := make(chan int)
	c3 := make(chan int)
	c4 := make(chan int)
	done := make(chan bool)
	cmux := make(chan int)
	go sender(iterations, c1, c2, c3, c4)
	go mux(cmux, c1, done)
	go mux(cmux, c2, done)
	go mux(cmux, c3, done)
	go mux(cmux, c4, done)
	go closer(done, cmux)
	// We keep the recver because it might catch more bugs in the future.
	// However, the result of the bug linked to at the top is that we'll
	// end up panicking with: "throw: bad g->status in ready".
	println("received", recver(cmux))
}
