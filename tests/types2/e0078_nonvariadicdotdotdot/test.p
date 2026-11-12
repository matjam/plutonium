package main

func printArgs(s []string) {
	for _, a := range s {
		println(a)
	}
}

func f() {
	s := []string{"a", "b", "c"}
	printArgs(s...)
}

func main() {}
