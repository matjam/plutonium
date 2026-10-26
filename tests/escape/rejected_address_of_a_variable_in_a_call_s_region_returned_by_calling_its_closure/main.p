package main

var saved *int
func cell(n int) func() *int { return func() *int { return &n } }
func h() { saved = cell(3)() }
func main() { h() }
