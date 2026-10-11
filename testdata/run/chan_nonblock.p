// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adapted from Go's test/chan/nonblock.go for the common subset of Go and
// Plutonium: sleep is a short busy wait, during which other processors
// steal the waiting task (and on one processor, preemption lets it
// run), instead of a time.Tick and runtime.Gosched; and the test prints a
// line when it is done.

// Test channel operations that test for blocking.
// Use several sizes and types of operands.

package main

func i32receiver(c chan int32, strobe chan bool) {
	if <-c != 123 {
		panic("i32 value")
	}
	strobe <- true
}

func i32sender(c chan int32, strobe chan bool) {
	c <- 234
	strobe <- true
}

func i64receiver(c chan int64, strobe chan bool) {
	if <-c != 123456 {
		panic("i64 value")
	}
	strobe <- true
}

func i64sender(c chan int64, strobe chan bool) {
	c <- 234567
	strobe <- true
}

func breceiver(c chan bool, strobe chan bool) {
	if !<-c {
		panic("b value")
	}
	strobe <- true
}

func bsender(c chan bool, strobe chan bool) {
	c <- true
	strobe <- true
}

func sreceiver(c chan string, strobe chan bool) {
	if <-c != "hello" {
		panic("s value")
	}
	strobe <- true
}

func ssender(c chan string, strobe chan bool) {
	c <- "hello again"
	strobe <- true
}

var sink int

func sleep() {
	x := 0
	for i := 0; i < 100000; i++ {
		x += i ^ sink
	}
	sink = x
}

const maxTries = 10000 // Up to 100ms per test.

func main() {
	var i32 int32
	var i64 int64
	var b bool
	var s string

	var sync = make(chan bool)

	for buffer := 0; buffer < 2; buffer++ {
		c32 := make(chan int32, buffer)
		c64 := make(chan int64, buffer)
		cb := make(chan bool, buffer)
		cs := make(chan string, buffer)

		select {
		case i32 = <-c32:
			panic("blocked i32sender")
		default:
		}

		select {
		case i64 = <-c64:
			panic("blocked i64sender")
		default:
		}

		select {
		case b = <-cb:
			panic("blocked bsender")
		default:
		}

		select {
		case s = <-cs:
			panic("blocked ssender")
		default:
		}

		task i32receiver(c32, sync)
		try := 0
	Send32:
		for {
			select {
			case c32 <- 123:
				break Send32
			default:
				try++
				if try > maxTries {
					println("i32receiver buffer=", buffer)
					panic("fail")
				}
				sleep()
			}
		}
		<-sync

		task i32sender(c32, sync)
		if buffer > 0 {
			<-sync
		}
		try = 0
	Recv32:
		for {
			select {
			case i32 = <-c32:
				break Recv32
			default:
				try++
				if try > maxTries {
					println("i32sender buffer=", buffer)
					panic("fail")
				}
				sleep()
			}
		}
		if i32 != 234 {
			panic("i32sender value")
		}
		if buffer == 0 {
			<-sync
		}

		task i64receiver(c64, sync)
		try = 0
	Send64:
		for {
			select {
			case c64 <- 123456:
				break Send64
			default:
				try++
				if try > maxTries {
					panic("i64receiver")
				}
				sleep()
			}
		}
		<-sync

		task i64sender(c64, sync)
		if buffer > 0 {
			<-sync
		}
		try = 0
	Recv64:
		for {
			select {
			case i64 = <-c64:
				break Recv64
			default:
				try++
				if try > maxTries {
					panic("i64sender")
				}
				sleep()
			}
		}
		if i64 != 234567 {
			panic("i64sender value")
		}
		if buffer == 0 {
			<-sync
		}

		task breceiver(cb, sync)
		try = 0
	SendBool:
		for {
			select {
			case cb <- true:
				break SendBool
			default:
				try++
				if try > maxTries {
					panic("breceiver")
				}
				sleep()
			}
		}
		<-sync

		task bsender(cb, sync)
		if buffer > 0 {
			<-sync
		}
		try = 0
	RecvBool:
		for {
			select {
			case b = <-cb:
				break RecvBool
			default:
				try++
				if try > maxTries {
					panic("bsender")
				}
				sleep()
			}
		}
		if !b {
			panic("bsender value")
		}
		if buffer == 0 {
			<-sync
		}

		task sreceiver(cs, sync)
		try = 0
	SendString:
		for {
			select {
			case cs <- "hello":
				break SendString
			default:
				try++
				if try > maxTries {
					panic("sreceiver")
				}
				sleep()
			}
		}
		<-sync

		task ssender(cs, sync)
		if buffer > 0 {
			<-sync
		}
		try = 0
	RecvString:
		for {
			select {
			case s = <-cs:
				break RecvString
			default:
				try++
				if try > maxTries {
					panic("ssender")
				}
				sleep()
			}
		}
		if s != "hello again" {
			panic("ssender value")
		}
		if buffer == 0 {
			<-sync
		}
	}
	println("nonblock ok")
}
