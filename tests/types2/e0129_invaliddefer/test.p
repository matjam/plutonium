package main

func f(i int) int {
	defer int32(i)
	return i
}

func main() {}
