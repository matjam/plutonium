package main
func inner(name string) func() string {
 return func() string {
  unused := func() int { return len(name) }
  _ = unused
  result := name; return result
 }
}

func outer(name string) func() string {
 return func() string { f := inner(name); return f() }
}
func main() { println(outer("world")()) }
