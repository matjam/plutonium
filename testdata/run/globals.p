package main

var (
	a     = b + 1
	b     = f()
	c     int
	d     = "global string"
	e     = [3]int{1, 2, 3}
	pi    = 3.25
	table [256]uint32
)

func f() int { return 41 }

func init() {
	c = a * 2
	for i := range table {
		v := uint32(i)
		for k := 0; k < 8; k++ {
			if v&1 != 0 {
				v = 0xEDB88320 ^ (v >> 1)
			} else {
				v >>= 1
			}
		}
		table[i] = v
	}
}

func init() {
	c++
}

func crc(s string) uint32 {
	crc := ^uint32(0)
	for i := 0; i < len(s); i++ {
		crc = table[byte(crc)^s[i]] ^ (crc >> 8)
	}
	return ^crc
}

func main() {
	println("globals", a, b, c, d, e[2], int(pi*100))
	println("crc", crc("hello world"), table[255])
	a = 0
	println("assigned", a)
}
