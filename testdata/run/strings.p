package main

const greeting = "hello, " + "world"

func count(s string, b byte) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			n++
		}
	}
	return n
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func main() {
	println(greeting, len(greeting))
	println("count l", count(greeting, 'l'))
	println("prefix", hasPrefix(greeting, "hello"), hasPrefix(greeting, "world"))
	s := "héllo, 世界"
	n, sum := 0, 0
	for i, r := range s {
		n++
		sum += int(r) + i
	}
	println("runes", n, sum, len(s))
	println("compare", "abc" < "abd", "abc" == "abc", "b" > "abc", "" < "a")
	sub := greeting[7:]
	println("sub", sub, len(sub), sub[0])
	var empty string
	println("empty", len(empty), empty == "")
	x := "abc"
	y := "ab" + "c"
	println("eq", x == y, x != "abd")
	words := []string{"pear", "apple", "fig"}
	longest := ""
	for _, w := range words {
		if len(w) > len(longest) {
			longest = w
		}
	}
	println("longest", longest)
}
