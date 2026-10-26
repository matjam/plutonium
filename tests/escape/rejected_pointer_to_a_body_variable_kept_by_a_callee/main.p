package main

func put(buf []*int, i int, p *int) { buf[i] = p }
func main() {
	var arr [4]*int
	buf := arr[:]
	for i := 0; i < 3; i++ {
		v := i
		put(buf, i, &v)
	}
	println(len(arr))
}
