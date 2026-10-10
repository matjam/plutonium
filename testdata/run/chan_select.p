// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adapted from Go's test/chan/select.go, select4.go, select8.go and
// sendstmt.go, with a line of output for each.

// Test simple select.

package main

var counter uint
var shift uint

func GetValue() uint {
	counter++
	return 1 << shift
}

func Send(a, b chan uint) int {
	var i int

LOOP:
	for {
		select {
		case a <- GetValue():
			i++
			a = nil
		case b <- GetValue():
			i++
			b = nil
		default:
			break LOOP
		}
		shift++
	}
	return i
}

func selectSend() {
	a := make(chan uint, 1)
	b := make(chan uint, 1)
	if v := Send(a, b); v != 2 {
		println("Send returned", v, "!= 2")
		panic("fail")
	}
	if av, bv := <-a, <-b; av|bv != 3 {
		println("bad values", av, bv)
		panic("fail")
	}
	if v := Send(a, nil); v != 1 {
		println("Send returned", v, "!= 1")
		panic("fail")
	}
	if counter != 10 {
		println("counter is", counter, "!= 10")
		panic("fail")
	}
	println("select ok")
}

// select4: a select statement proceeds when a value is ready, and does
// not evaluate the left-hand side of a case that does not run.

var fTarget int

func f() *int {
	println("BUG: called f")
	return &fTarget
}

func selectReady() {
	var x struct {
		a int
	}
	c := make(chan int, 1)
	c1 := make(chan int)
	c <- 42
	select {
	case *f() = <-c1:
		// nothing
	case x.a = <-c:
		if x.a != 42 {
			println("BUG:", x.a)
		}
	}
	println("select4 ok", x.a)
}

// select8: break statements in a select.

var ch = make(chan int)

func forever(ch chan int) {
	for {
		ch <- 5
	}
}

func selectBreak() {
	go forever(ch)

	select {
	case <-ch:
		break
		panic("unreachable")
	}

	select {
	default:
		break
		panic("unreachable")
	}

	select {
	case <-ch:
		break
		panic("unreachable")
	default:
		break
		panic("unreachable")
	}

	select {
	case <-ch:
		break
		panic("unreachable")
	case ch <- 10:
		panic("unreachable")
	default:
		break
		panic("unreachable")
	}
	println("select8 ok")
}

// sendstmt: parsing cases that differ now that send is a statement.

func chanchan() {
	cc := make(chan chan int, 1)
	c := make(chan int, 1)
	cc <- c
	select {
	case <-cc <- 2:
	default:
		panic("nonblock")
	}
	if <-c != 2 {
		panic("bad receive")
	}
}

func sendprec() {
	c := make(chan bool, 1)
	c <- false || true // not a syntax error: same as c <- (false || true)
	if !<-c {
		panic("sent false")
	}
}

func main() {
	selectSend()
	selectReady()
	selectBreak()
	chanchan()
	sendprec()
	println("sendstmt ok")
}
