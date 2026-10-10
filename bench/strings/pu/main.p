// strings: wc-style scan of a generated text buffer passed as a []byte.
// Counts lines, words and bytes, tracks the longest word, counts words equal
// to a string constant, and folds an FNV-1a hash of every word into a running
// hash. One byte of the buffer changes before each pass.
// Measures byte indexing, bounds checks, compare chains, string indexing and
// struct return by value.
package main

import "pluto/fmt"

const N = 16000000
const Reps = 35
const Pattern = "go"

var text [N]byte

var seed uint64 = 42

func nextRand() int64 {
	seed = seed*6364136223846793005 + 1442695040888963407
	return int64(seed >> 33)
}

type Stats struct {
	Lines   int64
	Words   int64
	Bytes   int64
	Longest int64
	Matches int64
	Hash    uint32
}

func generate() {
	for i := 0; i < N; i++ {
		r := nextRand() % 64
		if r < 10 {
			text[i] = ' '
		} else if r == 10 {
			text[i] = '\n'
		} else {
			text[i] = 'a' + byte(r%26)
		}
	}
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\t' || c == '\r'
}

func matches(b []byte, start int, end int, pat string) bool {
	if end-start != len(pat) {
		return false
	}
	for i := 0; i < len(pat); i++ {
		if b[start+i] != pat[i] {
			return false
		}
	}
	return true
}

func scan(b []byte) Stats {
	var s Stats
	inWord := false
	start := 0
	wordHash := uint32(0)
	n := len(b)
	for i := 0; i < n; i++ {
		c := b[i]
		if c == '\n' {
			s.Lines++
		}
		if isSpace(c) {
			if inWord {
				s.Words++
				s.Hash = s.Hash*31 + wordHash
				if int64(i-start) > s.Longest {
					s.Longest = int64(i - start)
				}
				if matches(b, start, i, Pattern) {
					s.Matches++
				}
				inWord = false
			}
		} else {
			if !inWord {
				inWord = true
				start = i
				wordHash = 2166136261
			}
			wordHash = (wordHash ^ uint32(c)) * 16777619
		}
	}
	if inWord {
		s.Words++
		s.Hash = s.Hash*31 + wordHash
		if int64(n-start) > s.Longest {
			s.Longest = int64(n - start)
		}
		if matches(b, start, n, Pattern) {
			s.Matches++
		}
	}
	s.Bytes = int64(n)
	return s
}

func main() int64 {
	generate()
	var s Stats
	totalWords := int64(0)
	hashXor := uint32(0)
	for r := 0; r < Reps; r++ {
		text[(r*7919)%N] = 'a' + byte(r%26)
		s = scan(text[:])
		totalWords += s.Words
		hashXor ^= s.Hash
	}
	fmt.Printf("%d\n", s.Lines)
	fmt.Printf("%d\n", s.Words)
	fmt.Printf("%d\n", s.Bytes)
	fmt.Printf("%d\n", s.Longest)
	fmt.Printf("%d\n", s.Matches)
	fmt.Printf("%d\n", int64(s.Hash))
	fmt.Printf("%d\n", totalWords)
	fmt.Printf("%d\n", int64(hashXor))
	return 0
}
