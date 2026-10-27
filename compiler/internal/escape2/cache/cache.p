// Translated by go2pu from internal/escape2/cache/cache.go at the go-cutover tag; edited by hand since.

// Package cache is the checked summary cache of decision D44 (plan
// docs/plans/2026-10-20-escape-redesign.md, section (f)): one run writes the
// summaries of the escape analysis to a file per package, and a later run
// loads them, checks them, and never trusts them.
//
// The check is the driver's (internal/escape2/sched, cache.go): an SCC whose
// functions all have summaries in the files is solved once against them, and
// accepted only if every result is summary.Leq its loaded summary. Then the
// loaded summaries are a post-fixed point of the solver over the SCC, given
// the final summaries of everything it calls, and by induction on the depth
// of calls they cover every execution: a summary from the file is used only
// where it is as good as one computed. A file that fails the check costs the
// solves of that SCC, as without a cache. Before a finding is reported, every
// SCC below it that depends on a summary from the file is solved again from
// bottom, so a file that is wider than need be (it passes the check) cannot
// reject a program either.
//
// What this package adds is which summary to offer and when not to: an
// entry is offered only when the inputs it was computed from are those of
// this run (the function's EIR, its SCC, its callees' summaries, the
// candidate sets of its dynamic calls, the compiler and the flags), and is
// accepted only when the stage B facts and contract entries its solve reads
// are those recorded with it. So a stale file is recomputed, not merely
// checked, and the summaries of a run with a cache are those of a run
// without one. A damaged, truncated, hand-edited or swapped file is read as
// far as its checksum and decoder accept it, and offered only through the
// check.
package cache

import (
	"pluto/os"
	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/sched"
	"compiler/internal/escape2/solve"
	"compiler/internal/escape2/summary"
	"compiler/internal/types2"
	"compiler/pu"
)

// EnvVar names the directory of the cache: unset or empty, no cache.
const EnvVar = "PLUTO_ESCAPE_CACHE"

// Config are the inputs of every summary that are not in the program.
type Config struct {
	// Compiler identifies the compiler; empty means a digest of the
	// running executable (so a rebuilt compiler starts afresh).
	Compiler string
	// Flags are the build settings that change the analysis but not the
	// program it reads.
	Flags string
}

// Stats count what the cache read.
type Stats struct {
	Files        int // package files read
	BadFiles     int // files that were damaged or of another version or package
	Offered      int // summaries offered to the driver
	BadSummaries int // entries whose summary did not decode for its function
}

// Cache is the cache of one program's analysis: Open it, pass it as
// sched.Options.Cache, and Save the result.
type Cache struct {
	dir  string
	salt digest

	prog  *eir.Program
	paths domain.Paths
	ids   []string
	byID  map[string]eir.FuncID // the functions by stable ID

	eirSum  []digest
	eirDone []bool
	ctx     []digest
	ctxDone []bool
	read    []digest
	readOK  []bool
	loaded  []digest // the reads recorded with the summary offered

	types map[types2.Type]string
	atoms map[string]domain.Atom // the type filters' atoms the program can make, by text (atomOf)
	sums  map[*summary.Summary]digest
	files map[string]map[lookupKey]entry

	Stats Stats
}

type lookupKey struct {
	id       string
	eir, ctx digest
}

var (
	exeOnce   compat.Once
	exeDigest string
	exeErr    error
)

// compilerDigest is a digest of the running executable.
func compilerDigest() (string, error) {
	exeOnce.Do(func() {
		name, err := compat.Executable()
		if err != nil {
			exeErr = err
			return
		}
		data, err := compat.ReadFile(name)
		if err != nil {
			exeErr = err
			return
		}
		sum := compat.Sum256(data)
		exeDigest = compat.Sprintf("%x", sum)
	})
	return exeDigest, exeErr
}

// Open returns a cache in dir, which it creates when it saves.
func Open(dir string, cfg Config) (*Cache, error) {
	if cfg.Compiler == "" {
		d, err := compilerDigest()
		if err != nil {
			return nil, compat.Wrap(compat.Sprintfb("escape cache: identifying the compiler: %w", compat.ArgW(err)), err)
		}
		cfg.Compiler = d
	}
	h := newHasher()
	h.str(magic)
	h.int(version)
	h.str(cfg.Compiler)
	h.str(cfg.Flags)
	return pu.New(Cache{
		dir:   dir,
		salt:  h.sum(),
		types: map[types2.Type]string{},
		sums:  map[*summary.Summary]digest{},
		files: map[string]map[lookupKey]entry{},
	}), nil
}

