package main

func closeAll(names []string) {
	for _, n := range names {
		defer println(n)
	}
}

// A goto back to a label before the defer makes a loop too.
func retry(n int) {
again:
	defer println(n)
	n--
	if n > 0 {
		goto again
	}
}

func main() {
	closeAll(nil)
	retry(2)
}
