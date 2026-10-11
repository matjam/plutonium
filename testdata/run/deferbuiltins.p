package main

func doDelete(m map[string]int) {
	defer delete(m, "a")
	defer delete(m, "c")
}

func doClearMap(m map[string]int) {
	defer clear(m)
}

func doClearInts(s []int) {
	defer clear(s)
}

func doClearStrings(s []string) {
	defer clear(s)
}

func doCopyInts(dst, src []int) {
	defer copy(dst, src)
}

func doCopyStrings(dst, src []string) {
	defer copy(dst, src)
}

func doCopyBytes(dst []byte) {
	defer copy(dst, "hi!")
}

func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	doDelete(m)
	println("delete", len(m), m["a"], m["b"], m["c"])

	m2 := map[string]int{"x": 1, "y": 2}
	doClearMap(m2)
	println("clearmap", len(m2))

	var ia [3]int
	ia = [3]int{1, 2, 3}
	doClearInts(ia[:])
	println("clearints", ia[0], ia[1], ia[2], len(ia[:]))

	var ss [2]string
	ss = [2]string{"p", "q"}
	doClearStrings(ss[:])
	println("clearstr", len(ss[0]), len(ss[1]), len(ss[:]))

	var da [4]int
	var sa [3]int
	sa = [3]int{9, 8, 7}
	doCopyInts(da[:], sa[:])
	println("copyints", da[0], da[1], da[2], da[3])

	var dstr [3]string
	var sstr [2]string
	sstr = [2]string{"x", "y"}
	doCopyStrings(dstr[:], sstr[:])
	println("copystr", dstr[0], dstr[1], dstr[2])

	var ba [4]byte
	doCopyBytes(ba[:])
	println("copybytes", int(ba[0]), int(ba[1]), int(ba[2]))
}
