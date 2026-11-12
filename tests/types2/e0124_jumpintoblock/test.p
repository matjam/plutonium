package main

func f(x int) {
	goto L
	if x > 0 {
	L:
		print("inside block")
	}
}

func main() {}
