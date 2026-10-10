package main

import "pluto/strings"

type Counter struct {
	total int
}

func (c *Counter) Add(n int) { c.total += n }

type Adder interface {
	Add(n int)
}

func pair() (int, error) {
	return 1
}

func main() {
	var small uint8 = 300
	var c Counter
	c.Totl = 1
	var a Adder = c
	ok := true
	n := ok + 1
	fmt.Println(small)
	b := strings.Builder{}
	_ = strings.HasPrefx("ab", "a")
	_, _, _, _ = a, n, b, pair
}
