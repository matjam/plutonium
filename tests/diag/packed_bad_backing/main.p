package main

// A packed type whose backing type is not an integer or a byte array
// (E2101).

type Flags packed float32 {
	a bits[uint8, 4]
}

func main() {}
