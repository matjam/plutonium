package main

var kept []int
func seven() int { return 7 }
func keep(s []int = []int{seven(), seven()}) { kept = s }
func f() { keep() }
func main() { f() }
