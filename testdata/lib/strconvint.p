package main

// ParseInt and ParseUint with base 0: underscores are accepted only after a
// base prefix or between digits (Go's underscoreOK). The expected output is
// what Go's strconv prints for the same program with the imports changed.

import (
	"pluto/fmt"
	"pluto/strconv"
)

func main() {
	for _, s := range []string{
		"0_123", "1_234", "1_234_567", "0x_1234", "0X_CAFE_f00d", "0o_1234", "0b_0010_1101", "-1_000", "+0x_ff",
		"1__2", "_1", "1_", "0x_", "0_", "0o_", "0b1_", "0x1__2", "-_1", "1_000_", "12_a", "0x1_g",
		"0", "7", "0b2", "0xg", "08", "9223372036854775807", "9223372036854775808", "1_9223372036854775807",
	} {
		v, err := strconv.ParseInt(s, 0, 64)
		u, uerr := strconv.ParseUint(s, 0, 64)
		fmt.Println(s, v, err != nil, u, uerr != nil)
	}
	// Underscores mean nothing in an explicit base.
	for _, s := range []string{"1_2", "ff_ff", "_1"} {
		v, err := strconv.ParseInt(s, 10, 64)
		w, werr := strconv.ParseInt(s, 16, 64)
		fmt.Println(s, v, err != nil, w, werr != nil)
	}
}
