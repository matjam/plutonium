package domain

import (
	"math/rand/v2"
	"testing"
)

// The benchmarks report allocations: everything on the analysis's hot path
// (extending a key, interning a set that exists, a memoized union, a lookup
// in an antichain) must make none.

func BenchmarkExtendKey(b *testing.B) {
	ps := NewPaths()
	r := rand.New(rand.NewPCG(61, 62))
	words := make([][]Step, 1024)
	for i := range words {
		words[i] = randWord(r, 12)
		walk(ps, words[i]...)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		walk(ps, words[i&1023]...)
	}
}

func BenchmarkCovers(b *testing.B) {
	ps := NewPaths()
	r := rand.New(rand.NewPCG(63, 64))
	keys := randKeys(r, ps, 1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Covers(ps, keys[i&1023], keys[(i>>10)&1023])
	}
}

// setsFixture makes n sets of small random size in a fresh table.
func setsFixture(n int) (Sets, []SetID) {
	s := NewSets(NewArena())
	r := rand.New(rand.NewPCG(65, 66))
	ids := make([]SetID, n)
	for i := range ids {
		xs := make([]uint64, 1+r.IntN(8))
		for j := range xs {
			xs[j] = r.Uint64N(4096)
		}
		ids[i] = s.Intern(xs)
	}
	return s, ids
}

func BenchmarkInternExisting(b *testing.B) {
	s, ids := setsFixture(1024)
	elems := make([][]uint64, len(ids))
	for i, id := range ids {
		elems[i] = append([]uint64(nil), s.Elems(id)...)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Intern(elems[i&1023])
	}
}

// BenchmarkInternNew interns new sets, resetting the table every 64K sets
// as an SCC driver would; it reports the memory one set costs.
func BenchmarkInternNew(b *testing.B) {
	a := NewArena()
	s := NewSets(a)
	r := rand.New(rand.NewPCG(67, 68))
	buf := make([]uint64, 8)
	b.ReportAllocs()
	b.ResetTimer()
	maxBytes, n := 0, 0
	for i := 0; i < b.N; i++ {
		if i&0xffff == 0 && i > 0 {
			if by := s.Bytes(); by > maxBytes {
				maxBytes, n = by, 0x10000
			}
			s.Reset()
			a.Reset()
		}
		for j := range buf {
			buf[j] = r.Uint64()
		}
		s.Intern(buf)
	}
	if n > 0 {
		b.ReportMetric(float64(maxBytes)/float64(n), "B/set(8)")
	}
}

func BenchmarkUnionMemo(b *testing.B) {
	s, ids := setsFixture(64)
	for i := range 64 * 64 {
		s.Union(ids[i&63], ids[i>>6])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Union(ids[i&63], ids[(i>>6)&63])
	}
}

func BenchmarkUnionFresh(b *testing.B) {
	s, ids := setsFixture(1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x, y := ids[i&1023], ids[(i*7+1)&1023]
		s.Union(x, y)
		s.Diff(x, y)
	}
}

func BenchmarkAntichain(b *testing.B) {
	ps := NewPaths()
	r := rand.New(rand.NewPCG(69, 70))
	src := randKeys(r, ps, 48)
	buf := make([]Key, len(src))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(buf, src)
		Antichain(ps, buf)
	}
}

func BenchmarkCoveredBy(b *testing.B) {
	ps := NewPaths()
	r := rand.New(rand.NewPCG(71, 72))
	set := Antichain(ps, randKeys(r, ps, 64))
	probe := randKeys(r, ps, 1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CoveredBy(ps, set, probe[i&1023])
	}
}
