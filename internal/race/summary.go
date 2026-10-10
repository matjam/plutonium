package race

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// A lock is a mutex (sync.Mutex, sync.RWMutex or the runtime's rt.Lock)
// named by its place, and whether it is held for writing (excl) or only
// for reading.
type lock struct {
	place vpath
	excl  bool
}

// A lockset is the set of locks a function has taken and still holds at a
// point, relative to its entry: they are held whatever the caller holds.
type lockset []lock

func (s lockset) find(p vpath) int {
	for i, l := range s {
		if l.place == p {
			return i
		}
	}
	return -1
}

// with returns s with p held (exclusively or not).
func (s lockset) with(p vpath, excl bool) lockset {
	if i := s.find(p); i >= 0 {
		if s[i].excl || !excl {
			return s
		}
		out := append(lockset(nil), s...)
		out[i].excl = true
		return out
	}
	out := make(lockset, len(s), len(s)+1)
	copy(out, s)
	return append(out, lock{place: p, excl: excl})
}

// without returns s without p.
func (s lockset) without(p vpath) lockset {
	i := s.find(p)
	if i < 0 {
		return s
	}
	out := append(lockset(nil), s[:i]...)
	return append(out, s[i+1:]...)
}

// meet returns the locks held in both s and t: those held on every path.
func (s lockset) meet(t lockset) lockset {
	if s.equal(t) {
		return s
	}
	var out lockset
	for _, l := range s {
		if i := t.find(l.place); i >= 0 {
			out = append(out, lock{place: l.place, excl: l.excl && t[i].excl})
		}
	}
	return out
}

// equal compares two locksets as sets.
func (s lockset) equal(t lockset) bool {
	if len(s) != len(t) {
		return false
	}
	for _, l := range s {
		i := t.find(l.place)
		if i < 0 || t[i].excl != l.excl {
			return false
		}
	}
	return true
}

// String returns the locks in a canonical order.
func (s lockset) String() string {
	var b []string
	for _, l := range s {
		k := l.place.key()
		if !l.excl {
			k += "(r)"
		}
		b = append(b, k)
	}
	sort.Strings(b)
	return strings.Join(b, ",")
}

// A released set is the locks a function may have released that its
// caller may have held: all of them, or those listed.
// maxReleased bounds the locks a released set lists.
const maxReleased = 16

type released struct {
	all  bool
	keys []vpath
}

func (r released) has(p vpath) bool {
	if r.all {
		return true
	}
	for _, q := range r.keys {
		if q == p {
			return true
		}
	}
	return false
}

func (r released) with(p vpath) released {
	if r.all || r.has(p) {
		return r
	}
	if len(r.keys) >= maxReleased {
		// Too many to follow: as if every lock had been released.
		return released{all: true}
	}
	keys := make([]vpath, len(r.keys), len(r.keys)+1)
	copy(keys, r.keys)
	return released{keys: append(keys, p)}
}

func (r released) union(s released) released {
	if r.all || s.all {
		return released{all: true}
	}
	if len(s.keys) == 0 {
		return r
	}
	if len(r.keys) == 0 {
		return s
	}
	out := r
	for _, k := range s.keys {
		out = out.with(k)
	}
	return out
}

func (r released) equal(s released) bool {
	if r.all || s.all {
		return r.all == s.all
	}
	if len(r.keys) != len(s.keys) {
		return false
	}
	for _, k := range r.keys {
		if !s.has(k) {
			return false
		}
	}
	return true
}

// String returns the released locks in a canonical order.
func (r released) String() string {
	if r.all {
		return "*"
	}
	var b []string
	for _, k := range r.keys {
		b = append(b, k.key())
	}
	sort.Strings(b)
	return strings.Join(b, ",")
}

// A frame is one step of the chain of calls that led to an access, for
// messages: the call, in the function that makes it.
type frame struct {
	pos    token.Pos
	callee string
}

