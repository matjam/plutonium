// Map micro-benchmarks, the Go version of pu/main.p and hash/main.p:
// inserting, finding, missing, deleting and ranging over a map with
// uint64 keys, and counting words in a map with string keys. Each phase
// prints nanoseconds per operation, and the last line a checksum that
// every version must print the same.
package main

import (
	"os"
	"strconv"
	"time"
)

const (
	N      = 1 << 20 // integer keys
	W      = 50000   // distinct words
	T      = 1 << 22 // words counted
	Rounds = 3
)

var words [W]string

var seed uint64 = 7

func next() uint64 {
	seed = seed*6364136223846793005 + 1442695040888963407
	return seed >> 33
}

func key(i int) uint64 { return uint64(i) * 0x9E3779B97F4A7C15 }

func report(name string, ns int64, ops int) {
	os.Stdout.WriteString(name + " " + strconv.FormatInt(ns/int64(ops), 10) + " ns/op\n")
}

func main() {
	for i := range words {
		words[i] = "w" + strconv.Itoa(int(next()%1000000)) + "x" + strconv.Itoa(i)
	}
	var check uint64
	var tIns, tHit, tMiss, tDel, tRange, tCount, tLook int64
	for r := 0; r < Rounds; r++ {
		start := time.Now()
		m := make(map[uint64]uint64)
		for i := 0; i < N; i++ {
			m[key(i)] = uint64(i)
		}
		tIns += time.Since(start).Nanoseconds()

		start = time.Now()
		for i := 0; i < N; i++ {
			check += m[key(i)]
		}
		tHit += time.Since(start).Nanoseconds()

		start = time.Now()
		for i := 0; i < N; i++ {
			if _, ok := m[key(i)+1]; ok {
				check++
			}
		}
		tMiss += time.Since(start).Nanoseconds()

		start = time.Now()
		for i := 0; i < N; i += 2 {
			delete(m, key(i))
		}
		tDel += time.Since(start).Nanoseconds()

		start = time.Now()
		for k, v := range m {
			check += k ^ v
		}
		tRange += time.Since(start).Nanoseconds()

		start = time.Now()
		counts := map[string]int{}
		for t := 0; t < T; t++ {
			counts[words[next()%W]]++
		}
		tCount += time.Since(start).Nanoseconds()

		start = time.Now()
		for i := range words {
			check += uint64(counts[words[i]])
		}
		tLook += time.Since(start).Nanoseconds()
		check += uint64(len(counts) + len(m))
	}
	report("int-insert", tIns, N*Rounds)
	report("int-hit", tHit, N*Rounds)
	report("int-miss", tMiss, N*Rounds)
	report("int-delete", tDel, N/2*Rounds)
	report("int-range", tRange, N/2*Rounds)
	report("str-count", tCount, T*Rounds)
	report("str-lookup", tLook, W*Rounds)
	os.Stdout.WriteString("check " + strconv.FormatUint(check, 10) + "\n")
}
