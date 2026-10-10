package main

import (
	"pluto/fmt"
	"pluto/math"
)

// The integer limits match Go's math package; the output was checked
// against Go running this program with the import pointed at "math".
func main() {
	fmt.Println(math.MaxInt8, math.MinInt8, math.MaxInt16, math.MinInt16)
	fmt.Println(math.MaxInt32, math.MinInt32, math.MaxInt64, math.MinInt64)
	fmt.Println(math.MaxInt, math.MinInt)
	fmt.Println(math.MaxUint8, math.MaxUint16, math.MaxUint32)
	var u uint64 = math.MaxUint64
	var w uint = math.MaxUint
	fmt.Println(u, w)
	var i32 int32 = math.MaxInt32
	i32++
	fmt.Println(i32 == math.MinInt32)
	fmt.Println(math.SmallestNonzeroFloat32 > 0, float32(math.SmallestNonzeroFloat32)/2 == 0)
}
