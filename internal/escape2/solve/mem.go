package solve

import "github.com/matjam/plutonium/internal/escape2/domain"

// The solver's working memory is carved from the workspace arena (plan
// section (e), "SCC arena"): growable word vectors and open-addressing hash
// tables over them. Nothing here is freed one piece at a time; the scheduler
// resets the arena when the SCC is final.

// vec is a growable vector of words in an arena. Growing copies into a new
// slab twice the size; the old slab stays in the arena until it is reset.
type vec struct {
	s []uint64
}

func (v *vec) grow(a domain.Arena, need int) {
	if need <= cap(v.s) {
		return
	}
	n := max(2*cap(v.s), need, 4)
	ns := a.Alloc(n)
	copy(ns, v.s)
	v.s = ns[:len(v.s)]
}

func (v *vec) push(a domain.Arena, x uint64) int {
	v.grow(a, len(v.s)+1)
	v.s = append(v.s, x)
	return len(v.s) - 1
}

// resize makes the vector n words long, the new words zero.
func (v *vec) resize(a domain.Arena, n int) {
	v.grow(a, n)
	old := len(v.s)
	v.s = v.s[:n]
	for i := old; i < n; i++ {
		v.s[i] = 0
	}
}

// table is an open-addressing hash table from a pair of words to a word, in
// an arena. Values are stored plus one, so a zero slot is empty.
type table struct {
	slots []uint64 // k1, k2, val+1
	n     int
}

func mix(k1, k2 uint64) uint64 {
	h := k1*0x9e3779b97f4a7c15 ^ (k2+0x632be59bd9b4e019)*0xbf58476d1ce4e5b9
	h ^= h >> 31
	h *= 0x94d049bb133111eb
	h ^= h >> 29
	return h
}

func (t *table) get(k1, k2 uint64) (uint64, bool) {
	if t.n == 0 {
		return 0, false
	}
	m := uint64(len(t.slots) / 3)
	for i := mix(k1, k2) & (m - 1); ; i = (i + 1) & (m - 1) {
		s := t.slots[3*i : 3*i+3]
		if s[2] == 0 {
			return 0, false
		}
		if s[0] == k1 && s[1] == k2 {
			return s[2] - 1, true
		}
	}
}

// put sets the value of (k1, k2) and reports whether the key is new.
func (t *table) put(a domain.Arena, k1, k2, v uint64) bool {
	if 2*(t.n+1) > len(t.slots)/3 {
		t.rehash(a)
	}
	m := uint64(len(t.slots) / 3)
	for i := mix(k1, k2) & (m - 1); ; i = (i + 1) & (m - 1) {
		s := t.slots[3*i : 3*i+3]
		if s[2] == 0 {
			s[0], s[1], s[2] = k1, k2, v+1
			t.n++
			return true
		}
		if s[0] == k1 && s[1] == k2 {
			s[2] = v + 1
			return false
		}
	}
}

// add inserts (k1, k2) with value 0 and reports whether it was absent: a set
// of pairs, for de-duplication.
func (t *table) add(a domain.Arena, k1, k2 uint64) bool {
	if _, ok := t.get(k1, k2); ok {
		return false
	}
	return t.put(a, k1, k2, 0)
}

func (t *table) rehash(a domain.Arena) {
	old := t.slots
	m := max(16, 2*len(old)/3)
	t.slots = a.Alloc(3 * m)
	t.n = 0
	for i := 0; i+2 < len(old); i += 3 {
		if old[i+2] != 0 {
			t.put(a, old[i], old[i+1], old[i+2]-1)
		}
	}
}
