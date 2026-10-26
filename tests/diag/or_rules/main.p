package main

import (
	"pluto/os"
	"pluto/strconv"
)

func f() (int, error) { return 0, nil }

func g() int { return 0 }

func uses(s string) error {
	c := int64(g()) or return             // a conversion is not a call of a function (E2461)
	l := len(s) or return                 // len is a built-in function (E2461)
	d := strconv.Itoa(5) or return        // Itoa returns no error (E2462)
	e := f() or println(err)              // the clause can complete (E2463)
	x := f() or os.Exit(1)                // a call that never returns does not leave (E2463)
	n, err := strconv.Atoi(s) or return   // Atoi has one value besides the error (E0017)
	for i := 0; i < 3; i = f() or break { // not a post statement (E2465)
	}
	_, _, _, _, _, _, _ = c, l, d, e, x, n, err
	return nil
}

func count() int {
	uses("") or return // count has no error result to carry err (E2464)
	return 0
}

func main() {
	f() or return // main has no error result to carry err (E2464)
	_ = count()
}