func (c *Cache) init(v sched.CacheView) {
	if c.prog != nil {
		if c.prog != v.Program() {
			panic("internal compiler error: escape2/cache: one cache used for two programs")
		}
		return
	}
	c.prog, c.paths = v.Program(), v.Paths()
	n := len(c.prog.Funcs)
	c.ids = stableIDs(c.prog)
	c.byID = make(map[string]eir.FuncID, n)
	for id, s := range c.ids {
		if s != "" {
			c.byID[s] = eir.FuncID(id)
		}
	}
	c.eirSum, c.eirDone = compat.Make[digest](n), compat.Make[bool](n)
	c.ctx, c.ctxDone = compat.Make[digest](n), compat.Make[bool](n)
	c.read, c.readOK = compat.Make[digest](n), compat.Make[bool](n)
	c.loaded = compat.Make[digest](n)
}

// Begin implements sched.Cache: it notes the context of comp's functions
// and, with load, offers their summaries.
func (c *Cache) Begin(v sched.CacheView, comp []eir.FuncID, load bool) []*summary.Summary {
	c.init(v)
	for _, id := range comp {
		c.ctx[id], c.ctxDone[id] = c.context(v, comp, id), true
	}
	if !load {
		return nil
	}
	out := compat.Make[*summary.Summary](len(comp))
	for i, id := range comp {
		if c.ids[id] == "" {
			return nil
		}
		f := c.prog.Funcs[id]
		e, ok := c.lookup(pkgOf(f), lookupKey{c.ids[id], c.eirDigest(id), c.ctx[id]})
		if !ok {
			return nil
		}
		s, err := decodeSummary(e.sum, c.paths, shapeOf(f), c.funcByID, c.atomOf)
		if err != nil {
			c.Stats.BadSummaries++
			return nil
		}
		out[i] = s
		c.loaded[id] = e.read
	}
	c.Stats.Offered += len(comp)
	return out
}

// context is the digest of what a solve of id reads that is fixed before
// its SCC is solved (file.go).
func (c *Cache) context(v sched.CacheView, comp []eir.FuncID, id eir.FuncID) digest {
	h := newHasher()
	h.bytes(c.salt[:])
	in := map[eir.FuncID]bool{}
	members := compat.MakeCap[string](0, len(comp))
	for _, m := range comp {
		in[m] = true
		d := c.eirDigest(m)
		members = compat.Append(members, compat.Concat(c.idOrName(m), hexDigest(d)))
	}
	sortStrings(members)
	h.int(int64(len(members)))
	for _, m := range members {
		h.str(m)
	}
	deps := v.Deps(id)
	h.int(int64(len(deps)))
	for _, d := range deps {
		c.writeFuncRef(h, d)
		var dd digest
		if in[d] {
			dd = c.eirDigest(d)
		} else {
			dd = c.sumDigest(v.Summary(d))
		}
		h.bytes(dd[:])
	}
	// The candidate set of each dynamic call: the solve applies their join
	// (D47), so a change of its members is a change of input.
	f := c.prog.Funcs[id]
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			cl := b.Instrs[i].Call
			if cl == nil {
				continue
			}
			switch cl.Kind {
			case eir.CallIface, eir.CallTypeParam, eir.CallFunc:
				cands := v.Candidates(f, cl)
				h.int(int64(len(cands)))
				for _, cand := range cands {
					c.writeFuncRef(h, cand)
				}
			}
		}
	}
	return h.sum()
}

// Recorder implements sched.Cache.
func (c *Cache) Recorder(eir.FuncID) sched.Recorder { return c.newRecorder() }

// Solved implements sched.Cache.
func (c *Cache) Solved(id eir.FuncID, r sched.Recorder) {
	c.read[id], c.readOK[id] = r.(*recorder).sum(), true
}

// Matches implements sched.Cache.
func (c *Cache) Matches(id eir.FuncID, r sched.Recorder) bool {
	return r.(*recorder).sum() == c.loaded[id]
}

func pkgOf(f *eir.Func) string {
	if f.Pkg == nil {
		return "_"
	}
	return f.Pkg.Path()
}

// lookup finds an entry in a package's file, reading the file the first
// time.
func (c *Cache) lookup(pkg string, k lookupKey) (entry, bool) {
	m, ok := c.files[pkg]
	if !ok {
		m = map[lookupKey]entry{}
		c.files[pkg] = m
		data, err := compat.ReadFile(fileName(c.dir, pkg))
		if err == nil {
			c.Stats.Files++
			entries, err := decodeFile(pkg, data)
			if err != nil {
				c.Stats.BadFiles++
			}
			for _, e := range entries {
				lk := lookupKey{e.id, e.eir, e.ctx}
				if _, dup := m[lk]; !dup {
					m[lk] = e
				}
			}
		}
	}
	e, ok := m[k]
	return e, ok
}

