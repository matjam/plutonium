package main

import (
	"pluto/fmt"
	"pluto/strings"
)

func isDigit(r rune) bool { return r >= '0' && r <= '9' }
func isJ(r rune) bool     { return r >= 0x3000 }

func main() {
	ss := []string{"", "123abc456", "abc", "日本123語", "99"}
	for _, s := range ss {
		a, f := strings.CutPrefix(s, "12")
		b, g := strings.CutSuffix(s, "56")
		fmt.Printf("%d %d %t %d %d [%s] [%s] [%s] [%s|%t] [%s|%t] [%s] %t\n", strings.IndexFunc(s, isDigit), strings.LastIndexFunc(s, isDigit),
			strings.ContainsFunc(s, isJ), strings.IndexFunc(s, isJ), strings.LastIndexFunc(s, isJ),
			strings.TrimFunc(s, isDigit), strings.TrimLeftFunc(s, isDigit), strings.TrimRightFunc(s, isJ), a, f, b, g,
			strings.Trim(s, "1日9"), strings.ContainsRune(s, '本'))
	}
	long := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab"
	fmt.Printf("%d %d %d %d\n", strings.Index(long, "aab"), strings.Index(long, "aaab"), strings.Index(long, "aac"), strings.LastIndex(long, "aa"))
	fmt.Printf("%d %d\n", strings.Count("cheese", "e"), strings.Count("five", ""))
	fmt.Printf("%d\n", strings.Count("aaaaaa", "aa"))
	fmt.Printf("%d %d\n", strings.IndexAny("日本語x", "x語"), strings.LastIndexAny("日本語x本", "本"))
}
