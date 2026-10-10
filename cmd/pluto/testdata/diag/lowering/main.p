package main

func closeAll(names []string) {
	for _, n := range names {
		defer println(n)
	}
}

func main() {
	closeAll(nil)
}
