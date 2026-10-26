package main

func main() {
	for p, j := (*int)(nil), 0; j < 2; j++ {
		for i := 0; i < 2; i++ {
			p = &i
		}
		_ = p
	}
}
