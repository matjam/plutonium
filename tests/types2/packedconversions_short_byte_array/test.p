package main

type Hdr packed be [4]byte {
	a uint8
	b uint8
	c uint16
}

type Wide packed uint32 {
	a uint16
	b uint16
}

func f(h Hdr) [3]byte { return [3]byte(h) }
