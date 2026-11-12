package main

type L owner struct{ x int }
type M owner struct{ ls [2]owned[L] }

func main() {
	m := M{ls: [2]owned[L]{new(L), new(L)}}
	pm := &m
	q := &*m.ls[1]
	pm.ls[1] = new(L)
	println(q.x)
}
