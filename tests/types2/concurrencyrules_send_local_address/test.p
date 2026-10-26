package main
func g(c chan *int) { x := 1; c <- &x }