// An access is a use of a package variable, of a synced object's memory,
// or of memory a parameter reaches.
type access struct {
	place  vpath
	write  bool
	atomic bool // an atomic operation, or a method of a sync type
	locks  lockset
	rel    released
	pos    token.Pos
	pre    bool   // the program only: before any task was started
	tkey   string // "field:T.f": the field of the object a pointer led to, for the type rule
}

// An accKey identifies what matters of an access for the rules; accesses
// that differ only in position are kept once, at the first position seen.
type accKey struct {
	place vpath
	flags uint8
	tkey  string
	locks string // the locks and released locks, when there are any
}

func (x *access) key() accKey {
	k := accKey{place: x.place, tkey: x.tkey}
	if x.write {
		k.flags |= 1
	}
	if x.atomic {
		k.flags |= 2
	}
	if x.pre {
		k.flags |= 4
	}
	if len(x.locks) > 0 || x.rel.all || len(x.rel.keys) > 0 {
		k.locks = "L" + x.locks.String() + "|R" + x.rel.String()
	}
	return k
}

// A paramCall is a call of a func-valued parameter.
type paramCall struct {
	idx   int
	locks lockset
	rel   released
	pos   token.Pos
	pre   bool
}

// A spawn is a task start: a go statement, or a function value handed to
// the runtime's task start (rt.Go, time.AfterFunc).
type spawn struct {
	pos   token.Pos // the go statement or the call that starts the task
	root  origins   // the function the task runs: rFunc, rLit, or a parameter
	args  []origins // its arguments, receiver first
	multi bool      // may run more than once, so several instances at a time
	via   []frame
	desc  string // "go worker(...)"
}

func (s *spawn) key() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|", s.pos)
	for _, o := range s.root {
		b.WriteString(o.vp.key() + ";")
	}
	b.WriteString("|")
	for _, a := range s.args {
		for _, o := range a {
			b.WriteString(o.vp.key() + o.tag + ";")
		}
		b.WriteString("/")
	}
	fmt.Fprintf(&b, "|%v", s.multi)
	return b.String()
}

// A crossing is a value that another task may use: an argument of a go
// statement, a value sent on a channel. The sendable rule applies to it.
type crossing struct {
	pos  token.Pos
	what string // "the argument p of go worker", "the value sent on ch"
	typ  types2.Type
	vals origins
	expr ast.Expr
	via  []frame
	fn   string // the function where the value crosses
}

func (c *crossing) key() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|%s|", c.pos, c.what)
	for _, o := range c.vals {
		b.WriteString(o.vp.key() + o.tag + ";")
	}
	return b.String()
}

// An edge is a call whose callee's own accesses (of package variables and
// synced objects) happen in the caller's context: with the caller's locks,
// and with the callee's parameters bound to args.
type edge struct {
	f     *function
	args  []origins
	locks lockset
	rel   released
	pre   bool
}

func (e *edge) key() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%p|%s|%s|%v|", e.f, e.locks, e.rel, e.pre)
	for _, a := range e.args {
		for _, o := range a {
			b.WriteString(o.vp.key())
			b.WriteString(o.tag)
			b.WriteByte(';')
		}
		b.WriteByte('/')
	}
	return b.String()
}

// A summary is what a function does that its callers need to know.
type summary struct {
	accesses []*access
	accKeys  map[accKey]bool
	calls    []*paramCall
	callKeys map[string]bool
	edges    []*edge
	edgeKeys map[string]bool
	spawns   []*spawn
	spKeys   map[string]bool
	cross    []*crossing
	crKeys   map[string]bool
	results  []origins

	exitLocks lockset // taken and still held when it returns
	exitRel   released
	maySpawn  bool
	pubParams bits   // parameters whose memory it publishes
	fresh     []bool // results that are memory it allocated and did not publish
	notFresh  []bool
}

