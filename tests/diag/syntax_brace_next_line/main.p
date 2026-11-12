package main

// The { of an if block on the next line (E1006).

func main() {
	ok := true
	if ok
	{
		println(ok)
	}
}
