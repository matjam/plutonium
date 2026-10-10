package strings

import (
	"pluto/testing"
	"pluto/unicode/utf8"
)

// Benchmarks of the byte-level functions on text with multi-byte runes,
// whose slices would be checked for rune boundaries if the functions did
// not slice with rt.SliceBytewise or the compiler did not prove the
// bounds.

const benchText = "Größenwahn und Übermut: «la vie en rose», 東京タワー, naïve café, señor; " +
	"the quick brown fox jumps over the lazy dog — ünïcödé everywhere ✓ end"

func BenchmarkIndexUnicode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if Index(benchText, "lazy") < 0 {
			b.Fatal("not found")
		}
	}
}

func BenchmarkLastIndexUnicode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if LastIndex(benchText, "Größ") != 0 {
			b.Fatal("wrong index")
		}
	}
}

func BenchmarkIndexRuneUnicode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if IndexRune(benchText, '✓') < 0 {
			b.Fatal("not found")
		}
	}
}

func BenchmarkIndexFuncUnicode(b *testing.B) {
	f := func(r rune) bool { return r == '✓' }
	for i := 0; i < b.N; i++ {
		if IndexFunc(benchText, f) < 0 {
			b.Fatal("not found")
		}
	}
}

func BenchmarkLastIndexAnyUnicode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if LastIndexAny(benchText, "ßÜ") < 0 {
			b.Fatal("not found")
		}
	}
}

func BenchmarkCountUnicode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if Count(benchText, "e ") == 0 {
			b.Fatal("none")
		}
	}
}

func BenchmarkEqualFoldUnicode(b *testing.B) {
	t := ToUpper(benchText)
	for i := 0; i < b.N; i++ {
		if !EqualFold(benchText, t) {
			b.Fatal("not equal")
		}
	}
}

func BenchmarkHasSuffixUnicode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if HasSuffix(benchText, "café") {
			b.Fatal("suffix")
		}
	}
}

func BenchmarkTrimSpaceUnicode(b *testing.B) {
	s := "  \t " + benchText + " \n "
	for i := 0; i < b.N; i++ {
		if len(TrimSpace(s)) != len(benchText) {
			b.Fatal("wrong trim")
		}
	}
}

func BenchmarkTrimUnicode(b *testing.B) {
	s := "«»«»" + benchText + "»«»«"
	for i := 0; i < b.N; i++ {
		if len(Trim(s, "«»")) == len(s) {
			b.Fatal("not trimmed")
		}
	}
}

func BenchmarkTrimFuncUnicode(b *testing.B) {
	f := func(r rune) bool { return r > 'z' }
	s := "ÜÜÜ" + benchText + "✓✓✓"
	for i := 0; i < b.N; i++ {
		if len(TrimFunc(s, f)) == len(s) {
			b.Fatal("not trimmed")
		}
	}
}

func BenchmarkRuneCountUnicode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if utf8.RuneCountInString(benchText) == len(benchText) {
			b.Fatal("all ASCII")
		}
	}
}

// Cut and Fields slice with plain slice expressions; the compiler proves
// their bounds are rune boundaries.

func BenchmarkCutUnicode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, after, ok := Cut(benchText, "—"); !ok || len(after) == 0 {
			b.Fatal("not cut")
		}
	}
}

func BenchmarkFieldsFuncUnicode(b *testing.B) {
	f := func(r rune) bool { return r == ' ' || r == ',' }
	for i := 0; i < b.N; i++ {
		n := 0
		for range FieldsFuncSeq(benchText, f) {
			n++
		}
		if n == 0 {
			b.Fatal("no fields")
		}
	}
}