func (s *summary) addAccess(x *access) {
	if s.accKeys == nil {
		s.accKeys = map[accKey]bool{}
	}
	k := x.key()
	if s.accKeys[k] {
		return
	}
	s.accKeys[k] = true
	s.accesses = append(s.accesses, x)
}

func (s *summary) addSpawn(x *spawn) {
	if s.spKeys == nil {
		s.spKeys = map[string]bool{}
	}
	k := x.key()
	if s.spKeys[k] {
		return
	}
	s.spKeys[k] = true
	s.spawns = append(s.spawns, x)
	s.maySpawn = true
}

func (s *summary) addCrossing(x *crossing) {
	if s.crKeys == nil {
		s.crKeys = map[string]bool{}
	}
	k := x.key()
	if s.crKeys[k] {
		return
	}
	s.crKeys[k] = true
	s.cross = append(s.cross, x)
}

func (s *summary) addPubParam(i int) { s.pubParams = s.pubParams.with(i) }

func (s *summary) addEdge(e *edge) {
	if s.edgeKeys == nil {
		s.edgeKeys = map[string]bool{}
	}
	k := e.key()
	if s.edgeKeys[k] {
		return
	}
	s.edgeKeys[k] = true
	s.edges = append(s.edges, e)
}

// addCall records a call of a func-valued parameter, once per parameter,
// locks and phase: calls that differ only in where they are, or in the
// chain of calls that leads to them, are kept once, so that a recursive
// function's summary does not grow with each round of the fixpoint.
func (s *summary) addCall(c *paramCall) {
	if s.callKeys == nil {
		s.callKeys = map[string]bool{}
	}
	k := fmt.Sprintf("%d|%s|%s|%v", c.idx, c.locks, c.rel, c.pre)
	if s.callKeys[k] {
		return
	}
	s.callKeys[k] = true
	s.calls = append(s.calls, c)
}

// noteFresh records whether one return of result i is fresh memory.
func (s *summary) noteFresh(i int, fresh bool) {
	for len(s.fresh) <= i {
		s.fresh = append(s.fresh, false)
		s.notFresh = append(s.notFresh, false)
	}
	if fresh {
		s.fresh[i] = true
	} else {
		s.notFresh[i] = true
	}
}

// freshResult reports whether every return gives fresh memory as result i.
func (s *summary) freshResult(i int) bool {
	return i < len(s.fresh) && s.fresh[i] && !s.notFresh[i]
}

// bits is a small set of parameter indexes.
type bits uint64

func (b bits) has(i int) bool { return i >= 64 || b&(1<<uint(i)) != 0 }

func (b bits) with(i int) bits {
	if i >= 64 {
		return b
	}
	return b | 1<<uint(i)
}

func (s *summary) addResult(i int, v origins) {
	for len(s.results) <= i {
		s.results = append(s.results, nil)
	}
	s.results[i].addAll(v)
}

func sameSet[K comparable](a, b map[K]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// equal reports whether two summaries of a function say the same.
func (s *summary) equal(t *summary) bool {
	if !sameSet(s.accKeys, t.accKeys) || !sameSet(s.callKeys, t.callKeys) || !sameSet(s.edgeKeys, t.edgeKeys) ||
		!sameSet(s.spKeys, t.spKeys) || !sameSet(s.crKeys, t.crKeys) {
		return false
	}
	if s.maySpawn != t.maySpawn || s.pubParams != t.pubParams || !s.exitLocks.equal(t.exitLocks) || !s.exitRel.equal(t.exitRel) {
		return false
	}
	if fmt.Sprint(s.fresh, s.notFresh) != fmt.Sprint(t.fresh, t.notFresh) || len(s.results) != len(t.results) {
		return false
	}
	for i, r := range s.results {
		if len(r) != len(t.results[i]) {
			return false
		}
		for _, o := range r {
			if !t.results[i].has(o) {
				return false
			}
		}
	}
	return true
}
