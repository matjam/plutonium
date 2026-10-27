package main

func main() {
	var ps [9]*int
	i := 0
outer:
	j := 0
inner:
	v := i*3 + j
	ps[v] = &v
	j++
	if j < 3 {
		goto inner
	}
	i++
	if i < 3 {
		goto outer
	}
	println(*ps[0])
}
