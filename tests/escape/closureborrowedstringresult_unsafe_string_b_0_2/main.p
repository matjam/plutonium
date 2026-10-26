package main
import "unsafe"
func borrow(b []byte) string { return unsafe.String(&b[0], len(b)) }
func inner() func() string {
 b := [2]byte{'o', 'k'}
 return func() string { return unsafe.String(&b[0], 2) }
}

func outer() func() string {
 return func() string { f := inner(); return f() }
}
func main() { println(outer()()) }
