package escape

import (
	"fmt"
	"iter"

	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// The escapes of a summary.
//
// The summaries of translated code have millions of keys between them, so a
// key costs what it must: the key and its reason are stored once, in the log
// of the parameter the key belongs to, which never moves an entry (so a
// reason can refer to the reason it was derived from), and a table of
// pointers to the entries finds a key.

// An escEnt is a key of escapes and its reason.
type escEnt struct {
	k pkey
	h uint64 // the hash of k (summary.keyHash)
	r reason
}

const escChunk = 1024

// An escLog is a list of escEnt that only grows, in chunks, so that the
// address of an entry stays.
type escLog struct {
	chunks []*[escChunk]escEnt
	n      int
}

func (l *escLog) len() int { return l.n }

func (l *escLog) at(i int) *escEnt { return &l.chunks[i/escChunk][i%escChunk] }

func (l *escLog) add(k pkey, h uint64, r reason) *escEnt {
	if l.n == len(l.chunks)*escChunk {
		l.chunks = append(l.chunks, new([escChunk]escEnt))
	}
	e := l.at(l.n)
	l.n++
	e.k, e.h, e.r = k, h, r
	return e
}

// An escSet finds the entry of a key: open addressing, linear probing.
type escSet struct {
	slots []*escEnt
	n     int
}

func (s *escSet) find(k pkey, h uint64) *escEnt {
	if len(s.slots) == 0 {
		return nil
	}
	mask := uint64(len(s.slots) - 1)
	for i := h & mask; ; i = (i + 1) & mask {
		e := s.slots[i]
		if e == nil {
			return nil
		}
		if e.k == k {
			return e
		}
	}
}

func (s *escSet) insert(e *escEnt) {
	if 2*(s.n+1) > len(s.slots) {
		old := s.slots
		size := 16
		if len(old) > 0 {
			size = 2 * len(old)
		}
		s.slots = make([]*escEnt, size)
		for _, o := range old {
			if o != nil {
				s.place(o)
			}
		}
	}
	s.place(e)
	s.n++
}

func (s *escSet) place(e *escEnt) {
	mask := uint64(len(s.slots) - 1)
	i := e.h & mask
	for s.slots[i] != nil {
		i = (i + 1) & mask
	}
	s.slots[i] = e
}

// hasEscape reports whether k is a key of escapes.
func (s *summary) hasEscape(k pkey) bool {
	k.shape = shapeWhole
	return s.escSet.find(k, s.keyHash(k)) != nil
}

// keyHash hashes a key. A filter type has no hash of its own to use, so the
// summary numbers the filters it sees.
func (s *summary) keyHash(k pkey) uint64 {
	var id uint64
	if k.filt != nil {
		n, ok := s.filtIDs[k.filt]
		if !ok {
			if s.filtIDs == nil {
				s.filtIDs = map[types2.Type]uint32{}
			}
			n = uint32(len(s.filtIDs)) + 1
			s.filtIDs[k.filt] = n
		}
		id = uint64(n)
	}
	h := uint64(14695981039346656037)
	for i := 0; i < len(k.path); i++ {
		h = (h ^ uint64(k.path[i])) * 1099511628211
	}
	flags := uint64(0)
	for i, b := range [...]bool{k.borrowed, k.counts, k.directCount, k.ge} {
		if b {
			flags |= 1 << i
		}
	}
	flags |= uint64(k.shape) << 4
	h = (h ^ uint64(k.idx)<<48 ^ uint64(k.filtAt)<<32 ^ id<<8 ^ flags) * 1099511628211
	h ^= h >> 29
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 32
	return h
}

// escapeReason returns the reason of key k, or nil.
func (s *summary) escapeReason(k pkey) *reason {
	k.shape = shapeWhole
	if e := s.escSet.find(k, s.keyHash(k)); e != nil {
		return &e.r
	}
	return nil
}

// numEscapes is the number of keys of escapes.
func (s *summary) numEscapes() int { return s.escSet.n }

// escapeKeys lists the keys of escapes, by parameter and in the order added.
func (s *summary) escapeKeys() iter.Seq[pkey] {
	return func(yield func(pkey) bool) {
		for i := range s.escLog {
			l := &s.escLog[i]
			for j := 0; j < l.n; j++ {
				if !yield(l.at(j).k) {
					return
				}
			}
		}
	}
}

// addEscape adds key k with reason r and reports whether it was new.
func (s *summary) addEscape(k pkey, r reason) bool {
	// What a pointer is of matters to the filters that judge it, which a key
	// of escapes applies to the pointers the caller passes, by their own
	// shapes (walkTok): the key's shape is of no use, and a key for each
	// would be millions.
	k.shape = shapeWhole
	h := s.keyHash(k)
	if s.escSet.find(k, h) != nil {
		return false
	}
	for len(s.escLog) <= k.idx {
		s.escLog = append(s.escLog, escLog{})
	}
	e := s.escLog[k.idx].add(k, h, r)
	e.r.ent = e
	s.escSet.insert(e)
	s.ver++
	s.escKeys = nil
	return true
}

// callReason makes the reason of an escape found by following a callee's
// escape (whose reason is parent) from a parameter pointer: "passes it to
// the callee (at pos), which <parent>".
func (w *walker) callReason(fn *function, act string, pos token.Pos, parent *reason) reason {
	return reason{a: w.a, fn: fn, act: act, pos: pos, parent: parent}
}

// callText is the text of a reason made by callReason.
func (r *reason) callText() string {
	return fmt.Sprintf(r.act, r.fn.paramDesc(r.ent.k)) + " (" + shortPos(r.a.fset.Position(r.pos)) + ")" + ", which " + r.parent.String()
}
