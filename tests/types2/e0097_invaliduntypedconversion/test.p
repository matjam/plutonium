package main

func f[T ~int8 | ~int16 | ~int32 | ~int64](x T) T {
	return x + 1024
}

func main() {}
