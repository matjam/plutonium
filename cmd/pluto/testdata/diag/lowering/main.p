package main

func greet(name string) string {
	return "hello, " + name
}

func closeAll(names []string) {
	for _, n := range names {
		defer println(n)
	}
}

func main() {
	println(greet("x"))
	closeAll(nil)
}
