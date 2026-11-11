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

func f(h Hdr, w Wide) ([4]byte, Hdr, [4]byte, Wide, uint32, Wide) {
	return [4]byte(h), Hdr([4]byte{1, 2, 3, 4}), [4]byte(w), Wide([4]byte{}), uint32(w), Wide(uint32(7))
}
