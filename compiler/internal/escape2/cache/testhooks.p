// Ported from internal/escape2/cache/export_test.go at the go-cutover tag.
// In Go it was a test file of package cache, compiled only into the
// package's own test binary. The tests that use it import
// compiler/internal/escape2, which imports this package, so they are in
// package escape2 (internal/escape2/cache_test.p and the others), and
// pluto test compiles a package's _test.p files only into that package's
// test binary: the hooks are an ordinary file here.

package cache

import (
	"pluto/os"
	"pluto/path/filepath"
	"pluto/slices"
	"pluto/sort"
	"pluto/strconv"
	"pluto/strings"

	"compiler/compat"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/summary"
	"compiler/pu"
)

// Hooks for the external tests, which run the whole analysis through
// internal/escape2 (which imports this package).

// EncodeSummary is appendSummary.
func EncodeSummary(ps domain.Paths, s *summary.Summary) []byte {
	b, _ := appendSummary(nil, ps, s, testName)
	if !slices.Contains(encodedWith, ps.Filters()) {
		encodedWith = compat.Append(encodedWith, ps.Filters())
	}
	return b
}

// encodedWith are the filter tables EncodeSummary wrote keys of: the atoms
// a test's decoding resolves (testAtom), as a cache resolves them in its
// program (Cache.atomOf). (The Go hooks held a mutex; a test runs on one
// thread here.)
var encodedWith []*domain.Filters

// ForgetEncoded empties encodedWith. A test that makes each program in a
// scratch.Region calls it before it releases the region: the tables, and
// encodedWith's array, are the region's, and the next program's
// EncodeSummary would read them after the release.
func ForgetEncoded() { encodedWith = nil }

// EncodedMark returns how many tables encodedWith holds, for
// ForgetEncodedFrom.
func EncodedMark() int { return len(encodedWith) }

// ForgetEncodedFrom keeps the first n tables of encodedWith, in an array
// from the current allocator. A test that runs one analysis in a region
// nested in the program's calls it, from the program's allocator, before
// it releases the nested region: the tables EncodeSummary added there, and
// perhaps encodedWith's array, are the nested region's.
func ForgetEncodedFrom(n int) { encodedWith = compat.Clone(encodedWith[:n]) }

// testName and testID name a deferred call's origin by its number, for the
// external tests.
func testName(id eir.FuncID) string { return strconv.Itoa(int(id)) }

// testAtom resolves an atom by its text among the filters of ps and of the
// tables EncodeSummary wrote with. Unlike the Go hook, which searched the
// tables in the function it returned, the atoms are listed by text first,
// as Cache.atomOf has them: the reader's atom function then only looks
// them up (the analysis takes every function a reader may call, and
// Atom.Text, which formats types, to keep what it is given).
func testAtom(ps domain.Paths) *testAtoms {
	ta := pu.New(testAtoms{byText: map[string]domain.Atom{}})
	tables := compat.Append([]*domain.Filters{ps.Filters()}, encodedWith...)
	for _, fs := range tables {
		atomsIn(ta.byText, fs)
	}
	return ta
}

// testAtoms are the atoms testAtom found, by text.
type testAtoms struct{ byText map[string]domain.Atom }

func (ta *testAtoms) atom(text string) (domain.Atom, bool) {
	a, ok := ta.byText[text]
	return a, ok
}

// atomsIn adds the atoms of fs to byText, keeping one found before (the
// first in the tables' order, as the Go hook's search found it).
func atomsIn(byText map[string]domain.Atom, fs *domain.Filters) {
	for id := domain.FilterID(1); ; id++ {
		f, ok := fs.Get(id)
		if !ok {
			return
		}
		for _, a := range f.Atoms {
			if _, seen := byText[a.Text()]; !seen {
				byText[a.Text()] = a
			}
		}
	}
}

func testID(s string) (eir.FuncID, bool) {
	n, err := strconv.Atoi(s)
	return eir.FuncID(n), err == nil
}

// DecodeSummary is decodeSummary for a function of the given shape.
func DecodeSummary(b []byte, ps domain.Paths, inputs, results int) (*summary.Summary, error) {
	return decodeSummary(b, ps, shape{inputs, results}, testID, testAtom(ps).atom)
}

// Entry is an entry of a file, for tests that rewrite files.
type Entry struct {
	ID  string
	Sum []byte
	e   entry
}

// Key returns the entry's digests, printed.
func (e Entry) Key() string {
	return compat.Sprintf("%x %x %x", compat.Clone(e.e.eir[:]), compat.Clone(e.e.ctx[:]), compat.Clone(e.e.read[:]))
}

// Files returns the package files in dir.
func Files(dir string) []string {
	var names []string
	ents := os.ReadDir(dir) or return nil
	for i := 0; i < ents.Len(); i++ {
		if n := ents.At(i).NameLossy(); strings.HasSuffix(n, ".pesc") {
			names = compat.Append(names, filepath.Join(dir, n))
		}
	}
	sort.Strings(names)
	return names
}

// ReadEntries decodes a file, returning its package path and entries.
func ReadEntries(name string) (string, []Entry, error) {
	data, err := compat.ReadFile(name)
	if err != nil {
		return "", nil, err
	}
	r := reader{b: data[len(magic):]}
	_ = r.uvarint()
	pkg := r.str()
	es, err := decodeFile(pkg, data)
	if err != nil {
		return "", nil, err
	}
	out := compat.Make[Entry](len(es))
	for i, e := range es {
		out[i] = Entry{ID: e.id, Sum: e.sum, e: e}
	}
	return pkg, out, nil
}

// WriteEntries writes a file with a valid checksum: what someone who knows
// the format can write.
func WriteEntries(name, pkg string, es []Entry) error {
	out := compat.Make[entry](len(es))
	for i, e := range es {
		out[i] = e.e
		out[i].sum = e.Sum
	}
	return os.WriteFile(name, encodeFile(pkg, out), 0o644)
}

// DecodeAny decodes an entry's summary for a function of the shape the
// summary itself claims, into ps.
func DecodeAny(b []byte, ps domain.Paths) (*summary.Summary, int, error) {
	inputs := 0
	if len(b) > 1 && b[0] == 1 {
		v, _ := compat.Uvarint(b[1:])
		inputs = int(v)
	}
	s, err := decodeSummary(b, ps, shape{inputs, 1 << 20}, testID, testAtom(ps).atom)
	return s, inputs, err
}
