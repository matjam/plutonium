package main

func main() {
	var s struct{ a int }
	s.a := 1
	_ = s
}
