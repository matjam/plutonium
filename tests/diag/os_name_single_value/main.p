package main

import "pluto/os"

// A name the operating system gives the program may not be valid UTF-8, so
// the functions with the Go name return an error as well (spec 7.3, F.9).
// Each single-value use below is an error.

func main() {
	ents, err := os.ReadDir(".")
	if err != nil {
		return
	}
	e := ents.At(0)
	name := e.Name()
	println(name)

	home := os.Getenv("HOME")
	println(home)

	println("dir " + e.Name())

	v, ok := os.LookupEnv("HOME")
	println(v, ok)
}
