package main

func checkReturn(i int) {
	if i {
		panic("non-zero return")
	}
}

func main() {}
