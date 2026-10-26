package main

type Errno int
func (e Errno) Error() string { return "errno" }
func f() error { return Errno(2) }
func main() { _ = f() }
