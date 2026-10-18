package main

//pu:extern strlen
func strlen(s *byte) uint64 { return 0 }

//pu:extern puts
func puts(s string) int32

//pu:export plu_add
func add(a, b int32) int32 { return a + b }

//pu:export plu_add
func add2(a, b int32) int32 { return a - b }

//pu:export plu_sum
func sum[T int | float64](a, b T) T { return a + b }

func main() {
	println(add(1, 2))
}
