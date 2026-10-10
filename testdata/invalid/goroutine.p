package main

// This program must be rejected: goroutines are forbidden in Plutonium.
func foo() {
	go bar()
}

func bar() {}
