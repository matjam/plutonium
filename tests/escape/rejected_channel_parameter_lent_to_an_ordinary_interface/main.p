package main

var kept any
func f(c chan int) { kept = c }
func main() { f(nil) }
