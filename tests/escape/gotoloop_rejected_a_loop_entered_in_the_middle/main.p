package main

//pu:noinline
func f(c bool) {
	var ps [4]*int
	n := 0
	if c {
		goto mid
	}
top:
	{
		v := n
		ps[n] = &v
	}
mid:
	n++
	if n < 3 {
		goto top
	}
	println(ps[1] != nil)
}

func main() {
	f(false)
	f(true)
}