// Save writes the summaries of res to the package files: each function's
// summary with the digests of its inputs, merged with the entries already
// in the file (another program's, another version's) up to maxVariants per
// function. A failure to write costs the next run time, nothing else.
func (c *Cache) Save(res *sched.Result) error {
	if c.prog == nil || res == nil {
		return nil
	}
	byPkg := map[string][]entry{}
	for id, f := range c.prog.Funcs {
		if c.ids[id] == "" || !c.ctxDone[id] || !c.readOK[id] || id >= len(res.Summaries) {
			continue
		}
		e := entry{entryKey: entryKey{id: c.ids[id], eir: c.eirDigest(eir.FuncID(id)), ctx: c.ctx[id], read: c.read[id]}}
		var ok bool
		e.sum, ok = appendSummary(nil, c.paths, res.Summaries[id], c.stableID)
		if !ok {
			continue // a deferred call of a function without a stable ID
		}
		byPkg[pkgOf(f)] = compat.Append(byPkg[pkgOf(f)], e)
	}
	if len(byPkg) == 0 {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return compat.Wrap(compat.Sprintfb("escape cache: %w", compat.ArgW(err)), err)
	}
	pkgs := compat.MakeCap[string](0, len(byPkg))
	for p := range byPkg {
		pkgs = compat.Append(pkgs, p)
	}
	sortStrings(pkgs)
	var firstErr error
	for _, pkg := range pkgs {
		name := fileName(c.dir, pkg)
		var old []entry
		if data, err := compat.ReadFile(name); err == nil {
			old, _ = decodeFile(pkg, data) // a damaged file is replaced
		}
		data := encodeFile(pkg, merge(byPkg[pkg], old))
		if err := writeFile(name, data); err != nil && firstErr == nil {
			firstErr = compat.Wrap(compat.Sprintfb("escape cache: %w", compat.ArgW(err)), err)
		}
	}
	return firstErr
}

// merge returns this run's entries and the old ones that do not have the
// same function, EIR and context as one of them, at most maxVariants per
// function (this run's first, then the old ones in file order).
func merge(cur, old []entry) []entry {
	have := map[lookupKey]bool{}
	count := map[string]int{}
	out := compat.MakeCap[entry](0, len(cur)+len(old))
	for _, e := range cur {
		k := lookupKey{e.id, e.eir, e.ctx}
		if have[k] {
			continue
		}
		have[k] = true
		count[e.id]++
		out = compat.Append(out, e)
	}
	for _, e := range old {
		k := lookupKey{e.id, e.eir, e.ctx}
		if have[k] || count[e.id] >= maxVariants {
			continue
		}
		have[k] = true
		count[e.id]++
		out = compat.Append(out, e)
	}
	return out
}

var _ sched.Cache = (*Cache)(nil)

// stableID is the stable ID of a function ("" for one without).
func (c *Cache) stableID(id eir.FuncID) string {
	if int(id) < len(c.ids) {
		return c.ids[id]
	}
	return ""
}

// funcByID is the function of a stable ID.
func (c *Cache) funcByID(s string) (eir.FuncID, bool) {
	id, ok := c.byID[s]
	return id, ok
}

// atomOf resolves a type filter's atom by its text: the atoms the program's
// assertions and dynamic calls' candidates make (solve.AssertFilter,
// solve.RecvFilter), each filter of a summary a union of them. A text two
// types share (local types of one name) resolves to none, and the key is
// read without its filter.
func (c *Cache) atomOf(text string) (domain.Atom, bool) {
	if c.atoms == nil {
		c.atoms = map[string]domain.Atom{}
		bad := map[string]bool{}
		add := func(f domain.Filter, ok bool) {
			if !ok {
				return
			}
			for _, a := range f.Atoms {
				t := a.Text()
				if old, dup := c.atoms[t]; dup && (old.Kind != a.Kind || !types2.Identical(old.T, a.T)) {
					bad[t] = true
				}
				c.atoms[t] = a
			}
		}
		for id, f := range c.prog.Funcs {
			add(solve.RecvFilter(c.prog, eir.FuncID(id), true))
			add(solve.RecvFilter(c.prog, eir.FuncID(id), false))
			for _, b := range f.Blocks {
				for i := range b.Instrs {
					if in := &b.Instrs[i]; in.Op == eir.OpAssert {
						add(solve.AssertFilter(in.Type))
					}
				}
			}
		}
		for t := range bad {
			delete(c.atoms, t)
		}
	}
	a, ok := c.atoms[text]
	return a, ok
}
