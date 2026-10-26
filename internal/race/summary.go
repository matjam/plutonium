package race

import (
	"fmt"
	"os"
	"sort"
	"strconv"
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
		// A region released with no site names every region of its kind.
		if q == p || q.root.kind == rRegion && q.root.pos == 0 && p.root.kind == rRegion && q.root.idx == p.root.idx {
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
	pre    bool      // the program only: before any task was started
	tkey   string    // "field:T.f": the field of the object a pointer led to, for the type rule
	uses   []typeUse // the types of what is used, when known (scope.go); none stands for any
	cpu    *cpuUse   // a use of a per-CPU variable (region.go), which the rules of sharing do not see
	free   bool      // a write that frees or drops the memory (free.go)
}

// An accKey identifies what matters of an access for the rules; accesses
// that differ only in position are kept once, at the first position seen.
type accKey struct {
	place vpath
	flags uint8
	tkey  string
	locks string // the locks and released locks, when there are any
	cpu   string // a use of a per-CPU variable
}

func (x *access) key() accKey {
	k := accKey{place: x.place, tkey: x.tkey}
	if x.cpu != nil {
		k.cpu = x.cpu.key()
	}
	if x.write {
		k.flags |= 1
	}
	if x.atomic {
		k.flags |= 2
	}
	if x.pre {
		k.flags |= 4
	}
	if x.free {
		k.flags |= 8
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
	conc  bool // made by scoped tasks that may run at the same time (scope.go)
}

// A spawn is a task start: a task statement, or a function value handed to
// the runtime's task start (rt.Go, time.AfterFunc).
type spawn struct {
	pos     token.Pos // the task statement or the call that starts the task
	root    origins   // the function the task runs: rFunc, rLit, or a parameter
	args    []origins // its arguments, receiver first
	multi   bool      // may run more than once, so several instances at a time
	handler bool      // an interrupt handler the program installs (x86.InterruptGate, x86.TrapGate), not a task
	trap    bool      // a handler entered through a trap gate: its body is not an interrupt-disabled region
	via     []frame
	desc    string // "task worker(...)"
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
	fmt.Fprintf(&b, "|%v|%v|%v", s.multi, s.handler, s.trap)
	return b.String()
}

// A crossing is a value that another task may use: an argument of a go
// statement, a value sent on a channel. The sendable rule applies to it.
type crossing struct {
	pos  token.Pos
	what string // "the argument p of task worker", "the value sent on ch"
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
	accKeys  map[accKey]*access
	accAt    map[accKey]int // where accKeys' accesses are in accesses
	typed    []*access      // uses of fields through cut places, for the type rule only
	tyKeys   map[accKey]*access
	tyAt     map[accKey]int
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
	mayAlloc  bool   // it may allocate, so it may call an allocator's Alloc (section 20.10)
	pubParams bits   // parameters whose memory it publishes
	fresh     []bool // results that are memory it allocated and did not publish
	notFresh  []bool
}

func (s *summary) addAccess(x *access) { s.putAccess(x) }

// putAccess adds x to the summary and returns the access the summary holds
// for it, and the typed access it adds with it if any.
func (s *summary) putAccess(x *access) (acc, ty *access) {
	if x.tkey != "" && x.place.root.kind != rParam {
		// The type rule has seen the use (recordType): only callers that
		// instantiate a parameter's place read the field again.
		y := *x
		y.tkey = ""
		x = &y
	} else if x.tkey != "" && strings.Contains(x.place.path, "#") {
		// Past a cut the place no longer tells the fields apart, so the
		// field it uses would make a copy of the access per field. The
		// type rule sees only the place's root and the locks of such a use
		// (recordType), so it is kept apart, at the root, and the access
		// for the other rules without its field.
		t := *x
		t.place = vpath{root: x.place.root, path: "#"}
		ty = s.putTyped(&t)
		y := *x
		y.tkey = ""
		x = &y
	}
	if s.accKeys == nil {
		s.accKeys = map[accKey]*access{}
		s.accAt = map[accKey]int{}
	}
	s.accesses, acc = putKeyed(s.accesses, s.accKeys, s.accAt, x)
	return acc, ty
}

func (s *summary) addTyped(x *access) { s.putTyped(x) }

// putTyped adds x to the typed accesses and returns the one held for it.
func (s *summary) putTyped(x *access) *access {
	if s.tyKeys == nil {
		s.tyKeys = map[accKey]*access{}
		s.tyAt = map[accKey]int{}
	}
	var y *access
	s.typed, y = putKeyed(s.typed, s.tyKeys, s.tyAt, x)
	return y
}

// putKeyed adds x to list, keyed in keys and at, and returns the list and
// the access held for it. An access past a cut held already gains x's types: the
// check of scoped tasks reads them there (cutApart), and one kept without
// them would hide x. Accesses are shared between summaries, so the one
// held is replaced, not changed.
func putKeyed(list []*access, keys map[accKey]*access, at map[accKey]int, x *access) ([]*access, *access) {
	k := x.key()
	y := keys[k]
	if y == nil {
		keys[k] = x
		at[k] = len(list)
		return append(list, x), x
	}
	if !strings.Contains(x.place.path, "#") {
		return list, y
	}
	u := joinUses(y.uses, x.uses)
	if u == nil {
		return list, y
	}
	z := *y
	z.uses = u
	keys[k] = &z
	list[at[k]] = &z
	return list, &z
}

// A typeUse is what a use past a cut touches: the type of the memory and
// the type of the object holding it in place, each nil when not known.
type typeUse struct{ typ, owner types2.Type }

// maxUses bounds the types one access keeps; past it the access stands
// for a use of any type, which only makes it meet more.
var maxUses = zzEnvInt("ZZUSES", 16)

// ZZ temporary instrumentation.
var zzOverflow, zzMax, zzJoins int

func zzEnvInt(k string, d int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return d
}

// anyUse reports whether uses stand for a use of any type.
func anyUse(uses []typeUse) bool {
	for _, u := range uses {
		if u.typ == nil && u.owner == nil {
			return true
		}
	}
	return len(uses) == 0
}

// usesOf returns the uses an access stands for, one of any type when it
// knows none.
func usesOf(x *access) []typeUse {
	if len(x.uses) == 0 {
		return []typeUse{{}}
	}
	return x.uses
}

// joinUses returns the uses of a and b together, or nil when b adds
// nothing to a. Neither is changed.
func joinUses(a, b []typeUse) []typeUse {
	if anyUse(a) {
		return nil
	}
	if anyUse(b) {
		return []typeUse{{}}
	}
	var out []typeUse
	for _, v := range b {
		if hasUse(a, v) || hasUse(out, v) {
			continue
		}
		if out == nil {
			out = append([]typeUse(nil), a...)
		}
		out = append(out, v)
		zzJoins++
		if len(out) > zzMax {
			zzMax = len(out)
		}
		if len(out) > maxUses {
			zzOverflow++
			return []typeUse{{}}
		}
	}
	return out
}

func hasUse(uses []typeUse, v typeUse) bool {
	for _, u := range uses {
		if sameType(u.typ, v.typ) && sameType(u.owner, v.owner) {
			return true
		}
	}
	return false
}

func sameType(T, U types2.Type) bool {
	if T == nil || U == nil {
		return T == nil && U == nil
	}
	return types2.Identical(T, U)
}

// sameUses reports whether the accesses past a cut of a and b, which have
// the same keys, keep as many types each.
func sameUses(a, b map[accKey]*access) bool {
	for k, x := range a {
		if strings.Contains(x.place.path, "#") && len(x.uses) != len(b[k].uses) {
			return false
		}
	}
	return true
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
	k := fmt.Sprintf("%d|%s|%s|%v|%v", c.idx, c.locks, c.rel, c.pre, c.conc)
	if s.callKeys[k] {
		return
	}
	s.callKeys[k] = true
	s.calls = append(s.calls, c)
}

// keep adds what old says that s does not: a function's summary only grows
// from walk to walk. A walk reads the summaries of the functions it calls,
// and some of those (calls through func values, cut places) can make a
// group of summaries take turns dropping and regaining the same accesses;
// growing only, they settle. Everything kept is something the function
// may do, so keeping more stays sound.
func (s *summary) keep(old *summary) {
	if old == nil {
		return
	}
	for _, x := range old.accesses {
		s.addAccess(x)
	}
	for _, x := range old.typed {
		s.addTyped(x)
	}
	for _, c := range old.calls {
		s.addCall(c)
	}
	for _, e := range old.edges {
		s.addEdge(e)
	}
	for _, x := range old.spawns {
		s.addSpawn(x)
	}
	for _, x := range old.cross {
		s.addCrossing(x)
	}
	for i, r := range old.results {
		s.addResult(i, r)
	}
	s.maySpawn = s.maySpawn || old.maySpawn
	s.mayAlloc = s.mayAlloc || old.mayAlloc
	s.pubParams |= old.pubParams
	for i := range old.fresh {
		if old.fresh[i] {
			s.noteFresh(i, true)
		}
		if old.notFresh[i] {
			s.noteFresh(i, false)
		}
	}
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

func sameKeys[K comparable, V any](a, b map[K]V) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// equal reports whether two summaries of a function say the same.
func (s *summary) equal(t *summary) bool {
	if !sameKeys(s.accKeys, t.accKeys) || !sameKeys(s.tyKeys, t.tyKeys) || !sameUses(s.accKeys, t.accKeys) || !sameUses(s.tyKeys, t.tyKeys) || !sameSet(s.callKeys, t.callKeys) || !sameSet(s.edgeKeys, t.edgeKeys) ||
		!sameSet(s.spKeys, t.spKeys) || !sameSet(s.crKeys, t.crKeys) {
		return false
	}
	if s.maySpawn != t.maySpawn || s.mayAlloc != t.mayAlloc || s.pubParams != t.pubParams || !s.exitLocks.equal(t.exitLocks) || !s.exitRel.equal(t.exitRel) {
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

// diff names what differs between s and t (debug).
func (s *summary) diff(t *summary) string {
	var out []string
	one := func(name string, a, b map[accKey]*access) {
		if sameKeys(a, b) {
			return
		}
		var add, del []string
		for k := range a {
			if b[k] == nil {
				add = append(add, fmt.Sprint(k))
			}
		}
		for k := range b {
			if a[k] == nil {
				del = append(del, fmt.Sprint(k))
			}
		}
		sort.Strings(add)
		sort.Strings(del)
		if len(add) > 2 {
			add = add[:2]
		}
		if len(del) > 2 {
			del = del[:2]
		}
		out = append(out, fmt.Sprintf("%s +%v -%v", name, add, del))
	}
	one("acc", s.accKeys, t.accKeys)
	one("typed", s.tyKeys, t.tyKeys)
	if !sameSet(s.callKeys, t.callKeys) {
		out = append(out, "calls")
	}
	if !sameSet(s.edgeKeys, t.edgeKeys) {
		out = append(out, "edges")
	}
	if !sameSet(s.spKeys, t.spKeys) || !sameSet(s.crKeys, t.crKeys) {
		out = append(out, "spawns/cross")
	}
	if s.maySpawn != t.maySpawn || s.mayAlloc != t.mayAlloc || s.pubParams != t.pubParams || !s.exitLocks.equal(t.exitLocks) || !s.exitRel.equal(t.exitRel) {
		out = append(out, "flags/locks")
	}
	if fmt.Sprint(s.fresh, s.notFresh) != fmt.Sprint(t.fresh, t.notFresh) {
		out = append(out, "fresh")
	}
	if fmt.Sprint(s.results) != fmt.Sprint(t.results) {
		out = append(out, "results")
	}
	return strings.Join(out, "; ")
}
