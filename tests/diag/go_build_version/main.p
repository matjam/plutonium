//go:build go1.21

package main

func main() {
	for i := range 3 {
		println(i)
	}
}
