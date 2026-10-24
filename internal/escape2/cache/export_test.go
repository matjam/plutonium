package cache

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
)

// Hooks for the external tests, which run the whole analysis through
// internal/escape2 (which imports this package).

// EncodeSummary is appendSummary.
func EncodeSummary(ps domain.Paths, s *summary.Summary) []byte {
	b, _ := appendSummary(nil, ps, s, testName)
	encodedMu.Lock()
	if !slices.Contains(encodedWith, ps.Filters()) {
		encodedWith = append(encodedWith, ps.Filters())
	}
	encodedMu.Unlock()
	return b
}

// encodedWith are the filter tables EncodeSummary wrote keys of: the atoms
// a test's decoding resolves (testAtom), as a cache resolves them in its
// program (Cache.atomOf).
var (
	encodedMu   sync.Mutex
	encodedWith []*domain.Filters
)

// testName and testID name a deferred call's origin by its number, for the
// external tests.
func testName(id eir.FuncID) string { return strconv.Itoa(int(id)) }

// testAtom resolves an atom by its text among the filters of ps and of the
// tables EncodeSummary wrote with.
func testAtom(ps domain.Paths) func(string) (domain.Atom, bool) {
	return func(text string) (domain.Atom, bool) {
		encodedMu.Lock()
		tables := append([]*domain.Filters{ps.Filters()}, encodedWith...)
		encodedMu.Unlock()
		for _, fs := range tables {
			if a, ok := atomIn(fs, text); ok {
				return a, true
			}
		}
		return domain.Atom{}, false
	}
}

func atomIn(fs *domain.Filters, text string) (domain.Atom, bool) {
	for id := domain.FilterID(1); ; id++ {
		f, ok := fs.Get(id)
		if !ok {
			return domain.Atom{}, false
		}
		for _, a := range f.Atoms {
			if a.Text() == text {
				return a, true
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
	return decodeSummary(b, ps, shape{inputs, results}, testID, testAtom(ps))
}

// Entry is an entry of a file, for tests that rewrite files.
type Entry struct {
	ID  string
	Sum []byte
	e   entry
}

// Key returns the entry's digests, printed.
func (e Entry) Key() string { return fmt.Sprintf("%x %x %x", e.e.eir, e.e.ctx, e.e.read) }

// Files returns the package files in dir.
func Files(dir string) []string {
	names, _ := filepath.Glob(filepath.Join(dir, "*.pesc"))
	return names
}

// ReadEntries decodes a file, returning its package path and entries.
func ReadEntries(name string) (string, []Entry, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return "", nil, err
	}
	r := &reader{b: data[len(magic):]}
	_ = r.uvarint()
	pkg := r.str()
	es, err := decodeFile(pkg, data)
	if err != nil {
		return "", nil, err
	}
	out := make([]Entry, len(es))
	for i, e := range es {
		out[i] = Entry{ID: e.id, Sum: e.sum, e: e}
	}
	return pkg, out, nil
}

// WriteEntries writes a file with a valid checksum: what someone who knows
// the format can write.
func WriteEntries(name, pkg string, es []Entry) error {
	out := make([]entry, len(es))
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
		v, _ := binary.Uvarint(b[1:])
		inputs = int(v)
	}
	s, err := decodeSummary(b, ps, shape{inputs, 1 << 20}, testID, testAtom(ps))
	return s, inputs, err
}
