package main

func f() {
	var m map[string]struct{ i int }
	m["foo"].i = 42
}

func main() {}
