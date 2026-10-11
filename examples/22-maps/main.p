package main

import "pluto/fmt"

// Maps are counted references.
//
// A map value is one word that points at a hash table. Copying a map, passing
// it to a function or storing it in a struct gives the other side the same
// table, so a function that stores into its argument changes the caller's
// map, as in Go (language spec section 7.10). What differs is the end of its
// life: there is no garbage collector, so the table counts the copies that
// refer to it and is freed, with its keys and elements, when the last copy
// goes away. A program never calls a free function and never leaks a map.

// count adds one to a word's tally for every occurrence in words.
func count(words []string, into map[string]int) {
	for _, w := range words {
		into[w]++ // a missing key reads as the zero value, so this just works
	}
}

func main() {
	words := [5]string{"to", "be", "or", "not", "to"}
	freq := map[string]int{}
	count(words[:], freq) // freq is changed by count

	alias := freq // the same table, not a copy of its contents
	alias["be"] = 10
	fmt.Println(freq, len(freq)) // fmt prints keys in sorted order

	// The comma-ok form tells a missing key from a stored zero.
	if n, ok := freq["to"]; ok {
		fmt.Println("to appears", n, "times")
	}
	if n, ok := freq["maybe"]; !ok {
		fmt.Println("maybe is missing, n =", n)
	}

	delete(freq, "or")
	fmt.Println(alias) // alias sees the delete

	// Iteration order is unspecified, so add up instead of printing in turn.
	total := 0
	for _, n := range freq {
		total += n
	}
	fmt.Println("total", total)

	// A nil map reads as empty. Storing into one panics.
	var none map[string]int
	fmt.Println(len(none), none["x"])

	clear(freq)
	fmt.Println(len(freq), len(alias)) // both names refer to one empty table
}
