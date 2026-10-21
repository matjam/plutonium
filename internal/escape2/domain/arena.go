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
//   - an arena made by NewCheckedArena never reuses memory: Reset fills every
//     word it handed out with PoisonWord and drops it, so a slice kept past
//     Reset reads poison instead of another SCC's data.

// PoisonWord is what a checked arena writes over its memory at Reset.
const PoisonWord uint64 = 0xdeadbeefdeadbeef

// chunkWords is the size of an arena chunk; allocations of more than a
// quarter of it get a chunk of their own.
const chunkWords = 8192

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
}

// NewArena returns an empty arena.
func NewArena() Arena { return &arena{} }

// NewCheckedArena returns an arena that never reuses memory and poisons it
// at Reset, for tests that a client keeps nothing past a Reset.
func NewCheckedArena() Arena { return &arena{checked: true} }

func (a *arena) Alloc(n int) []uint64 {
	if n < 0 {
		panic("escape2/domain: Alloc: negative size")
	}
	if n == 0 {
		return nil
	}
	if n > chunkWords/4 {
		b := make([]uint64, n)
		a.big = append(a.big, b)
		a.bigSize += n
		a.notePeak()
		return b
	}
	if a.used == 0 || a.pos+n > chunkWords {
		if a.used == len(a.chunks) {
			a.chunks = append(a.chunks, make([]uint64, chunkWords))
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

func (a *arena) Reset() {
	if a.checked {
		for _, c := range a.chunks[:a.used] {
			poison(c)
		}
		for _, b := range a.big {
			poison(b)
		}
		a.chunks = nil
	}
	a.used, a.pos = 0, 0
	a.big, a.bigSize = nil, 0
	a.epoch++
}

func poison(s []uint64) {
	for i := range s {
		s[i] = PoisonWord
	}
}
