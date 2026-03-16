package main

import "pluto/fmt"

// Demonstrates break, continue, switch, and defer.
//
// Build: pluto build -o control ./examples/16-control-flow/
// Run:   ./control
// Expected output:
//   break at 5
//   even sum=20
//   three
//   hello world

func testBreak() {
	// Break should exit the loop
	i := int64(0)
	for i < 100 {
		if i == 5 {
			break
		}
		i = i + 1
	}
	fmt.Printf("break at %d\n", i)
}

func testContinue() {
	// Continue should skip odd numbers, summing evens 0+2+4+6+8=20
	sum := int64(0)
	i := int64(0)
	for i < 10 {
		if i % 2 != 0 {
			i = i + 1
			continue
		}
		sum = sum + i
		i = i + 1
	}
	fmt.Printf("even sum=%d\n", sum)
}

func testSwitch() {
	x := int64(3)
	switch x {
	case 1:
		fmt.Printf("one\n")
	case 2:
		fmt.Printf("two\n")
	case 3:
		fmt.Printf("three\n")
	default:
		fmt.Printf("other\n")
	}
}

func testDefer() {
	defer fmt.Printf("world\n")
	fmt.Printf("hello ")
}

func main() int64 {
	testBreak()
	testContinue()
	testSwitch()
	testDefer()
	return 0
}
