package main

// An import path with a space, which the module has as a directory (E0005).

import ab "example.com/proj/a b"

func main() { ab.F() }
