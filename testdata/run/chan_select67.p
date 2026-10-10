// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adapted from Go's test/chan/select6.go, select7.go and zerosize.go for
// the common subset of Go and Plutonium: the puroutines take their
// channels as arguments instead of capturing them, and select7 does
// without runtime.Gosched.

package main

// select6: Issue 2075.
// A bug in select corrupts channel queues of failed cases
// if there are multiple waiters on those channels and the
// select is the last in the queue. If further waits are made
// on the channel without draining it first then those waiters
// will never wake up. In the code below c1 is such a channel.

func recvOnce(c1 chan bool) { <-c1 }

func selector(c1, c2, c3 chan bool) {
	select {
	case <-c1:
		panic("dummy")
	case <-c2:
		c3 <- true
	}
	<-c1
}

func sendOnce(c2 chan bool) { c2 <- true }

func select6() {
	c1 := make(chan bool)
	c2 := make(chan bool)
	c3 := make(chan bool)
	go recvOnce(c1)
	go selector(c1, c2, c3)
	go sendOnce(c2)
	<-c3
	c1 <- true
	c1 <- true
	println("select6 ok")
}

// select7: select when discarding a value.

func recv1(c <-chan int) {
	<-c
}

func recv2(c <-chan int) {
	select {
	case <-c:
	}
}

func recv3(c <-chan int) {
	c2 := make(chan int)
	select {
	case <-c:
	case <-c2:
	}
}

func send1(recv func(<-chan int)) {
	c := make(chan int)
	go recv(c)
	c <- 1
}

func send2(recv func(<-chan int)) {
	c := make(chan int)
	go recv(c)
	select {
	case c <- 1:
	}
}

func send3(recv func(<-chan int)) {
	c := make(chan int)
	go recv(c)
	c2 := make(chan int)
	select {
	case c <- 1:
	case c2 <- 1:
	}
}

func select7() {
	send1(recv1)
	send2(recv1)
	send3(recv1)
	send1(recv2)
	send2(recv2)
	send3(recv2)
	send1(recv3)
	send2(recv3)
	send3(recv3)
	println("select7 ok")
}

// zerosize: channels of a zero-sized type.

func zerosize() {
	_ = make(chan [0]byte)
	_ = make(chan [0]byte, 1)
	_ = make(chan struct{})
	c := make(chan struct{}, 1)
	c <- struct{}{}
	<-c
	println("zerosize ok", len(c), cap(c))
}

func main() {
	select6()
	select7()
	zerosize()
}
