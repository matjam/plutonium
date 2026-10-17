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

// An unlabeled continue does not leave the loop.
func skip(names []string) {
	for _, n := range names {
		if n == "" {
			defer println("empty")
			continue
		}
	}
}

// A break that ends only the inner loop leaves the outer one running.
func inner(names []string) {
	for range names {
		for _, n := range names {
			defer println(n)
			break
		}
	}
}

// These leave every loop on every path after the defer, and are accepted.
func leaves(names []string, id int) string {
outer:
	for i, n := range names {
		for j := range n {
			if i == id {
				defer println(n)
				return n
			}
			if j == id {
				defer println(j)
				break outer
			}
		}
	}
	return ""
}

func main() {
	closeAll(nil)
	retry(2)
	skip(nil)
	inner(nil)
	leaves(nil, 0)
}
