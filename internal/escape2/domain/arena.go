package domain

// The arena: memory carved from chunks that are released together, so that
// an SCC's working state needs no garbage collector and is gone when the SCC
// is final (plan section (e)).
//
// Nothing allocated from an arena may be used after its Reset. Two things
// make a use after Reset visible:
//
//   - every Reset starts a new epoch, and a Sets table on the arena makes IDs
//     that carry their epoch, so an old SetID panics when used;
//   - an arena made by NewCheckedArena never reuses memory: Reset and Release
//     fill every word it handed out with PoisonWord and drop it, so a slice
//     kept past them reads poison instead of another SCC's data.
//
// Release gives an arena's chunks, and Reset its big allocations, to a pool
// that every arena takes memory from, cleared. An arena made fresh for each
// solve then reuses the memory of the solves before it, and still holds,
// and reports in Bytes, only what it took itself. The self-hosted compiler
// frees nothing, and without the pool held the memory of every solve.

import "sync"

// PoisonWord is what a checked arena writes over its memory at Reset.
const PoisonWord uint64 = 0xdeadbeefdeadbeef

// chunkWords is the size of an arena chunk; allocations of more than a
// quarter of it get a chunk of their own.
const chunkWords = 1 << chunkClass

const chunkClass = 13

// arena implements Arena.
type arena struct {
	chunks  [][]uint64 // the chunks the arena holds, in use or kept for reuse
	used    int        // chunks[:used] are in use; chunks[used-1] is the current one
	pos     int        // words used of the current chunk
	big     [][]uint64 // allocations with a chunk of their own
	bigSize int        // words in big
	epoch   uint32
	checked bool
	peak    int
	alloc   int // words handed out since Reset
}

// NewArena returns an empty arena.
func NewArena() Arena { return &arena{} }

// NewCheckedArena returns an arena that never reuses memory and poisons it
// at Reset, for tests that a client keeps nothing past a Reset.
func NewCheckedArena() Arena { return &arena{checked: true} }

// pool holds the memory of released arenas, and the big allocations of
// reset ones, for other arenas: blocks of a power of two words, by size,
// at most maxPooledWords in all.
var pool struct {
	mu    sync.Mutex
	free  [64][][]uint64 // free[k]: blocks of 1<<k words
	words int
}

// maxPooledWords bounds the pool: 1 GiB.
const maxPooledWords = 1 << 27

// sizeClass is the k of the smallest block of 1<<k words that holds n.
func sizeClass(n int) int {
	k := 0
	for 1<<k < n {
		k++
	}
	return k
}

// take returns a zeroed block of 1<<k words, from the pool if it has one.
func take(k int) []uint64 {
	pool.mu.Lock()
	var b []uint64
	if f := pool.free[k]; len(f) > 0 {
		b = f[len(f)-1]
		pool.free[k] = f[:len(f)-1]
		pool.words -= len(b)
	}
	pool.mu.Unlock()
	if b == nil {
		return make([]uint64, 1<<k)
	}
	clear(b)
	return b
}

// give puts blocks made by take into the pool, while it has room.
func give(bs [][]uint64) {
	pool.mu.Lock()
	for _, b := range bs {
		if pool.words+len(b) > maxPooledWords {
			break
		}
		k := sizeClass(len(b))
		pool.free[k] = append(pool.free[k], b)
		pool.words += len(b)
	}
	pool.mu.Unlock()
}

func (a *arena) Alloc(n int) []uint64 {
	if n < 0 {
		panic("escape2/domain: Alloc: negative size")
	}
	if n == 0 {
		return nil
	}
	a.alloc += n
	if n > chunkWords/4 {
		var b []uint64
		if a.checked {
			b = make([]uint64, n)
		} else {
			b = take(sizeClass(n))
		}
		a.big = append(a.big, b)
		a.bigSize += n
		a.notePeak()
		return b[:n:n]
	}
	if a.used == 0 || a.pos+n > chunkWords {
		if a.used == len(a.chunks) {
			a.chunks = append(a.chunks, take(chunkClass))
			a.notePeak()
		} else {
			clear(a.chunks[a.used])
		}
		a.used++
		a.pos = 0
	}
	c := a.chunks[a.used-1]
	s := c[a.pos : a.pos+n : a.pos+n]
	a.pos += n
	return s
}

func (a *arena) notePeak() {
	if b := a.Bytes(); b > a.peak {
		a.peak = b
	}
}

func (a *arena) Bytes() int { return (len(a.chunks)*chunkWords + a.bigSize) * 8 }

func (a *arena) Peak() int { return a.peak }

func (a *arena) Allocated() int { return a.alloc * 8 }

func (a *arena) Reset() {
	if a.checked {
		for _, c := range a.chunks[:a.used] {
			poison(c)
		}
		for _, b := range a.big {
			poison(b)
		}
		a.chunks = nil
	} else {
		give(a.big)
	}
	a.used, a.pos, a.alloc = 0, 0, 0
	a.big, a.bigSize = nil, 0
	a.epoch++
}

func (a *arena) Release() {
	if !a.checked {
		give(a.chunks)
		a.chunks = nil
	}
	a.Reset()
	a.chunks = nil
}

func poison(s []uint64) {
	for i := range s {
		s[i] = PoisonWord
	}
}
